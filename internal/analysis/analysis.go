// Package analysis derives deterministic facts from a normalized session.
// Everything here is computed, not inferred: interpretation belongs to the
// strategy and postmortem layers. Evidence is referenced by event Seq.
package analysis

import (
	"cmp"
	"encoding/json"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/zakharkiv-vitalik/sexy-agent-tuner/internal/session"
)

// LongRunning is the threshold above which a tool call is reported as slow.
const LongRunning = 30 * time.Second

type Report struct {
	SessionID string
	Duration  time.Duration
	Turns     int // human prompts
	Tokens    TokenStats
	Tools     ToolStats
	Files     FileStats
	Tests     TestStats
	MCP       MCPStats
	Subagents []Ref
	Skills    []Ref
	Failures  []Ref
	Timeline  []TimelineEntry
}

// Ref points at one event as evidence.
type Ref struct {
	Seq    int
	Offset time.Duration // since session start
	Label  string
}

type TokenStats struct {
	ModelCalls    int
	Input         int // uncached input
	CacheCreation int
	CacheRead     int
	Output        int
	Processed     int // input + cache + output summed over all calls
	PeakContext   int
	ByTurn        []TurnTokens
	Growth        []ContextPoint
	// BeforeFirstEdit is the share of processed tokens spent before the first
	// file modification: a proxy for exploration cost. -1 when nothing was edited.
	BeforeFirstEdit float64
}

type TurnTokens struct {
	Turn       int
	Seq        int // the prompt event
	Prompt     string
	ModelCalls int
	Output     int
	Processed  int
}

type ContextPoint struct {
	Seq     int
	Offset  time.Duration
	Context int
}

type ToolStats struct {
	Calls       int
	Failed      int
	ByName      []ToolCount
	ByCategory  map[Category]int
	Repeated    []Repeat // identical name+input issued more than once
	LongRunning []Ref
}

type ToolCount struct {
	Name     string
	Calls    int
	Failed   int
	Duration time.Duration
	// ResultTokens estimates context added by results (bytes/4).
	ResultTokens int
}

type Repeat struct {
	Key   string
	Count int
	Seqs  []int
}

type FileStats struct {
	UniqueRead          int
	Reads               int
	ReadBeforeFirstEdit int // unique files
	Edited              []string
	Directories         []string
	// RedundantReads are re-reads of a file that had not been edited since
	// the previous read. A re-read after an edit is legitimate and excluded.
	RedundantReads []Repeat
	BroadSearches  []Ref
}

type TestStats struct {
	Runs     int
	Failed   int
	Repeated int // runs of a command already run before
	RunRefs  []Ref
	// FirstEditToFirstTest is the gap between the first code change and the
	// first test run after it; zero when not applicable.
	FirstEditToFirstTest time.Duration
	Style                string // test-first | test-after | tests-only | no-test-changes
}

type MCPStats struct {
	Calls           int
	BeforeFirstEdit int
	ByTool          []ToolCount
	Repeated        []Repeat
	Refs            []MCPCall
}

// Relevance of an MCP call. Deterministic analysis alone cannot prove a call
// was useless, so everything starts as unknown.
type Relevance string

const (
	RelevanceUnknown       Relevance = "unknown"
	RelevanceRelevant      Relevance = "relevant"
	RelevancePotentiallyNo Relevance = "potentially_unnecessary"
)

type MCPCall struct {
	Ref
	Server, Tool string
	BeforeEdit   bool
	Relevance    Relevance
}

type TimelineEntry struct {
	Seq      int
	Offset   time.Duration
	Agent    string
	Kind     session.Kind
	Category Category
	Label    string
	IsError  bool
	Duration time.Duration
	Context  int // context size of the model call that produced this step
	// Name is a short tag for steps worth naming on the chart: the skill, the
	// subagent type, the MCP server, or the slash command. Empty otherwise.
	Name string
}

// Analyze computes the deterministic report for a session.
func Analyze(s *session.Session) *Report {
	r := &Report{SessionID: s.ID, Duration: s.FinishedAt.Sub(s.StartedAt)}
	r.Tools.ByCategory = map[Category]int{}
	r.Tokens.BeforeFirstEdit = -1

	var (
		byTool      = map[string]*ToolCount{}
		byMCP       = map[string]*ToolCount{}
		callKeys    = map[string][]int{}
		mcpKeys     = map[string][]int{}
		testCmds    = map[string]bool{}
		readSeqs    = map[string][]int{} // path -> seqs of redundant-read chains
		lastRead    = map[string]int{}   // path -> seq of last read
		lastEdit    = map[string]int{}   // path -> seq of last edit
		readBefore  = map[string]bool{}
		readAll     = map[string]bool{}
		dirs        = map[string]bool{}
		edited      = map[string]bool{}
		firstEdit   = -1
		firstEditAt time.Time
		firstTest   = -1
		firstCode   = -1 // first edit of non-test file
		tokensEdit  int
		ctx         int
		turn        *TurnTokens
	)
	offset := func(e session.Event) time.Duration {
		if e.Timestamp.IsZero() {
			return 0
		}
		return e.Timestamp.Sub(s.StartedAt)
	}

	for _, e := range s.Events {
		ref := Ref{Seq: e.Seq, Offset: offset(e)}
		switch e.Kind {
		case session.KindUserPrompt:
			if e.Agent != "" {
				continue // a subagent's task prompt, not a human turn
			}
			r.Turns++
			r.Tokens.ByTurn = append(r.Tokens.ByTurn, TurnTokens{Turn: r.Turns, Seq: e.Seq, Prompt: e.Text})
			turn = &r.Tokens.ByTurn[len(r.Tokens.ByTurn)-1]
			var name string
			if cmd, ok := strings.CutPrefix(e.Text, "/"); ok {
				name = "/" + strings.Fields(cmd + " ")[0]
			}
			r.Timeline = append(r.Timeline, TimelineEntry{Seq: e.Seq, Offset: ref.Offset, Kind: e.Kind, Label: e.Text, Name: name})

		case session.KindModelCall:
			u := e.Usage
			processed := u.Context() + u.Output
			t := &r.Tokens
			t.ModelCalls++
			t.Input += u.Input
			t.CacheCreation += u.CacheCreation
			t.CacheRead += u.CacheRead
			t.Output += u.Output
			t.Processed += processed
			if firstEdit < 0 {
				tokensEdit += processed
			}
			if e.Agent == "" {
				ctx = u.Context()
				t.PeakContext = max(t.PeakContext, ctx)
				t.Growth = append(t.Growth, ContextPoint{Seq: e.Seq, Offset: ref.Offset, Context: ctx})
			}
			if turn != nil {
				turn.ModelCalls++
				turn.Output += u.Output
				turn.Processed += processed
			}

		case session.KindAssistantText:
			r.Timeline = append(r.Timeline, TimelineEntry{Seq: e.Seq, Offset: ref.Offset, Agent: e.Agent, Kind: e.Kind, Label: e.Text, Context: ctx})

		case session.KindToolCall:
			t := e.Tool
			a := Classify(t, s.Project)
			ref.Label = t.Name + " " + a.Target
			if a.Category == CatMCP {
				ref.Label = a.Target
			}
			r.Timeline = append(r.Timeline, TimelineEntry{Seq: e.Seq, Offset: ref.Offset, Agent: e.Agent, Kind: e.Kind,
				Category: a.Category, Label: ref.Label, IsError: t.IsError, Duration: t.Duration, Context: ctx, Name: stepName(t, a)})

			r.Tools.Calls++
			r.Tools.ByCategory[a.Category]++
			tc := count(byTool, t.Name)
			tc.Calls++
			tc.Duration += t.Duration
			tc.ResultTokens += t.OutputBytes / 4
			if t.IsError {
				r.Tools.Failed++
				tc.Failed++
				r.Failures = append(r.Failures, withLabel(ref, ref.Label+": "+firstLine(t.Output)))
			}
			if t.Duration >= LongRunning {
				r.Tools.LongRunning = append(r.Tools.LongRunning, ref)
			}
			key := t.Name + " " + canonical(t.Input)
			callKeys[key] = append(callKeys[key], e.Seq)

			switch a.Category {
			case CatRead:
				for _, p := range a.Files {
					r.Files.Reads++
					readAll[p] = true
					dirs[filepath.Dir(p)] = true
					if firstEdit < 0 {
						readBefore[p] = true
					}
					if prev, ok := lastRead[p]; ok && !editedSince(lastEdit, p, prev) {
						if len(readSeqs[p]) == 0 {
							readSeqs[p] = []int{prev}
						}
						readSeqs[p] = append(readSeqs[p], e.Seq)
					}
					lastRead[p] = e.Seq
				}
			case CatSearch:
				if a.Broad {
					r.Files.BroadSearches = append(r.Files.BroadSearches, ref)
				}
			case CatEdit:
				for _, p := range a.Files {
					edited[p] = true
					lastEdit[p] = e.Seq
					if firstCode < 0 && !IsTestFile(p) {
						firstCode = e.Seq
					}
				}
				if firstEdit < 0 {
					firstEdit, firstEditAt = e.Seq, e.Timestamp
				}
			case CatTest:
				cmd := t.Str("command")
				r.Tests.Runs++
				r.Tests.RunRefs = append(r.Tests.RunRefs, ref)
				if t.IsError || strings.Contains(t.Output, "--- FAIL") || strings.Contains(t.Output, "FAILED") {
					r.Tests.Failed++
				}
				if testCmds[cmd] {
					r.Tests.Repeated++
				}
				testCmds[cmd] = true
				if firstTest < 0 && firstEdit >= 0 {
					firstTest = e.Seq
					r.Tests.FirstEditToFirstTest = e.Timestamp.Sub(firstEditAt)
				}
			case CatMCP:
				server, name, _ := t.MCP()
				mc := count(byMCP, server+"."+name)
				mc.Calls++
				mc.Duration += t.Duration
				mc.ResultTokens += t.OutputBytes / 4
				if t.IsError {
					mc.Failed++
				}
				mcpKeys[key] = append(mcpKeys[key], e.Seq)
				r.MCP.Calls++
				if firstEdit < 0 {
					r.MCP.BeforeFirstEdit++
				}
				r.MCP.Refs = append(r.MCP.Refs, MCPCall{Ref: ref, Server: server, Tool: name, BeforeEdit: firstEdit < 0, Relevance: RelevanceUnknown})
			case CatAgent:
				r.Subagents = append(r.Subagents, ref)
			case CatSkill:
				r.Skills = append(r.Skills, ref)
			}
		}
	}

	if firstEdit >= 0 && r.Tokens.Processed > 0 {
		r.Tokens.BeforeFirstEdit = float64(tokensEdit) / float64(r.Tokens.Processed)
	}
	r.Tools.ByName = sorted(byTool)
	r.Tools.Repeated = repeats(callKeys)
	r.MCP.ByTool = sorted(byMCP)
	r.MCP.Repeated = repeats(mcpKeys)
	r.Files.UniqueRead = len(readAll)
	r.Files.ReadBeforeFirstEdit = len(readBefore)
	r.Files.Edited = slices.Sorted(maps.Keys(edited))
	r.Files.Directories = slices.Sorted(maps.Keys(dirs))
	r.Files.RedundantReads = repeats(readSeqs)
	r.Tests.Style = testStyle(s, firstCode)
	return r
}

// testStyle compares when test files and code files were first modified.
func testStyle(s *session.Session, firstCode int) string {
	for _, e := range s.Events {
		if e.Kind != session.KindToolCall {
			continue
		}
		a := Classify(e.Tool, s.Project)
		if a.Category == CatEdit && len(a.Files) > 0 && IsTestFile(a.Files[0]) {
			if firstCode < 0 {
				return "tests-only"
			}
			if e.Seq < firstCode {
				return "test-first"
			}
			return "test-after"
		}
	}
	return "no-test-changes"
}

func count(m map[string]*ToolCount, name string) *ToolCount {
	if m[name] == nil {
		m[name] = &ToolCount{Name: name}
	}
	return m[name]
}

func sorted(m map[string]*ToolCount) []ToolCount {
	out := make([]ToolCount, 0, len(m))
	for _, v := range m {
		out = append(out, *v)
	}
	slices.SortFunc(out, func(a, b ToolCount) int {
		return cmp.Or(cmp.Compare(b.Calls, a.Calls), cmp.Compare(a.Name, b.Name))
	})
	return out
}

func repeats(m map[string][]int) []Repeat {
	var out []Repeat
	for k, seqs := range m {
		if len(seqs) > 1 {
			out = append(out, Repeat{Key: k, Count: len(seqs), Seqs: seqs})
		}
	}
	slices.SortFunc(out, func(a, b Repeat) int { return cmp.Or(cmp.Compare(b.Count, a.Count), cmp.Compare(a.Key, b.Key)) })
	return out
}

// canonical re-encodes JSON so key order does not affect equality.
func canonical(raw json.RawMessage) string {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return string(raw)
	}
	b, _ := json.Marshal(v) // maps marshal with sorted keys
	return string(b)
}

func editedSince(lastEdit map[string]int, path string, seq int) bool {
	e, ok := lastEdit[path]
	return ok && e > seq
}

func withLabel(r Ref, l string) Ref { r.Label = l; return r }

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}

// stepName tags skill, subagent and MCP steps for the timeline.
func stepName(t *session.ToolCall, a Action) string {
	switch a.Category {
	case CatSkill:
		return a.Target
	case CatAgent:
		kind, _, _ := strings.Cut(a.Target, ":")
		return kind
	case CatMCP:
		server, _, _ := t.MCP()
		return server
	}
	return ""
}
