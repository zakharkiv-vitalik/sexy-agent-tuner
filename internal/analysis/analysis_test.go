package analysis

import (
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/zakharkiv-vitalik/sexy-agent-tuner/internal/session"
)

func TestAnalyzeValidTo9999(t *testing.T) {
	s, err := session.ImportClaudeFile("../session/testdata/valid_to_9999.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	r := Analyze(s)
	const proj = "/work/snapshots-svc/internal/snapshot/"

	checks := []struct {
		name      string
		got, want any
	}{
		{"turns", r.Turns, 1},
		{"model calls", r.Tokens.ModelCalls, 18},
		{"tool calls", r.Tools.Calls, 17},
		{"failed tools", r.Tools.Failed, 1},
		{"mcp calls", r.MCP.Calls, 3},
		{"mcp before first edit", r.MCP.BeforeFirstEdit, 3},
		{"mcp repeated query", len(r.MCP.Repeated), 1},
		{"mcp relevance defaults to unknown", r.MCP.Refs[0].Relevance, RelevanceUnknown},
		{"test runs", r.Tests.Runs, 2},
		{"test failures", r.Tests.Failed, 1},
		{"repeated test command", r.Tests.Repeated, 1},
		{"test style", r.Tests.Style, "test-after"},
		{"unique files read", r.Files.UniqueRead, 3},
		// model.go read twice with no edit between; validity.go read by Read then by sed.
		// The re-read of validity.go after the edit is legitimate and not counted.
		{"redundant read files", len(r.Files.RedundantReads), 2},
		{"files read before first edit", r.Files.ReadBeforeFirstEdit, 3},
		{"edited", r.Files.Edited, []string{proj + "validity.go", proj + "validity_test.go"}},
		{"broad searches (Grep no path, Glob **, grep -rn .)", len(r.Files.BroadSearches), 3},
		{"long running (go test 35s, 30s)", len(r.Tools.LongRunning), 2},
		{"read category", r.Tools.ByCategory[CatRead], 6},
		{"exploration share in range", r.Tokens.BeforeFirstEdit > 0.4 && r.Tokens.BeforeFirstEdit < 0.8, true},
		{"edit→test gap", r.Tests.FirstEditToFirstTest > 0, true},
	}
	for _, c := range checks {
		if !equal(c.got, c.want) {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
	if r.Tokens.Processed != r.Tokens.Input+r.Tokens.CacheRead+r.Tokens.CacheCreation+r.Tokens.Output {
		t.Errorf("processed tokens do not add up: %+v", r.Tokens)
	}
	if len(r.Tokens.Growth) != r.Tokens.ModelCalls || r.Tokens.PeakContext != r.Tokens.Growth[len(r.Tokens.Growth)-1].Context {
		t.Errorf("growth series inconsistent: peak=%d last=%v", r.Tokens.PeakContext, r.Tokens.Growth[len(r.Tokens.Growth)-1])
	}
}

func equal(a, b any) bool {
	if as, ok := a.([]string); ok {
		return slices.Equal(as, b.([]string))
	}
	return a == b
}

func TestClassify(t *testing.T) {
	tool := func(name string, in map[string]any) *session.ToolCall {
		b, _ := json.Marshal(in)
		return &session.ToolCall{Name: name, Input: b}
	}
	tests := []struct {
		name  string
		call  *session.ToolCall
		cat   Category
		files []string
		broad bool
	}{
		{"read relative", tool("Read", map[string]any{"file_path": "a/b.go"}), CatRead, []string{"/p/a/b.go"}, false},
		{"mcp", tool("mcp__postgres__query", nil), CatMCP, nil, false},
		{"grep scoped", tool("Grep", map[string]any{"pattern": "x", "path": "internal"}), CatSearch, nil, false},
		{"grep repo-wide", tool("Grep", map[string]any{"pattern": "x"}), CatSearch, nil, true},
		{"glob scoped", tool("Glob", map[string]any{"pattern": "internal/**/*.go"}), CatSearch, nil, false},
		{"bash cat", tool("Bash", map[string]any{"command": "cat /x/y.go | head -20"}), CatRead, []string{"/x/y.go"}, false},
		{"bash sed -n", tool("Bash", map[string]any{"command": "sed -n '1,20p' f.go"}), CatRead, []string{"/p/f.go"}, false},
		{"bash sed -i is an edit", tool("Bash", map[string]any{"command": "sed -i '' 's/a/b/' f.go"}), CatEdit, []string{"/p/f.go"}, false},
		{"bash same file twice counted once", tool("Bash", map[string]any{"command": "sed -n '1,5p' f.go; sed -n '9,20p' f.go"}), CatRead, []string{"/p/f.go"}, false},
		{"bash heredoc write", tool("Bash", map[string]any{"command": "cat > t/x_test.py <<'PY'\ndef test(): pass\nPY"}), CatEdit, []string{"/p/t/x_test.py"}, false},
		{"bash inline script writing files", tool("Bash", map[string]any{"command": "cd /p && python3 - <<'PY'\nopen(p, 'w').write(s)\nPY"}), CatEdit, nil, false},
		{"bash inline script only reading", tool("Bash", map[string]any{"command": "python3 - <<'PY'\nprint(open(p).read())\nPY"}), CatShell, nil, false},
		{"bash stderr redirect is not a write", tool("Bash", map[string]any{"command": "ls x 2>/dev/null 2>&1"}), CatSearch, nil, false},
		{"bash quoted redirect target ignored", tool("Bash", map[string]any{"command": `echo x > "$F"`}), CatShell, nil, false},
		{"bash rm", tool("Bash", map[string]any{"command": "rm tests/a_test.go"}), CatEdit, []string{"/p/tests/a_test.go"}, false},
		{"scratch write is not a project edit", tool("Bash", map[string]any{"command": "cat > /tmp/x.txt <<EOF\nhi\nEOF"}), CatShell, []string{"/tmp/x.txt"}, false},
		{"plan file", tool("Write", map[string]any{"file_path": "/u/.claude/plans/p.md"}), CatPlan, []string{"/u/.claude/plans/p.md"}, false},
		{"bash grep piped pattern", tool("Bash", map[string]any{"command": `grep -rn 'a\|b' .`}), CatSearch, nil, true},
		{"bash grep scoped", tool("Bash", map[string]any{"command": "grep -rn foo internal/"}), CatSearch, nil, false},
		{"bash find root", tool("Bash", map[string]any{"command": "find . -name '*.go'"}), CatSearch, nil, true},
		{"go test", tool("Bash", map[string]any{"command": "cd x && go test ./..."}), CatTest, nil, false},
		{"pytest", tool("Bash", map[string]any{"command": "python -m pytest -q"}), CatTest, nil, false},
		{"git is shell", tool("Bash", map[string]any{"command": "git status"}), CatShell, nil, false},
		{"subagent", tool("Agent", map[string]any{"subagent_type": "Explore", "description": "find"}), CatAgent, nil, false},
		{"write", tool("Write", map[string]any{"file_path": "/p/x_test.go"}), CatEdit, []string{"/p/x_test.go"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := Classify(tt.call, "/p")
			if a.Category != tt.cat || !slices.Equal(a.Files, tt.files) || a.Broad != tt.broad {
				t.Errorf("Classify = %+v, want cat=%s files=%v broad=%v", a, tt.cat, tt.files, tt.broad)
			}
		})
	}
}

func TestRedundantReadsIgnoreRereadAfterEdit(t *testing.T) {
	ev := func(seq int, name, path string) session.Event {
		in, _ := json.Marshal(map[string]string{"file_path": path})
		return session.Event{Seq: seq, Kind: session.KindToolCall, Timestamp: time.Unix(int64(seq), 0),
			Tool: &session.ToolCall{Name: name, Input: in}}
	}
	tests := []struct {
		name   string
		events []session.Event
		want   int
	}{
		{"read read", []session.Event{ev(0, "Read", "/a"), ev(1, "Read", "/a")}, 1},
		{"read edit read", []session.Event{ev(0, "Read", "/a"), ev(1, "Edit", "/a"), ev(2, "Read", "/a")}, 0},
		{"different files", []session.Event{ev(0, "Read", "/a"), ev(1, "Read", "/b")}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := Analyze(&session.Session{Events: tt.events})
			if got := len(r.Files.RedundantReads); got != tt.want {
				t.Errorf("redundant = %d, want %d (%+v)", got, tt.want, r.Files.RedundantReads)
			}
		})
	}
}
