package insight

import (
	"cmp"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/zakharkiv-vitalik/sexy-agent-tuner/internal/analysis"
	"github.com/zakharkiv-vitalik/sexy-agent-tuner/internal/session"
)

// TargetModel: act by choosing a different model.
const TargetModel Target = "model"

// Model rule thresholds.
const (
	outdatedMinShare   = 0.25 // of the session's model calls
	routineMaxLookups  = 5    // reads + searches
	routineMaxEdits    = 3
	routineMinCommands = 3
	routineMaxCalls    = 40
	struggleMinFailed  = 5
	struggleFailShare  = 0.2
)

type upgrade struct{ to, why string }

// upgrades maps a model to its current successor, with prices per 1M
// input/output tokens on the Anthropic API as of 2026-10-06.
// ponytail: hand-kept table; update it when models ship.
var upgrades = map[string]upgrade{
	"claude-opus-5":     {"claude-opus-5-5", "newer and cheaper: $4/$20 per 1M tokens instead of $5/$25"},
	"claude-opus-4-8":   {"claude-opus-5-5", "newer and cheaper: $4/$20 per 1M tokens instead of $5/$25"},
	"claude-opus-4-7":   {"claude-opus-5-5", "newer and cheaper: $4/$20 per 1M tokens instead of $5/$25"},
	"claude-opus-4-6":   {"claude-opus-5-5", "newer and cheaper: $4/$20 per 1M tokens instead of $5/$25"},
	"claude-sonnet-5":   {"claude-sonnet-5-5", "newer at the same price ($2/$10 per 1M tokens)"},
	"claude-sonnet-4-6": {"claude-sonnet-5-5", "newer and cheaper: $2/$10 per 1M tokens instead of $3/$15"},
	"claude-haiku-4-5":  {"claude-haiku-5-5", "newer and much cheaper: $0.10/$0.50 per 1M tokens instead of $1/$5"},
}

var dateSuffix = regexp.MustCompile(`-\d{8}$`)

// normModel drops the date suffix some logs carry (claude-haiku-4-5-20251001).
func normModel(m string) string { return dateSuffix.ReplaceAllString(m, "") }

type tierKind int

const (
	tierUnknown tierKind = iota
	tierSmall            // haiku
	tierMid              // sonnet
	tierTop              // opus, fable, mythos
)

func tier(model string) tierKind {
	switch {
	case strings.Contains(model, "opus"), strings.Contains(model, "fable"), strings.Contains(model, "mythos"):
		return tierTop
	case strings.Contains(model, "sonnet"):
		return tierMid
	case strings.Contains(model, "haiku"):
		return tierSmall
	}
	return tierUnknown
}

// modelUse is per-model call and token counts for one session.
type modelUse struct {
	calls, tokens, first int // first: seq of the first call
}

// models counts model calls; main restricts it to the main agent.
func models(s *session.Session, main bool) map[string]*modelUse {
	out := map[string]*modelUse{}
	for _, e := range s.Events {
		if e.Kind != session.KindModelCall || (main && e.Agent != "") || e.Usage.Model == "" || e.Usage.Model == "<synthetic>" {
			continue
		}
		m := normModel(e.Usage.Model)
		u := out[m]
		if u == nil {
			u = &modelUse{first: e.Seq}
			out[m] = u
		}
		u.calls++
		u.tokens += e.Usage.Context() + e.Usage.Output
	}
	return out
}

// mainModel is the model behind most of the main agent's calls.
func mainModel(s *session.Session) (string, int) {
	use := models(s, true)
	best, total := "", 0
	for _, m := range slices.Sorted(maps.Keys(use)) {
		total += use[m].calls
		if best == "" || use[m].calls > use[best].calls {
			best = m
		}
	}
	return best, total
}

func outdatedModel(f *facts) *Finding {
	use := models(f.s, false)
	total := 0
	for _, u := range use {
		total += u.calls
	}
	best := ""
	for _, m := range slices.Sorted(maps.Keys(use)) {
		if _, ok := upgrades[m]; ok && float64(use[m].calls) >= outdatedMinShare*float64(total) && (best == "" || use[m].calls > use[best].calls) {
			best = m
		}
	}
	if best == "" {
		return nil
	}
	u, up := use[best], upgrades[best]
	return &Finding{
		Rule: "outdated-model", Target: TargetModel, Confidence: "medium",
		Title:          fmt.Sprintf("Ran on %s; %s is its successor", best, up.to),
		Observation:    fmt.Sprintf("%d of %d model calls (%s tokens processed) ran on %s.", u.calls, total, tok(u.tokens), best),
		Hypothesis:     "The session used an older model, probably a default or a /model choice that was never revisited.",
		Suggestion:     fmt.Sprintf("Switch to %s with /model %s, or make it the default in ~/.claude/settings.json. It is %s.", up.to, up.to, up.why),
		Snippet:        fmt.Sprintf(`{ "model": %q }`, up.to),
		Benefit:        "A newer model of the same tier, " + up.why + ".",
		Downside:       "Prompts and CLAUDE.md tuned for the old model may land differently; check a couple of sessions after switching.",
		TokensInvolved: u.tokens,
		Evidence:       []int{u.first},
	}
}

const explorerAgent = `---
name: explorer
description: Read-only codebase exploration. Use before editing to find where something is implemented and how the data flows.
tools: Read, Grep, Glob, Bash
model: haiku
---
Find the code relevant to the task. Report the files and functions involved (with line numbers),
how they connect, and your hypothesis. Be concise and don't edit anything.`

// exploreOnTopModel replaces exploration-heavy when the exploring was done by
// the most expensive tier: the fix is the same subagent, pinned to a cheaper model.
func exploreOnTopModel(f *facts) *Finding {
	model, _ := mainModel(f.s)
	t := f.r.Tokens
	if tier(model) != tierTop || f.firstEdit < 0 || t.BeforeFirstEdit < explorationShare || f.callsFirst < minCallsBeforeEdit {
		return nil
	}
	tokens := int(t.BeforeFirstEdit * float64(t.Processed))
	return &Finding{
		Rule: "explore-on-top-model", Target: TargetModel, Confidence: "low",
		Title: fmt.Sprintf("Exploration ran on %s, the most expensive tier", model),
		Observation: fmt.Sprintf("%.0f%% of processed tokens (%s) and %d model calls came before the first edit, all on %s; %d file(s) were read by then.",
			t.BeforeFirstEdit*100, tok(tokens), f.callsFirst, model, f.r.Files.ReadBeforeFirstEdit),
		Hypothesis:     "Finding the relevant code is mostly reading and searching, which a smaller model does well; the top model is needed for the change itself.",
		Suggestion:     "Add an explorer subagent pinned to Haiku (or Sonnet) in .claude/agents/explorer.md and start similar tasks by delegating to it.",
		Snippet:        explorerAgent,
		Benefit:        "Exploration is billed at the smaller model's rate, and only its summary enters the main context.",
		Downside:       "A smaller model can miss subtle connections; if its summary is thin, switch the subagent to sonnet.",
		TokensInvolved: tokens,
		Evidence:       []int{f.firstEdit},
	}
}

func routineOnTopModel(f *facts) *Finding {
	model, calls := mainModel(f.s)
	c := f.r.Tools.ByCategory
	lookups, edits := c[analysis.CatRead]+c[analysis.CatSearch], c[analysis.CatEdit]
	commands := c[analysis.CatShell] + c[analysis.CatTest] + c[analysis.CatOther]
	if tier(model) != tierTop || calls > routineMaxCalls || lookups > routineMaxLookups || edits > routineMaxEdits || commands < routineMinCommands {
		return nil
	}
	first := -1
	for _, e := range f.s.Events {
		if e.Kind == session.KindUserPrompt && e.Agent == "" {
			first = e.Seq
			break
		}
	}
	if first < 0 {
		return nil
	}
	return &Finding{
		Rule: "routine-on-top-model", Target: TargetModel, Confidence: "low",
		Title: fmt.Sprintf("A routine session ran on %s", model),
		Observation: fmt.Sprintf("%d tool calls: %d commands (shell, git, tests), %d reads/searches, %d edits, over %d model calls on %s.",
			f.r.Tools.Calls, commands, lookups, edits, calls, model),
		Hypothesis:     "Mechanical steps like committing, opening a merge request or running tests rarely need the top model.",
		Suggestion:     "For routine work like this, switch with /model claude-sonnet-5-5 and switch back for design or debugging.",
		Snippet:        "/model claude-sonnet-5-5",
		Benefit:        "Sonnet 5.5 costs about half of Opus 5.5 per token ($2/$10 vs $4/$20 per 1M) and is usually faster.",
		Downside:       "If the 'routine' task turns out to need judgment, the smaller model may take more turns; switch back then.",
		TokensInvolved: f.r.Tokens.Processed,
		Evidence:       []int{first},
	}
}

func struggleOnSmallerModel(f *facts) *Finding {
	model, _ := mainModel(f.s)
	if k := tier(model); k != tierMid && k != tierSmall {
		return nil
	}
	r := f.r
	var signs []string
	var ev []int
	if r.Tests.Failed >= minFailedTests {
		signs = append(signs, fmt.Sprintf("%d of %d test runs failed", r.Tests.Failed, r.Tests.Runs))
		for _, ref := range r.Tests.RunRefs {
			ev = append(ev, ref.Seq)
		}
	}
	if r.Tools.Failed >= struggleMinFailed && float64(r.Tools.Failed) >= struggleFailShare*float64(r.Tools.Calls) {
		signs = append(signs, fmt.Sprintf("%d of %d tool calls failed", r.Tools.Failed, r.Tools.Calls))
		for _, ref := range r.Failures {
			ev = append(ev, ref.Seq)
		}
	}
	if len(r.Tokens.ByTurn) > 1 && r.Tokens.Processed > 0 {
		top := slices.MaxFunc(r.Tokens.ByTurn, func(a, b analysis.TurnTokens) int { return cmp.Compare(a.Processed, b.Processed) })
		if share := float64(top.Processed) / float64(r.Tokens.Processed); share >= heavyTurnShare && top.ModelCalls >= minHeavyTurnCalls {
			signs = append(signs, fmt.Sprintf("one request took %.0f%% of the session over %d model calls", share*100, top.ModelCalls))
			ev = append(ev, top.Seq)
		}
	}
	if len(signs) == 0 {
		return nil
	}
	slices.Sort(ev)
	return &Finding{
		Rule: "struggle-on-smaller-model", Target: TargetModel, Confidence: "low",
		Title:          fmt.Sprintf("The session struggled on %s", model),
		Observation:    fmt.Sprintf("On %s: %s.", model, strings.Join(signs, "; ")),
		Hypothesis:     "Repeated failures and long iterations can mean the task needed more reasoning than this model gave it.",
		Suggestion:     "For tasks like this, try /model claude-opus-5-5 (or a higher effort) and compare the next session.",
		Snippet:        "/model claude-opus-5-5",
		Benefit:        "Fewer retries can cost less overall than a cheaper model that needs many turns.",
		Downside:       "Opus 5.5 costs about twice Sonnet 5.5 per token; it pays off only if it cuts the retries. The cause may be the task or the prompt, not the model.",
		TokensInvolved: r.Tokens.Processed,
		Evidence:       slices.Compact(ev),
	}
}
