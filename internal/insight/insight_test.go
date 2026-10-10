package insight

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zakharkiv-vitalik/sexy-agent-tuner/internal/analysis"
	"github.com/zakharkiv-vitalik/sexy-agent-tuner/internal/session"
)

func rules(fs []Finding) []string {
	var out []string
	for _, f := range fs {
		out = append(out, f.Rule)
	}
	slices.Sort(out)
	return out
}

func TestGenerateValidTo9999(t *testing.T) {
	s, err := session.ImportClaudeFile("../session/testdata/valid_to_9999.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	fs := Generate(s, analysis.Analyze(s))

	want := []string{"broad-search", "exploration-heavy", "mcp-before-edit", "redundant-reads"}
	if got := rules(fs); !slices.Equal(got, want) {
		t.Fatalf("rules = %v, want %v", got, want)
	}
	for _, f := range fs {
		if f.Observation == "" || f.Hypothesis == "" || f.Suggestion == "" || f.Downside == "" || len(f.Evidence) == 0 {
			t.Errorf("%s: incomplete finding %+v", f.Rule, f)
		}
		if f.Confidence != "medium" && f.Confidence != "low" {
			t.Errorf("%s: confidence %q", f.Rule, f.Confidence)
		}
	}
	// Medium-confidence findings come before low-confidence ones.
	for i := 1; i < len(fs); i++ {
		if rank(fs[i-1].Confidence) > rank(fs[i].Confidence) {
			t.Errorf("order: %s (%s) before %s (%s)", fs[i-1].Rule, fs[i-1].Confidence, fs[i].Rule, fs[i].Confidence)
		}
	}
	byRule := map[string]Finding{}
	for _, f := range fs {
		byRule[f.Rule] = f
	}
	if s := byRule["broad-search"].Snippet; !strings.Contains(s, "internal/snapshot/") {
		t.Errorf("broad-search snippet should point at the most-read dir: %q", s)
	}
	if o := byRule["mcp-before-edit"].Observation; !strings.Contains(o, "postgres") || !strings.Contains(o, "validity.go") {
		t.Errorf("mcp-before-edit observation = %q", o)
	}
}

// builder assembles synthetic sessions for single-rule tests.
type builder struct {
	s   session.Session
	now time.Time
}

func newBuilder() *builder {
	b := &builder{now: time.Unix(1_700_000_000, 0)}
	b.s.Project = "/p"
	b.s.StartedAt = b.now
	return b
}

func (b *builder) add(e session.Event) {
	b.now = b.now.Add(10 * time.Second)
	e.Seq, e.Timestamp = len(b.s.Events), b.now
	b.s.Events = append(b.s.Events, e)
	b.s.FinishedAt = b.now
}

func (b *builder) prompt(text string) *builder {
	b.add(session.Event{Kind: session.KindUserPrompt, Text: text})
	return b
}

func (b *builder) model(context int) *builder {
	b.add(session.Event{Kind: session.KindModelCall, Usage: &session.Usage{CacheRead: context, Output: 100}})
	return b
}

func (b *builder) tool(name string, input map[string]any, isErr bool, outBytes int) *builder {
	in, _ := json.Marshal(input)
	b.model(50_000)
	b.add(session.Event{Kind: session.KindToolCall, Tool: &session.ToolCall{Name: name, Input: in, IsError: isErr, OutputBytes: outBytes, HasResult: true}})
	return b
}

func (b *builder) findings() []Finding {
	return Generate(&b.s, analysis.Analyze(&b.s))
}

func TestRules(t *testing.T) {
	bash := func(cmd string) map[string]any { return map[string]any{"command": cmd} }
	tests := []struct {
		name  string
		build func(b *builder)
		want  []string
	}{
		{
			name: "clean session produces nothing",
			build: func(b *builder) {
				b.prompt("fix x").tool("Read", map[string]any{"file_path": "/p/a.go"}, false, 400).
					tool("Edit", map[string]any{"file_path": "/p/a.go"}, false, 10).
					tool("Bash", bash("go test ./..."), false, 100)
			},
		},
		{
			name: "mcp heavy",
			build: func(b *builder) {
				b.prompt("do it")
				for i := range 35 {
					b.tool("mcp__chrome__click", map[string]any{"n": i}, false, 100)
				}
			},
			want: []string{"mcp-heavy"},
		},
		{
			name: "failing tests rerun without change",
			build: func(b *builder) {
				b.prompt("fix").tool("Edit", map[string]any{"file_path": "/p/a.go"}, false, 10).
					tool("Bash", bash("go test ./..."), true, 100).
					tool("Bash", bash("go test ./..."), true, 100)
			},
			want: []string{"test-failures"},
		},
		{
			name: "code edited, no tests",
			build: func(b *builder) {
				b.prompt("fix").tool("Edit", map[string]any{"file_path": "/p/a.go"}, false, 10).
					tool("Write", map[string]any{"file_path": "/p/README.md"}, false, 10)
			},
			want: []string{"no-tests"},
		},
		{
			name: "docs-only edit does not ask for tests",
			build: func(b *builder) {
				b.prompt("docs").tool("Write", map[string]any{"file_path": "/p/README.md"}, false, 10)
			},
		},
		{
			name: "large outputs",
			build: func(b *builder) {
				b.prompt("look")
				for _, f := range []string{"a", "b", "c"} {
					b.tool("Bash", bash("git log -p "+f), false, 40_000)
				}
			},
			want: []string{"large-outputs"},
		},
		{
			name: "repeated identical calls",
			build: func(b *builder) {
				b.prompt("look")
				for range 3 {
					b.tool("Bash", bash("git status"), false, 100)
				}
			},
			want: []string{"repeated-calls"},
		},
		{
			name: "scratch re-reads are ignored",
			build: func(b *builder) {
				b.prompt("x")
				for range 4 {
					b.tool("Read", map[string]any{"file_path": "/tmp/scratch.txt"}, false, 100)
				}
			},
			// neither redundant-reads (scratch) nor repeated-calls (reads have their own rule)
		},
		{
			name: "browser automation: repeated screenshots and early page reads are not flagged",
			build: func(b *builder) {
				b.prompt("check the ticket")
				for range 3 {
					b.tool("mcp__claude-in-chrome__get_page_text", map[string]any{"tabId": 1}, false, 100)
				}
				b.tool("Edit", map[string]any{"file_path": "/p/a.md"}, false, 10)
			},
		},
		{
			name: "one heavy turn",
			build: func(b *builder) {
				b.prompt("small").model(1000)
				b.prompt("big open-ended request")
				for range 12 {
					b.model(200_000)
				}
			},
			want: []string{"heavy-turn"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := newBuilder()
			tt.build(b)
			if got := rules(b.findings()); !slices.Equal(got, tt.want) {
				t.Errorf("rules = %v, want %v", got, tt.want)
			}
		})
	}
}
