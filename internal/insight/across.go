package insight

import (
	"cmp"
	"fmt"
	"maps"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/zakharkiv-vitalik/sexy-agent-tuner/internal/analysis"
	"github.com/zakharkiv-vitalik/sexy-agent-tuner/internal/session"
)

// SessionRef points at one event of one session: evidence for cross-session findings.
type SessionRef struct {
	Session string `json:"session"`
	Seq     int    `json:"seq"`
}

// Cross-session thresholds.
const (
	minSessions         = 3   // a pattern must recur in at least this many sessions
	minInstructionWords = 4   // shorter sentences ("ok, go on") are not instructions
	alwaysShare         = 0.5 // in this share of a project's sessions → belongs in CLAUDE.md
	chainLen            = 3
	maxPerRule          = 5
	maxFilesListed      = 10
)

// Across finds patterns that recur over several sessions: instructions the
// user keeps retyping, files every session re-explores, tool chains the agent
// keeps rebuilding. These are the candidates for CLAUDE.md, skills, subagents
// or scripts. Findings carry Sessions as evidence, not Evidence.
func Across(ss []*session.Session) []Finding {
	out := slices.Concat(repeatedInstructions(ss), repeatedExploration(ss), repeatedChains(ss), outdatedAcross(ss), secretsAcross(ss))
	slices.SortStableFunc(out, func(a, b Finding) int { return cmp.Compare(len(b.Sessions), len(a.Sessions)) })
	return out
}

// recurring maps a pattern key to its first occurrence in each session.
type recurring map[string]map[string]SessionRef

func (r recurring) add(key string, ref SessionRef) {
	if r[key] == nil {
		r[key] = map[string]SessionRef{}
	}
	if _, ok := r[key][ref.Session]; !ok {
		r[key][ref.Session] = ref
	}
}

// groups returns the keys seen in at least minSessions sessions, clustered by
// identical session sets: sentences of one pasted block, or overlapping chains
// of one routine, always co-occur and are one pattern, not several.
// Most frequent first.
func (r recurring) groups() [][]string {
	bySet := map[string][]string{}
	for key, refs := range r {
		if len(refs) >= minSessions {
			set := strings.Join(slices.Sorted(maps.Keys(refs)), ",")
			bySet[set] = append(bySet[set], key)
		}
	}
	var out [][]string
	for _, keys := range bySet {
		slices.Sort(keys)
		out = append(out, keys)
	}
	slices.SortFunc(out, func(a, b []string) int {
		return cmp.Or(cmp.Compare(len(r[b[0]]), len(r[a[0]])), cmp.Compare(a[0], b[0]))
	})
	return out[:min(maxPerRule, len(out))]
}

func (r recurring) refs(key string) []SessionRef {
	refs := slices.Collect(maps.Values(r[key]))
	slices.SortFunc(refs, func(a, b SessionRef) int { return cmp.Compare(a.Session, b.Session) })
	return refs
}

var (
	pastedBlock   = regexp.MustCompile(`(?s)<pasted_content[^>]*>.*?</pasted_content[^>]*>`)
	sentenceBreak = regexp.MustCompile(`[.!?]+\s+|\n+`)
)

// sentences splits a prompt into normalized instruction candidates.
func sentences(prompt string) []string {
	var out []string
	for _, s := range sentenceBreak.Split(pastedBlock.ReplaceAllString(prompt, " "), -1) {
		s = strings.Trim(strings.Join(strings.Fields(strings.ToLower(s)), " "), " .,:;!?-*\"'«»`")
		if len(strings.Fields(s)) >= minInstructionWords {
			out = append(out, s)
		}
	}
	return out
}

func repeatedInstructions(ss []*session.Session) []Finding {
	seen := recurring{}
	project := map[string]string{} // session id -> project
	perProject := map[string]int{} // project -> sessions
	for _, s := range ss {
		project[s.ID] = s.Project
		perProject[s.Project]++
		for _, e := range s.Events {
			if e.Kind == session.KindUserPrompt && e.Agent == "" {
				for _, t := range sentences(e.Text) {
					seen.add(t, SessionRef{s.ID, e.Seq})
				}
			}
		}
	}

	var out []Finding
	for _, keys := range seen.groups() {
		refs := seen.refs(keys[0])
		projects := map[string]bool{}
		total := 0
		for _, ref := range refs {
			if p := project[ref.Session]; !projects[p] {
				projects[p] = true
				total += perProject[p]
			}
		}
		if len(projects) > 1 {
			total = len(ss) // a user-level CLAUDE.md would load into every session
		}
		always := float64(len(refs))/float64(total) >= alwaysShare
		scope := "this project's CLAUDE.md"
		if len(projects) > 1 {
			scope = "~/.claude/CLAUDE.md (user level, all projects)"
		}

		f := Finding{
			Rule: "repeated-instructions", Target: TargetContext, Confidence: "medium",
			Observation: fmt.Sprintf("The same instruction (%d sentence(s)) appears in your prompts in %d of %d sessions across %d project(s).",
				len(keys), len(refs), total, len(projects)),
			Snippet:  strings.Join(keys, "\n"),
			Sessions: refs,
		}
		if always {
			f.Title = "You retype the same instruction in most sessions"
			f.Hypothesis = "This is a standing rule, not part of any one task. Typing it every time costs attention and is easy to forget."
			f.Suggestion = "Move it to " + scope + " so every session starts with it."
			f.Benefit = "The rule applies even when you forget to type it."
			f.Downside = "CLAUDE.md is loaded into every session's context, including ones where the rule is irrelevant."
		} else {
			f.Title = "You repeat the same procedure on some tasks"
			f.Target = TargetWorkflow
			f.Hypothesis = "It's a procedure for one kind of task, not a rule for every session, so CLAUDE.md would carry it where it isn't needed."
			f.Suggestion = "Turn it into a skill (loaded only when relevant) or a slash command (.claude/commands/<name>.md) you invoke by name."
			f.Benefit = "One short command instead of retyping; the procedure stays identical between sessions."
			f.Downside = "A skill is only used when its description matches the task. Check that it actually triggers."
		}
		out = append(out, f)
	}
	return out
}

// repeatedExploration finds files read before the first edit in many sessions
// of the same project: knowledge every session rediscovers.
func repeatedExploration(ss []*session.Session) []Finding {
	byProject := map[string]recurring{}
	for _, s := range ss {
		if byProject[s.Project] == nil {
			byProject[s.Project] = recurring{}
		}
		for _, e := range s.Events {
			if e.Kind != session.KindToolCall || e.Agent != "" {
				continue
			}
			a := analysis.Classify(e.Tool, s.Project)
			if a.Category == analysis.CatEdit {
				break
			}
			if a.Category == analysis.CatRead {
				for _, p := range a.Files {
					if !analysis.IsScratch(p) {
						byProject[s.Project].add(p, SessionRef{s.ID, e.Seq})
					}
				}
			}
		}
	}

	var out []Finding
	for _, project := range slices.Sorted(maps.Keys(byProject)) {
		seen := byProject[project]
		var files []string
		var refs []SessionRef
		sessions := map[string]bool{}
		for p, m := range seen {
			if len(m) >= minSessions {
				files = append(files, p)
			}
		}
		if len(files) == 0 {
			continue
		}
		slices.SortFunc(files, func(a, b string) int { return cmp.Or(cmp.Compare(len(seen[b]), len(seen[a])), cmp.Compare(a, b)) })
		files = files[:min(maxFilesListed, len(files))]
		var snippet strings.Builder
		snippet.WriteString("Key files:\n")
		for _, p := range files {
			fmt.Fprintf(&snippet, "- %s — <what it is responsible for>\n", strings.TrimPrefix(p, project+"/"))
			for _, ref := range seen.refs(p) {
				if !sessions[ref.Session] {
					sessions[ref.Session] = true
					refs = append(refs, ref)
				}
			}
		}
		out = append(out, Finding{
			Rule: "repeated-exploration", Target: TargetContext, Confidence: "medium",
			Title:       fmt.Sprintf("Sessions keep re-exploring the same %d file(s) in %s", len(files), filepath.Base(project)),
			Observation: fmt.Sprintf("In %s, %d sessions read these files before their first edit; each file in %d+ sessions.", project, len(refs), minSessions),
			Hypothesis:  "The agent rebuilds the same mental map of the project at the start of each task.",
			Suggestion:  "Describe these files in the project's CLAUDE.md so the agent starts with the map instead of rebuilding it.",
			Snippet:     strings.TrimSuffix(snippet.String(), "\n"),
			Benefit:     "Fewer exploratory reads at the start of each session.",
			Downside:    "The description goes stale when the code moves; the agent may trust it over the code.",
			Sessions:    refs,
		})
	}
	return out
}

// repeatedChains finds identical runs of consecutive tool steps (category +
// target) that the main agent performs in several sessions.
func repeatedChains(ss []*session.Session) []Finding {
	seen := recurring{}
	cats := map[string][]analysis.Category{}
	for _, s := range ss {
		var steps []string
		var stepCats []analysis.Category
		var seqs []int
		for _, e := range s.Events {
			if e.Kind != session.KindToolCall || e.Agent != "" {
				continue
			}
			a := analysis.Classify(e.Tool, s.Project)
			step := string(a.Category) + " " + strings.TrimPrefix(a.Target, s.Project+"/")
			if a.Category == analysis.CatPlan || (len(steps) > 0 && steps[len(steps)-1] == step) {
				continue
			}
			steps, stepCats, seqs = append(steps, step), append(stepCats, a.Category), append(seqs, e.Seq)
		}
		for i := 0; i+chainLen <= len(steps); i++ {
			key := strings.Join(steps[i:i+chainLen], "  →  ")
			cats[key] = stepCats[i : i+chainLen]
			seen.add(key, SessionRef{s.ID, seqs[i]})
		}
	}

	var out []Finding
	for _, keys := range seen.groups() {
		key := keys[0] // overlapping windows of one routine; one is enough to show it
		refs := seen.refs(key)
		f := Finding{
			Rule: "repeated-chain", Confidence: "low",
			Observation: fmt.Sprintf("The same %d-step tool sequence ran in %d sessions: %s", chainLen, len(refs), key),
			Snippet:     strings.ReplaceAll(key, "  →  ", "\n"),
			Sessions:    refs,
			Downside:    "Identical steps may be coincidence on a small project; check the sessions before automating.",
		}
		switch c := cats[key]; {
		case !slices.ContainsFunc(c, func(c analysis.Category) bool {
			return c != analysis.CatRead && c != analysis.CatSearch && c != analysis.CatWeb && c != analysis.CatMCP
		}):
			f.Target, f.Title = TargetWorkflow, "The agent repeats the same investigation across sessions"
			f.Hypothesis = "Each session pays the full context cost of this lookup, though only its conclusion is used afterwards."
			f.Suggestion = "Delegate it to a subagent that returns a short summary, or record the conclusion in CLAUDE.md if it rarely changes."
			f.Benefit = "The raw tool output stays out of the main context."
		case slices.Contains(c, analysis.CatEdit):
			f.Target, f.Title = TargetWorkflow, "The agent rebuilds the same procedure across sessions"
			f.Hypothesis = "This is a recurring routine the agent rediscovers step by step each time."
			f.Suggestion = "Write it down as a skill with the exact steps, so the agent follows it instead of reconstructing it."
			f.Benefit = "Fewer steps and the same result every time."
		default:
			f.Target, f.Title = TargetWorkflow, "The same commands run in the same order across sessions"
			f.Hypothesis = "A deterministic sequence doesn't need the model to decide each step."
			f.Suggestion = "Put it in a script or Makefile target (or a hook if it should always run) and tell the agent to call it."
			f.Benefit = "One call instead of several model round trips."
		}
		out = append(out, f)
	}
	return out
}

// outdatedAcross is outdated-model over many sessions: an old model in many
// sessions usually means a default worth changing once.
func outdatedAcross(ss []*session.Session) []Finding {
	seen := recurring{}
	tokens := map[string]int{}
	for _, s := range ss {
		use := models(s, false)
		total := 0
		for _, u := range use {
			total += u.calls
		}
		for m, u := range use {
			if _, ok := upgrades[m]; ok && float64(u.calls) >= outdatedMinShare*float64(total) {
				seen.add(m, SessionRef{s.ID, u.first})
				tokens[m] += u.tokens
			}
		}
	}
	var out []Finding
	for _, m := range slices.Sorted(maps.Keys(seen)) {
		refs := seen.refs(m)
		if len(refs) < minSessions {
			continue
		}
		up := upgrades[m]
		out = append(out, Finding{
			Rule: "outdated-model", Target: TargetModel, Confidence: "medium",
			Title:          fmt.Sprintf("%d sessions still run on %s", len(refs), m),
			Observation:    fmt.Sprintf("%s did a large part of the work in %d sessions (%s tokens processed).", m, len(refs), tok(tokens[m])),
			Hypothesis:     "It is probably a default or a habit rather than a per-task choice.",
			Suggestion:     fmt.Sprintf("Make %s the default in ~/.claude/settings.json, or use /model %s. It is %s.", up.to, up.to, up.why),
			Snippet:        fmt.Sprintf(`{ "model": %q }`, up.to),
			Benefit:        "Every future session gets the newer model of the same tier.",
			Downside:       "Prompts and CLAUDE.md tuned for the old model may land differently; check a few sessions after switching.",
			TokensInvolved: tokens[m],
			Sessions:       refs,
		})
	}
	return out
}
