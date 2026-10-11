package llm

import (
	"strings"
	"testing"
	"time"

	"github.com/zakharkiv-vitalik/sexy-agent-tuner/internal/insight"
	"github.com/zakharkiv-vitalik/sexy-agent-tuner/internal/storage"
)

func TestRender(t *testing.T) {
	t0 := time.Unix(1_700_000_000, 0)
	sessions := map[string]storage.SessionSummary{
		"old-session": {ID: "old-session", Project: "/r/a", StartedAt: t0, Tokens: 1_500_000},
		"new-session": {ID: "new-session", Project: "/r/b", StartedAt: t0.Add(time.Hour), Tokens: 900},
	}
	// Newest first, as ListPrompts returns them.
	prompts := []storage.Prompt{
		{SessionID: "new-session", Seq: 4, Text: "second <pasted_content id=\"1\">pasted</pasted_content>"},
		{SessionID: "new-session", Seq: 1, Text: "first"},
		{SessionID: "old-session", Seq: 0, Text: strings.Repeat("я", maxPromptChars+50)},
	}
	in := render(prompts, sessions, true)

	if in.total != 3 || len(in.ids) != 3 {
		t.Fatalf("total=%d ids=%d, want 3/3", in.total, len(in.ids))
	}
	// Oldest session first; prompts chronological inside a session.
	if a, b := strings.Index(in.text, "old-sess"), strings.Index(in.text, "new-sess"); a < 0 || b < a {
		t.Errorf("session order wrong:\n%s", in.text)
	}
	if in.ids["p2"] != (insight.SessionRef{Session: "new-session", Seq: 1}) || in.ids["p3"].Seq != 4 {
		t.Errorf("ids = %v", in.ids)
	}
	for _, want := range []string{"1.5M tokens", "repo a", "p3: second pasted", "…"} {
		if !strings.Contains(in.text, want) {
			t.Errorf("rendered text lacks %q:\n%s", want, in.text)
		}
	}
	if strings.Contains(in.text, "pasted_content") {
		t.Error("pasted_content tags should be stripped")
	}

	// Over budget: the newest prompts are kept, the count says how many.
	var many []storage.Prompt
	for i := range 400 {
		many = append(many, storage.Prompt{SessionID: "new-session", Seq: 1000 - i, Text: strings.Repeat("x", maxPromptChars)})
	}
	in = render(many, sessions, false)
	if len(in.ids) >= 400 || len(in.ids) == 0 || in.total != 400 {
		t.Errorf("budget: analyzed %d of %d", len(in.ids), in.total)
	}
	if strings.Contains(in.text, "repo ") {
		t.Error("single-repo scope should not label repos")
	}
}

func TestParse(t *testing.T) {
	ids := map[string]insight.SessionRef{"p1": {Session: "s1", Seq: 3}, "p2": {Session: "s2", Seq: 7}}
	answer := `{"findings": [
	  {"kind": "skill", "title": "Repo walkthrough", "observation": "o", "hypothesis": "h", "suggestion": "s",
	   "snippet": "snip", "benefit": "b", "downside": "d", "confidence": "medium", "evidence": ["p1", " p2 ", "p99"]},
	  {"kind": "claude_md", "title": "Untraceable", "observation": "o", "hypothesis": "h", "suggestion": "s",
	   "snippet": "", "benefit": "b", "downside": "d", "confidence": "low", "evidence": ["p42"]}
	]}`
	fs, err := parse(answer, ids)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 1 {
		t.Fatalf("got %d findings, want 1 (untraceable one dropped)", len(fs))
	}
	f := fs[0]
	if f.Rule != "llm-skill" || f.Target != "skill" || len(f.Sessions) != 2 || f.Sessions[1].Seq != 7 {
		t.Errorf("finding = %+v", f)
	}
	if _, err := parse("not json", ids); err == nil {
		t.Error("want error for invalid JSON")
	}
}

func TestRenderRedactsSecrets(t *testing.T) {
	key := "f00dcafe0123456789abcdef0123456789beef42"
	in := render([]storage.Prompt{{SessionID: "s", Seq: 0, Text: "use https://redmine.example.com/issues/1.json?key=" + key}},
		map[string]storage.SessionSummary{"s": {ID: "s"}}, false)
	if strings.Contains(in.text, key) || !strings.Contains(in.text, "[REDACTED:credential]") {
		t.Errorf("secret not redacted:\n%s", in.text)
	}
}
