package insight

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/zakharkiv-vitalik/sexy-agent-tuner/internal/session"
)

func TestAcross(t *testing.T) {
	read := func(p string) map[string]any { return map[string]any{"file_path": p} }
	bash := func(cmd string) map[string]any { return map[string]any{"command": cmd} }
	rule := "Always run go vet before you finish. Fix only the failing test."

	var ss []*session.Session
	for i := range 4 {
		b := newBuilder()
		b.s.ID = fmt.Sprint("s", i)
		prompt := fmt.Sprintf("task number %d please do it now", i)
		if i < 3 {
			prompt += "\n" + rule
		}
		b.prompt(prompt).
			tool("Read", read("/p/internal/core.go"), false, 400).
			tool("Bash", bash("make gen"), false, 10).
			tool("Bash", bash("make lint"), false, 10).
			tool("Bash", bash(fmt.Sprint("make run-", i)), false, 10).
			tool("Edit", read(fmt.Sprintf("/p/f%d.go", i)), false, 10)
		ss = append(ss, &b.s)
	}

	byRule := map[string][]Finding{}
	for _, f := range Across(ss) {
		byRule[f.Rule] = append(byRule[f.Rule], f)
		if len(f.Sessions) < minSessions || f.Observation == "" || f.Suggestion == "" || f.Snippet == "" {
			t.Errorf("%s: incomplete finding %+v", f.Rule, f)
		}
	}

	ins := byRule["repeated-instructions"]
	if len(ins) != 1 {
		t.Fatalf("repeated-instructions = %d findings, want 1 (both sentences co-occur)", len(ins))
	}
	if got := ins[0].Snippet; got != "always run go vet before you finish\nfix only the failing test" {
		t.Errorf("snippet = %q", got)
	}
	if ins[0].Target != TargetContext { // 3 of 4 sessions ≥ alwaysShare
		t.Errorf("target = %s, want context", ins[0].Target)
	}

	exp := byRule["repeated-exploration"]
	if len(exp) != 1 || !strings.Contains(exp[0].Snippet, "internal/core.go") || len(exp[0].Sessions) != 4 {
		t.Errorf("repeated-exploration = %+v", exp)
	}

	ch := byRule["repeated-chain"]
	if len(ch) != 1 || !strings.Contains(ch[0].Suggestion, "script") {
		t.Fatalf("repeated-chain = %+v", ch)
	}
	if !strings.Contains(ch[0].Snippet, "make gen") || !strings.Contains(ch[0].Snippet, "make lint") {
		t.Errorf("chain snippet = %q", ch[0].Snippet)
	}

	// Two sessions are not a pattern.
	if fs := Across(ss[:2]); len(fs) != 0 {
		t.Errorf("2 sessions produced %v", rules(fs))
	}
}

func TestSentences(t *testing.T) {
	got := sentences("Ok go. Please   ALWAYS run the tests!\n<pasted_content id=1>paste this long block of text here</pasted_content>use short commit messages please")
	want := []string{"please always run the tests", "use short commit messages please"}
	if !slices.Equal(got, want) {
		t.Errorf("sentences = %q, want %q", got, want)
	}
}
