package apply

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zakharkiv-vitalik/sexy-agent-tuner/internal/insight"
	"github.com/zakharkiv-vitalik/sexy-agent-tuner/internal/session"
	"github.com/zakharkiv-vitalik/sexy-agent-tuner/internal/storage"
)

var day = time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)

func TestPlanAndWrite(t *testing.T) {
	home, repo := t.TempDir(), t.TempDir()
	allowed := []string{repo, filepath.Join(home, ".claude")}
	read := func(p string) string {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}

	// CLAUDE.md: append to an existing file, keeping what's there.
	if err := os.WriteFile(filepath.Join(repo, "CLAUDE.md"), []byte("# Rules\n- be brief\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Plan(insight.Finding{Rule: "repeated-exploration", Target: insight.TargetContext, Title: "Key files", Snippet: "Key files:\n- a.go"}, repo, home)
	if err != nil || c.Mode != ModeAppend || c.Path != filepath.Join(repo, "CLAUDE.md") || !c.Exists {
		t.Fatalf("plan = %+v, %v", c, err)
	}
	c.Content = "Key files:\n- a.go — the parser" // the user filled the placeholder in the preview
	if err := Write(*c, allowed, day); err != nil {
		t.Fatal(err)
	}
	if got := read(c.Path); !strings.HasPrefix(got, "# Rules\n- be brief\n\n<!-- added by Sexy Agent Tuner 2026-10-11: Key files -->\nKey files:\n- a.go — the parser\n") {
		t.Errorf("CLAUDE.md =\n%s", got)
	}

	// User-level scope goes to ~/.claude.
	c, err = Plan(insight.Finding{Rule: "secrets", Target: insight.TargetSecurity, Title: "Secrets", Snippet: "Never paste tokens."}, "", home)
	if err != nil || c.Path != filepath.Join(home, ".claude", "CLAUDE.md") || c.Exists {
		t.Fatalf("user plan = %+v, %v", c, err)
	}

	// New file; planning it again is refused once it exists.
	c, err = Plan(insight.Finding{Rule: "explore-on-top-model", Title: "Explore", Snippet: "---\nname: explorer\n---\nbody"}, repo, home)
	if err != nil || c.Mode != ModeCreate {
		t.Fatalf("agent plan = %+v, %v", c, err)
	}
	if err := Write(*c, allowed, day); err != nil {
		t.Fatal(err)
	}
	if _, err := Plan(insight.Finding{Rule: "explore-on-top-model", Snippet: "x"}, repo, home); err == nil {
		t.Error("planning an existing agent file should fail")
	}

	// settings.json: set the model, keep other keys.
	settings := filepath.Join(home, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settings), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settings, []byte(`{"theme":"dark","model":"claude-opus-5"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err = Plan(insight.Finding{Rule: "outdated-model", Title: "Old", Snippet: `{ "model": "claude-opus-5-5" }`}, repo, home)
	if err != nil || c.Mode != ModeSettings || c.Path != settings || c.Scope != "" {
		t.Fatalf("settings plan = %+v, %v", c, err)
	}
	if err := Write(*c, allowed, day); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(read(settings)), &got); err != nil || got["model"] != "claude-opus-5-5" || got["theme"] != "dark" {
		t.Errorf("settings = %v, %v", got, err)
	}

	// Skill from an AI finding gets frontmatter and a slug name.
	c, err = Plan(insight.Finding{Rule: "llm-skill", Title: "Repo walkthrough!", Snippet: "Explain the repo."}, repo, home)
	if err != nil || !strings.HasSuffix(c.Path, filepath.Join("skills", "repo-walkthrough", "SKILL.md")) || !strings.HasPrefix(c.Content, "---\nname: repo-walkthrough\n") {
		t.Errorf("skill plan = %+v, %v", c, err)
	}

	// Not every finding is a file change.
	if _, err := Plan(insight.Finding{Rule: "heavy-turn", Target: insight.TargetPrompt, Snippet: "Goal:"}, repo, home); !errors.Is(err, ErrNotApplicable) {
		t.Errorf("heavy-turn err = %v", err)
	}
}

func TestWriteGuards(t *testing.T) {
	repo, other := t.TempDir(), t.TempDir()
	c := Change{Mode: ModeCreate, Path: filepath.Join(other, "x.md"), Content: "x"}
	if err := Write(c, []string{repo}, day); err == nil {
		t.Error("wrote outside the allowed roots")
	}

	path := filepath.Join(repo, "CLAUDE.md")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	c = Change{Mode: ModeAppend, Path: path, Content: "rule", Exists: true, Before: "old"}
	if err := os.WriteFile(path, []byte("edited meanwhile"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Write(c, []string{repo}, day); err == nil {
		t.Error("overwrote a file that changed since the preview")
	}
}

func TestCompare(t *testing.T) {
	sess := func(start time.Time, readsTwice bool) *session.Session {
		s := &session.Session{Project: "/p", StartedAt: start}
		add := func(e session.Event) { e.Seq, e.Timestamp = len(s.Events), start; s.Events = append(s.Events, e) }
		add(session.Event{Kind: session.KindUserPrompt, Text: "do it"})
		add(session.Event{Kind: session.KindModelCall, Usage: &session.Usage{CacheRead: 10_000}})
		read := session.Event{Kind: session.KindToolCall, Tool: &session.ToolCall{Name: "Read", Input: json.RawMessage(`{"file_path":"/p/a.go"}`), HasResult: true}}
		add(read)
		if readsTwice {
			add(read)
			add(read)
		}
		return s
	}
	var ss []*session.Session
	for i := range 4 {
		ss = append(ss, sess(day.Add(-time.Duration(i+1)*time.Hour), true)) // before: redundant reads
	}
	for i := range 3 {
		ss = append(ss, sess(day.Add(time.Duration(i+1)*time.Hour), false))
	}
	e := Compare(ss, storage.AppliedChange{Rule: "redundant-reads", AppliedAt: day})
	if e.Before.Sessions != 4 || e.After.Sessions != 3 || e.TooEarly {
		t.Fatalf("split = %+v", e)
	}
	if e.Before.RuleRate != 1 || e.After.RuleRate != 0 {
		t.Errorf("rule rate before %.2f after %.2f, want 1 and 0", e.Before.RuleRate, e.After.RuleRate)
	}
	if e := Compare(ss[:5], storage.AppliedChange{Rule: "redundant-reads", AppliedAt: day}); !e.TooEarly {
		t.Error("one session after the change should be too early")
	}
	if e := Compare(ss, storage.AppliedChange{Rule: "repeated-exploration", AppliedAt: day}); e.Before.RuleRate != -1 {
		t.Error("cross-session rules can't be re-checked per session")
	}
}
