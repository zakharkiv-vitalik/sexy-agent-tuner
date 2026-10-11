// Package apply turns a finding into a concrete file change (a CLAUDE.md
// rule, a subagent, a skill, a slash command, the default model) and measures
// afterwards whether sessions improved.
package apply

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/zakharkiv-vitalik/sexy-agent-tuner/internal/insight"
)

// Modes of a change.
const (
	ModeAppend   = "append"   // add a block to the end of a text file (CLAUDE.md)
	ModeCreate   = "create"   // write a new file (agent, skill, command)
	ModeSettings = "settings" // set "model" in a Claude Code settings.json
)

// Change is a planned edit. Content is what the user reviews and may edit
// before it is written; Before is the file as it was when planned, so a file
// that changed in between is not overwritten.
type Change struct {
	Rule    string `json:"rule"`
	Title   string `json:"title"`
	Scope   string `json:"scope"` // repo path, or "" for user-level
	Path    string `json:"path"`
	Mode    string `json:"mode"`
	Content string `json:"content"`
	Exists  bool   `json:"exists"`
	Before  string `json:"before"`
}

// ErrNotApplicable means the finding has no file change to make; copy its snippet instead.
var ErrNotApplicable = errors.New("this suggestion has no automatic change")

// Plan maps a finding to a change. scope is the repo the finding belongs to,
// or "" for user-level findings (All repos); home is the user's home directory.
func Plan(f insight.Finding, scope, home string) (*Change, error) {
	base := scope
	if base == "" {
		base = filepath.Join(home, ".claude")
	}
	dotClaude := filepath.Join(scope, ".claude")
	if scope == "" {
		dotClaude = base
	}
	c := &Change{Rule: f.Rule, Title: f.Title, Scope: scope, Content: f.Snippet}

	switch {
	case f.Rule == "outdated-model":
		var s struct {
			Model string `json:"model"`
		}
		if json.Unmarshal([]byte(f.Snippet), &s) != nil || s.Model == "" {
			return nil, ErrNotApplicable
		}
		// The default model is a personal preference, so it always goes to the user settings.
		c.Mode, c.Path, c.Content, c.Scope = ModeSettings, filepath.Join(home, ".claude", "settings.json"), s.Model, ""
	case f.Rule == "explore-on-top-model":
		c.Mode, c.Path = ModeCreate, filepath.Join(dotClaude, "agents", "explorer.md")
	case f.Rule == "llm-skill":
		name := slug(f.Title)
		c.Mode, c.Path = ModeCreate, filepath.Join(dotClaude, "skills", name, "SKILL.md")
		if !strings.HasPrefix(strings.TrimSpace(f.Snippet), "---") {
			c.Content = fmt.Sprintf("---\nname: %s\ndescription: %s\n---\n%s\n", name, oneLine(f.Title), f.Snippet)
		}
	case f.Rule == "llm-slash_command":
		c.Mode, c.Path = ModeCreate, filepath.Join(dotClaude, "commands", slug(f.Title)+".md")
	case f.Target == insight.TargetContext || f.Target == insight.TargetSecurity || f.Rule == "llm-claude_md":
		c.Mode = ModeAppend
		c.Path = filepath.Join(scope, "CLAUDE.md")
		if scope == "" {
			c.Path = filepath.Join(home, ".claude", "CLAUDE.md")
		}
	default:
		return nil, ErrNotApplicable
	}
	if strings.TrimSpace(c.Content) == "" {
		return nil, ErrNotApplicable
	}
	if b, err := os.ReadFile(c.Path); err == nil {
		c.Exists, c.Before = true, string(b)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if c.Mode == ModeCreate && c.Exists {
		return nil, fmt.Errorf("%s already exists", c.Path)
	}
	return c, nil
}

// Write performs a planned change. allowed are the directories it may write
// into (known repos and ~/.claude); the target file must not have changed
// since Plan read it.
func Write(c Change, allowed []string, now time.Time) error {
	path := filepath.Clean(c.Path)
	if !filepath.IsAbs(path) || !inside(path, allowed) {
		return fmt.Errorf("refusing to write outside known repos and ~/.claude: %s", c.Path)
	}
	cur, err := os.ReadFile(path)
	switch {
	case err == nil && (!c.Exists || string(cur) != c.Before):
		return fmt.Errorf("%s changed since the preview; open it again", path)
	case err != nil && !errors.Is(err, os.ErrNotExist):
		return err
	case err != nil && c.Exists:
		return fmt.Errorf("%s was removed since the preview; open it again", path)
	}

	var out []byte
	switch c.Mode {
	case ModeAppend:
		block := fmt.Sprintf("<!-- added by Sexy Agent Tuner %s: %s -->\n%s\n", now.Format("2006-01-02"), oneLine(c.Title), strings.TrimSpace(c.Content))
		prefix := string(cur)
		if prefix != "" {
			prefix = strings.TrimRight(prefix, "\n") + "\n\n"
		}
		out = []byte(prefix + block)
	case ModeCreate:
		out = []byte(strings.TrimRight(c.Content, "\n") + "\n")
	case ModeSettings:
		settings := map[string]any{}
		if len(cur) > 0 {
			if err := json.Unmarshal(cur, &settings); err != nil {
				return fmt.Errorf("%s is not valid JSON, edit it by hand: %w", path, err)
			}
		}
		settings["model"] = strings.TrimSpace(c.Content)
		if out, err = json.MarshalIndent(settings, "", "  "); err != nil {
			return err
		}
		out = append(out, '\n')
	default:
		return fmt.Errorf("unknown change mode %q", c.Mode)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o644)
}

func inside(path string, roots []string) bool {
	for _, r := range roots {
		if r == "" {
			continue
		}
		if rel, err := filepath.Rel(filepath.Clean(r), path); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			return true
		}
	}
	return false
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// slug makes a file name from a title: "Repo walkthrough" -> "repo-walkthrough".
func slug(title string) string {
	s := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(title), "-"), "-")
	if len(s) > 40 {
		s = strings.TrimRight(s[:40], "-")
	}
	if s == "" {
		s = "suggestion"
	}
	return s
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }
