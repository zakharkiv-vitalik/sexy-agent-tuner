// Package insight turns deterministic facts into improvement suggestions.
//
// Every finding separates what was measured (Observation, a FACT) from what it
// might mean (Hypothesis). Suggestions are things to try and compare, not
// proven improvements: one session never establishes causality.
package insight

import (
	"cmp"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/zakharkiv-vitalik/sexy-agent-tuner/internal/analysis"
	"github.com/zakharkiv-vitalik/sexy-agent-tuner/internal/session"
)

// Target is what the user would change to act on a finding.
type Target string

const (
	TargetPrompt   Target = "prompt"
	TargetContext  Target = "context" // CLAUDE.md / AGENTS.md
	TargetMCP      Target = "mcp"
	TargetWorkflow Target = "workflow"
	TargetTests    Target = "tests"
	TargetOutput   Target = "tool-output"
)

type Finding struct {
	Rule        string `json:"rule"`
	Target      Target `json:"target"`
	Title       string `json:"title"`
	Observation string `json:"observation"` // measured fact
	Hypothesis  string `json:"hypothesis"`  // interpretation, unproven
	Suggestion  string `json:"suggestion"`
	Snippet     string `json:"snippet"` // copyable text for a prompt or CLAUDE.md
	Benefit     string `json:"benefit"`
	Downside    string `json:"downside"`
	Confidence  string `json:"confidence"` // medium | low
	// TokensInvolved estimates the tokens tied to the pattern. It orders
	// findings; it is not a promised saving.
	TokensInvolved int   `json:"tokensInvolved"`
	Evidence       []int `json:"evidence"`
	// Sessions is the evidence of cross-session findings (see Across).
	Sessions []SessionRef `json:"sessions,omitempty"`
}

// Thresholds. Deliberately conservative: a quiet list beats a noisy one.
const (
	minExtraReads         = 2
	minBroadSearches      = 2
	explorationShare      = 0.5
	minCallsBeforeEdit    = 8
	minMCPBeforeEdit      = 3
	mcpHeavyCalls         = 30
	mcpHeavyShare         = 0.4
	minRepeatedCalls      = 2
	minFailedTests        = 2
	heavyTurnShare        = 0.4
	minHeavyTurnCalls     = 10
	largeOutputBytes      = 20_000 // ≈5k tokens
	minLargeOutputs       = 3
	freshMinContext       = 150_000 // conversation size at the prompt worth a fresh start
	freshMinCalls         = 8
	freshMinWords         = 6   // shorter prompts are follow-ups ("yes, do it"), not new tasks
	freshMinShare         = 0.2 // of the session's processed tokens
	freshMinCarried       = 2_000_000
	maxEvidencePerFinding = 20
)

// facts is precomputed per-session context shared by the rules.
type facts struct {
	s          *session.Session
	r          *analysis.Report
	actions    map[int]analysis.Action // tool-call seq -> classification
	firstEdit  int                     // seq, -1 if none
	callsFirst int                     // model calls before first edit
}

// Generate runs every rule and returns findings, most solid and costly first.
func Generate(s *session.Session, r *analysis.Report) []Finding {
	f := &facts{s: s, r: r, actions: map[int]analysis.Action{}, firstEdit: -1}
	for _, e := range s.Events {
		switch {
		case e.Kind == session.KindToolCall:
			a := analysis.Classify(e.Tool, s.Project)
			f.actions[e.Seq] = a
			if a.Category == analysis.CatEdit && f.firstEdit < 0 {
				f.firstEdit = e.Seq
			}
		case e.Kind == session.KindModelCall && f.firstEdit < 0:
			f.callsFirst++
		}
	}

	var out []Finding
	for _, rule := range []func(*facts) *Finding{
		redundantReads, broadSearches, explorationHeavy, mcpBeforeEdit, mcpHeavy,
		repeatedCalls, testFailures, noTests, heavyTurn, largeOutputs, freshSession,
		outdatedModel, exploreOnTopModel, routineOnTopModel, struggleOnSmallerModel,
	} {
		if fd := rule(f); fd != nil {
			if len(fd.Evidence) > maxEvidencePerFinding {
				fd.Evidence = fd.Evidence[:maxEvidencePerFinding]
			}
			out = append(out, *fd)
		}
	}
	slices.SortStableFunc(out, func(a, b Finding) int {
		return cmp.Or(cmp.Compare(rank(a.Confidence), rank(b.Confidence)), cmp.Compare(b.TokensInvolved, a.TokensInvolved))
	})
	return out
}

func rank(confidence string) int {
	if confidence == "medium" {
		return 0
	}
	return 1
}

func redundantReads(f *facts) *Finding {
	var extra, tokens int
	var files []string
	var ev []int
	for _, rep := range f.r.Files.RedundantReads {
		if analysis.IsScratch(rep.Key) {
			continue // the agent's own scratch files are not a context problem
		}
		extra += rep.Count - 1
		files = append(files, f.rel(rep.Key))
		ev = append(ev, rep.Seqs...)
		for _, seq := range rep.Seqs[1:] {
			tokens += f.resultTokens(seq)
		}
	}
	if extra < minExtraReads {
		return nil
	}
	top := files[:min(3, len(files))]
	var snippet strings.Builder
	snippet.WriteString("Key files for this area:\n")
	for _, p := range top {
		fmt.Fprintf(&snippet, "- %s — <what it is responsible for>\n", p)
	}
	return &Finding{
		Rule: "redundant-reads", Target: TargetContext, Confidence: "medium",
		Title:          "Re-read the same files without changing them",
		Observation:    fmt.Sprintf("%d file(s) were read again with no edit in between (%d extra reads): %s.", len(files), extra, strings.Join(top, ", ")),
		Hypothesis:     "The agent lost track of what it had already seen, often because the context grew long or the role of these files wasn't clear up front.",
		Suggestion:     "Name the key files and what each one does in the prompt, or keep a short module map in CLAUDE.md / AGENTS.md.",
		Snippet:        strings.TrimSpace(snippet.String()),
		Benefit:        fmt.Sprintf("≈%s tokens of repeated file content in this session.", tok(tokens)),
		Downside:       "A stale module map misleads the agent. Keep it short and update it when the code moves.",
		TokensInvolved: tokens,
		Evidence:       ev,
	}
}

func broadSearches(f *facts) *Finding {
	var ev []int
	tokens := 0
	for _, ref := range f.r.Files.BroadSearches {
		if f.beforeEdit(ref.Seq) {
			ev = append(ev, ref.Seq)
			tokens += f.resultTokens(ref.Seq)
		}
	}
	if len(ev) < minBroadSearches {
		return nil
	}
	start := f.mostReadDirBeforeEdit()
	if start == "" {
		start = "<module or package>"
	}
	return &Finding{
		Rule: "broad-search", Target: TargetPrompt, Confidence: "medium",
		Title:          "Searched the whole repository before knowing where to look",
		Observation:    fmt.Sprintf("%d repo-wide searches (no path, ** globs, grep -r .) happened %s.", len(ev), f.editPhrase()),
		Hypothesis:     "The prompt didn't say which module, layer or entry point the problem lives in, so the agent had to find it.",
		Suggestion:     "Tell the agent where to start, or ask it to trace the value from a known entry point instead of searching everywhere.",
		Snippet:        fmt.Sprintf("Start in %s. Trace the value from its entry point to the output before searching the rest of the repository.", start),
		Benefit:        fmt.Sprintf("≈%s tokens of search results in this session; fewer detours before the first hypothesis.", tok(tokens)),
		Downside:       "Pointing at the wrong place anchors the agent. Say \"start in\", not \"only in\".",
		TokensInvolved: tokens,
		Evidence:       ev,
	}
}

func explorationHeavy(f *facts) *Finding {
	t := f.r.Tokens
	if f.firstEdit < 0 || t.BeforeFirstEdit < explorationShare || f.callsFirst < minCallsBeforeEdit {
		return nil
	}
	if m, _ := mainModel(f.s); tier(m) == tierTop {
		return nil // exploreOnTopModel gives the same advice with a model-specific fix
	}
	tokens := int(t.BeforeFirstEdit * float64(t.Processed))
	return &Finding{
		Rule: "exploration-heavy", Target: TargetWorkflow, Confidence: "low",
		Title: "Most of the budget went to exploration before the first change",
		Observation: fmt.Sprintf("%.0f%% of processed tokens (%s) and %d model calls came before the first edit; %d file(s) were read by then.",
			t.BeforeFirstEdit*100, tok(tokens), f.callsFirst, f.r.Files.ReadBeforeFirstEdit),
		Hypothesis: "A separate exploration step could hand the main agent a compact summary (relevant files, data flow, hypothesis) instead of the main agent carrying every file it looked at.",
		Suggestion: "On a similar task, try an Explorer subagent or a plan-first step, then compare the two sessions.",
		Snippet: "First use an Explore subagent to find the code involved and how the data flows through it. " +
			"Have it report: relevant files, the data flow, and its hypothesis for the cause. Then implement the fix.",
		Benefit:        "The main agent's context stays smaller for everything after exploration.",
		Downside:       "The subagent spends its own tokens (often 10–30k). On small tasks that is pure overhead, so test it before adopting it.",
		TokensInvolved: tokens,
		Evidence:       []int{f.firstEdit},
	}
}

// isBrowser reports browser-automation MCP servers. Their calls observe a page
// that changes between calls, so identical inputs are not redundant, and
// opening a page (e.g. a ticket) is not premature data investigation.
// ponytail: name heuristic; make it a user setting if servers get renamed.
func isBrowser(server string) bool {
	s := strings.ToLower(server)
	return strings.Contains(s, "chrome") || strings.Contains(s, "browser") || strings.Contains(s, "playwright") || strings.Contains(s, "puppeteer")
}

func mcpBeforeEdit(f *facts) *Finding {
	if f.firstEdit < 0 || f.r.MCP.BeforeFirstEdit < minMCPBeforeEdit {
		return nil
	}
	byServer := map[string][]int{}
	for _, c := range f.r.MCP.Refs {
		if c.BeforeEdit && !isBrowser(c.Server) {
			byServer[c.Server] = append(byServer[c.Server], c.Seq)
		}
	}
	server := ""
	for s, seqs := range byServer {
		if len(seqs) > len(byServer[server]) || (len(seqs) == len(byServer[server]) && s < server) {
			server = s
		}
	}
	ev := byServer[server]
	if len(ev) < minMCPBeforeEdit {
		return nil
	}
	tokens := 0
	for _, seq := range ev {
		tokens += f.resultTokens(seq)
	}
	edited := f.r.Files.Edited
	fix := "code"
	if len(edited) > 0 {
		fix = f.rel(edited[0])
	}
	return &Finding{
		Rule: "mcp-before-edit", Target: TargetMCP, Confidence: "low",
		Title:       fmt.Sprintf("Queried %s before forming a code-level hypothesis", server),
		Observation: fmt.Sprintf("%d %s call(s) happened before the first edit; the change was eventually made in %s.", len(ev), server, fix),
		Hypothesis: "If the cause was in application code, these queries may have been premature. They may also be what ruled the data out. " +
			"Metrics alone can't tell which, so relevance stays unknown.",
		Suggestion:     "Ask the agent to state a hypothesis before it queries external systems.",
		Snippet:        fmt.Sprintf("Before querying %s, state what you expect to find and how the answer would change your plan. Trace the value through the application code first.", server),
		Benefit:        "Fewer exploratory external calls when the answer is in the code.",
		Downside:       "For real data problems this gate slows the agent down.",
		TokensInvolved: tokens,
		Evidence:       ev,
	}
}

func mcpHeavy(f *facts) *Finding {
	byServer := map[string][]int{}
	for _, c := range f.r.MCP.Refs {
		byServer[c.Server] = append(byServer[c.Server], c.Seq)
	}
	server := ""
	for s, seqs := range byServer {
		if len(seqs) > len(byServer[server]) {
			server = s
		}
	}
	ev := byServer[server]
	if len(ev) < mcpHeavyCalls || float64(len(ev)) < mcpHeavyShare*float64(f.r.Tools.Calls) {
		return nil
	}
	// Each step re-sends the whole context, so the cost of a step-by-step
	// routine is the context carried on every call, not just the results.
	ctx := map[int]int{}
	for _, e := range f.r.Timeline {
		ctx[e.Seq] = e.Context
	}
	tokens := 0
	for _, seq := range ev {
		tokens += ctx[seq]
	}
	return &Finding{
		Rule: "mcp-heavy", Target: TargetWorkflow, Confidence: "medium",
		Title:       fmt.Sprintf("Most of the work went step by step through %s", server),
		Observation: fmt.Sprintf("%d of %d tool calls went to %s.", len(ev), f.r.Tools.Calls, server),
		Hypothesis: "Repetitive step-by-step actions through an MCP server are expensive, because every step re-sends the whole context. " +
			"A script, API call or skill might do the same routine in a few steps.",
		Suggestion:     "If this is a routine you repeat, have the agent write a script for it once, or turn it into a skill.",
		Snippet:        "This is a repetitive routine. Write a script that performs it end to end, run it, and only inspect the failures.",
		Benefit:        fmt.Sprintf("≈%s tokens of context were processed across these steps.", tok(tokens)),
		Downside:       "Scripts need access (auth, API) the agent may not have, and some UI-only flows can't be scripted.",
		TokensInvolved: tokens,
		Evidence:       ev,
	}
}

func repeatedCalls(f *facts) *Finding {
	extra, tokens := 0, 0
	var ev []int
	for _, rep := range f.r.Tools.Repeated {
		cat := f.actions[rep.Seqs[0]].Category
		if cat == analysis.CatRead || cat == analysis.CatTest {
			continue // re-reads and test reruns have their own rules
		}
		if server, _, ok := f.s.Events[rep.Seqs[0]].Tool.MCP(); ok && isBrowser(server) {
			continue
		}
		extra += rep.Count - 1
		ev = append(ev, rep.Seqs...)
		for _, seq := range rep.Seqs[1:] {
			tokens += f.resultTokens(seq)
		}
	}
	if extra < minRepeatedCalls {
		return nil
	}
	return &Finding{
		Rule: "repeated-calls", Target: TargetWorkflow, Confidence: "medium",
		Title:          "Ran the exact same tool call more than once",
		Observation:    fmt.Sprintf("%d call(s) repeated an earlier call with identical input.", extra),
		Hypothesis:     "The agent didn't keep the earlier result in view, which usually happens once the context gets long.",
		Suggestion:     "Ask the agent to write down key findings (a todo list or scratch note) instead of re-running commands.",
		Snippet:        "Record important command results in a short scratch note and reuse them instead of re-running the same command.",
		Benefit:        fmt.Sprintf("≈%s tokens of duplicate results.", tok(tokens)),
		Downside:       "Re-running is correct when the underlying state changed; the rule only sees identical inputs.",
		TokensInvolved: tokens,
		Evidence:       ev,
	}
}

func testFailures(f *facts) *Finding {
	t := f.r.Tests
	if t.Failed < minFailedTests {
		return nil
	}
	var ev []int
	reruns := 0
	var prevFailedCmd string
	var editedSince bool
	for _, e := range f.s.Events {
		a, ok := f.actions[e.Seq]
		if !ok {
			continue
		}
		switch a.Category {
		case analysis.CatEdit:
			editedSince = true
		case analysis.CatTest:
			ev = append(ev, e.Seq)
			cmd := e.Tool.Str("command")
			if cmd == prevFailedCmd && !editedSince {
				reruns++
			}
			prevFailedCmd, editedSince = "", false
			if e.Tool.IsError {
				prevFailedCmd = cmd
			}
		}
	}
	obs := fmt.Sprintf("%d of %d test runs failed.", t.Failed, t.Runs)
	if reruns > 0 {
		obs += fmt.Sprintf(" %d time(s) a failed test was re-run with no code change in between.", reruns)
	}
	return &Finding{
		Rule: "test-failures", Target: TargetTests, Confidence: "medium",
		Title:          "Repeated fix → test → fail cycles",
		Observation:    obs,
		Hypothesis:     "Cycles like this often mean fixes were attempted before the failure was understood.",
		Suggestion:     "Ask the agent to explain each failure before changing code.",
		Snippet:        "When a test fails, explain the cause before changing any code. Don't re-run a failing test without a change.",
		Benefit:        "Fewer blind retries; each run tests a specific idea.",
		Downside:       "Adds a short reasoning step to each failure.",
		TokensInvolved: 0,
		Evidence:       ev,
	}
}

var codeExt = map[string]bool{".go": true, ".py": true, ".java": true, ".kt": true, ".ts": true, ".tsx": true, ".js": true,
	".jsx": true, ".rs": true, ".rb": true, ".cs": true, ".php": true, ".scala": true, ".swift": true, ".c": true, ".cpp": true, ".sql": true}

func noTests(f *facts) *Finding {
	if f.r.Tests.Runs > 0 {
		return nil
	}
	var code []string
	var ev []int
	for _, e := range f.s.Events {
		a, ok := f.actions[e.Seq]
		if !ok || a.Category != analysis.CatEdit {
			continue
		}
		for _, p := range a.Files {
			if codeExt[filepath.Ext(p)] {
				code = append(code, f.rel(p))
				ev = append(ev, e.Seq)
			}
		}
	}
	if len(code) == 0 {
		return nil
	}
	slices.Sort(code)
	code = slices.Compact(code)
	return &Finding{
		Rule: "no-tests", Target: TargetTests, Confidence: "medium",
		Title:          "Code changed but no tests were run",
		Observation:    fmt.Sprintf("%d code file(s) changed (%s) and no test command ran in this session.", len(code), strings.Join(code[:min(3, len(code))], ", ")),
		Hypothesis:     "Nothing verified the change inside the session.",
		Suggestion:     "Put the verification command in the prompt or in CLAUDE.md so the agent runs it before finishing.",
		Snippet:        "After changing code, run the tests for the affected package (<test command>) and fix failures before finishing.",
		Benefit:        "Regressions are caught by the agent instead of by you later.",
		Downside:       "Slow suites add minutes. Point at the narrowest relevant package.",
		TokensInvolved: 0,
		Evidence:       ev,
	}
}

func heavyTurn(f *facts) *Finding {
	t := f.r.Tokens
	if len(t.ByTurn) < 2 || t.Processed == 0 {
		return nil
	}
	top := slices.MaxFunc(t.ByTurn, func(a, b analysis.TurnTokens) int { return cmp.Compare(a.Processed, b.Processed) })
	share := float64(top.Processed) / float64(t.Processed)
	if share < heavyTurnShare || top.ModelCalls < minHeavyTurnCalls {
		return nil
	}
	return &Finding{
		Rule: "heavy-turn", Target: TargetPrompt, Confidence: "low",
		Title:          fmt.Sprintf("One request consumed %.0f%% of the session", share*100),
		Observation:    fmt.Sprintf("Turn %d (%q) used %s tokens across %d model calls.", top.Turn, short(top.Prompt, 80), tok(top.Processed), top.ModelCalls),
		Hypothesis:     "The request was open-ended or didn't state the expected result, so the agent iterated to find out what \"done\" means.",
		Suggestion:     "For similar requests, state the goal, the expected result and when the task is done, or split it into steps.",
		Snippet:        "Goal: <what should change>\nExpected result: <observable outcome>\nDone when: <check that proves it>\nConstraints: <what not to touch>",
		Benefit:        "Less iteration spent discovering the goal.",
		Downside:       "Some tasks are exploratory by nature. Over-specifying can hide a better solution.",
		TokensInvolved: top.Processed,
		Evidence:       []int{top.Seq},
	}
}

// freshSession finds a substantial prompt sent on a large context: every model
// call after it re-sends the earlier conversation. What that cost is measured;
// whether the prompt was really a new task is the hypothesis.
func freshSession(f *facts) *Finding {
	type candidate struct {
		seq, ctx, calls, carried int
		text                     string
	}
	var (
		best              candidate
		cur               *candidate
		baseline, lastCtx int
		first             = true
	)
	closeCur := func() {
		if cur != nil && cur.carried > best.carried && cur.ctx >= freshMinContext && cur.calls >= freshMinCalls {
			best = *cur
		}
		cur = nil
	}
	for _, e := range f.s.Events {
		if e.Agent != "" {
			continue // subagents have their own context
		}
		switch e.Kind {
		case session.KindUserPrompt:
			if strings.HasPrefix(e.Text, "/") {
				if strings.HasPrefix(e.Text, "/clear") || strings.HasPrefix(e.Text, "/compact") {
					closeCur()
				}
				continue
			}
			if len(strings.Fields(e.Text)) < freshMinWords {
				continue
			}
			closeCur()
			if !first {
				cur = &candidate{seq: e.Seq, ctx: lastCtx, text: e.Text}
			}
			first = false
		case session.KindModelCall:
			c := e.Usage.Context()
			if baseline == 0 {
				baseline = c // what a fresh session starts with: system prompt, tools, CLAUDE.md
			}
			if cur != nil {
				if c < cur.ctx*6/10 {
					closeCur() // the context was reset or compacted
				} else {
					cur.calls++
					cur.carried += max(cur.ctx-baseline, 0)
				}
			}
			lastCtx = c
		}
	}
	closeCur()
	processed := f.r.Tokens.Processed
	if best.carried < freshMinCarried || float64(best.carried) < freshMinShare*float64(processed) {
		return nil
	}
	return &Finding{
		Rule: "fresh-session", Target: TargetWorkflow, Confidence: "low",
		Title: fmt.Sprintf("The conversation outgrew a fresh start (%s tokens carried on every step)", tok(best.ctx)),
		Observation: fmt.Sprintf("%q was sent when the context was already %s tokens (a fresh session starts at %s). "+
			"The %d model calls after it re-sent that history: ≈%s tokens, %.0f%% of the session.",
			short(best.text, 80), tok(best.ctx), tok(baseline), best.calls, tok(best.carried), 100*float64(best.carried)/float64(max(processed, 1))),
		Hypothesis: "Most of that history was likely no longer needed for this request: it was paid for on every step and " +
			"is a source of stale assumptions.",
		Suggestion:     "New task: start a new session (or /clear) with a short handover. Same task, long history: /compact first.",
		Snippet:        "Context: <one line on what was just done>\nTask: <the new request>\nRelevant files: <paths>\nDone when: <check>",
		Benefit:        fmt.Sprintf("Up to ≈%s fewer tokens for a request like this; each step starts near %s instead of %s.", tok(best.carried), tok(baseline), tok(best.ctx)),
		Downside:       "If the request builds on the previous work, a fresh session has to rediscover it. A short handover note keeps the useful part.",
		TokensInvolved: best.carried,
		Evidence:       []int{best.seq},
	}
}

func largeOutputs(f *facts) *Finding {
	var ev []int
	tokens := 0
	for _, e := range f.s.Events {
		if e.Kind == session.KindToolCall && e.Tool.OutputBytes >= largeOutputBytes {
			ev = append(ev, e.Seq)
			tokens += e.Tool.OutputBytes / 4
		}
	}
	if len(ev) < minLargeOutputs {
		return nil
	}
	return &Finding{
		Rule: "large-outputs", Target: TargetOutput, Confidence: "medium",
		Title:          "Large tool outputs pulled into the context",
		Observation:    fmt.Sprintf("%d tool results were over ~5k tokens each (≈%s in total).", len(ev), tok(tokens)),
		Hypothesis:     "Once in the context, these tokens are re-read on every later model call.",
		Suggestion:     "Ask the agent to keep outputs short: head/tail/grep, --stat or -c flags, and reading files by line range.",
		Snippet:        "Keep command output short: pipe through head/tail/grep, prefer --stat or -c flags, and read large files by line ranges.",
		Benefit:        fmt.Sprintf("≈%s tokens that stayed in the context afterwards.", tok(tokens)),
		Downside:       "Over-truncated output can hide the line that mattered.",
		TokensInvolved: tokens,
		Evidence:       ev,
	}
}

// --- helpers ---

func (f *facts) beforeEdit(seq int) bool { return f.firstEdit < 0 || seq < f.firstEdit }

func (f *facts) editPhrase() string {
	if f.firstEdit < 0 {
		return "in this session (no files were edited)"
	}
	return "before the first edit"
}

func (f *facts) resultTokens(seq int) int {
	if seq < 0 || seq >= len(f.s.Events) || f.s.Events[seq].Tool == nil {
		return 0
	}
	return f.s.Events[seq].Tool.OutputBytes / 4
}

func (f *facts) mostReadDirBeforeEdit() string {
	count := map[string]int{}
	for seq, a := range f.actions {
		if a.Category == analysis.CatRead && f.beforeEdit(seq) {
			for _, p := range a.Files {
				if d := filepath.Dir(p); !analysis.IsScratch(p) && d != f.s.Project {
					count[d]++
				}
			}
		}
	}
	best := ""
	for d, n := range count {
		if n > count[best] || (n == count[best] && d < best) {
			best = d
		}
	}
	if best == "" {
		return ""
	}
	return f.rel(best) + "/"
}

func (f *facts) rel(p string) string {
	if r, ok := strings.CutPrefix(p, f.s.Project+"/"); ok && f.s.Project != "" {
		return r
	}
	return p
}

func tok(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1000:
		return fmt.Sprintf("%.1fk", float64(n)/1e3)
	}
	return fmt.Sprint(n)
}

var pastedTag = regexp.MustCompile(`</?pasted_content[^>]*>`)

func short(s string, n int) string {
	s = strings.Join(strings.Fields(pastedTag.ReplaceAllString(s, " ")), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}
