package main

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/zakharkiv-vitalik/sexy-agent-tuner/internal/analysis"
	"github.com/zakharkiv-vitalik/sexy-agent-tuner/internal/insight"
	"github.com/zakharkiv-vitalik/sexy-agent-tuner/internal/session"
	"github.com/zakharkiv-vitalik/sexy-agent-tuner/internal/storage"
)

// App is the backend bound to the frontend. All business logic stays in
// internal/; these methods only load, convert and delegate.
type App struct {
	ctx context.Context
	db  *storage.DB

	mu     sync.Mutex
	cached *session.Session // last opened session, for event detail lookups
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	db, err := storage.Open(ctx, storage.DefaultPath())
	if err != nil {
		runtime.LogFatalf(ctx, "open database: %v", err)
	}
	a.db = db
}

func (a *App) shutdown(context.Context) {
	if a.db != nil {
		a.db.Close()
	}
}

// SessionRow is a row of the imported-sessions table.
type SessionRow struct {
	ID         string `json:"id"`
	Project    string `json:"project"`
	StartedAt  string `json:"startedAt"`
	DurationNs int64  `json:"durationNs"`
	Events     int    `json:"events"`
	Prompts    int    `json:"prompts"`
	Tokens     int    `json:"tokens"`
	Peak       int    `json:"peak"`
}

func (a *App) ListSessions() ([]SessionRow, error) {
	list, err := a.db.ListSessions(a.ctx)
	if err != nil {
		return nil, err
	}
	rows := make([]SessionRow, 0, len(list))
	for _, s := range list {
		rows = append(rows, SessionRow{ID: s.ID, Project: s.Project, StartedAt: s.StartedAt.Format(time.RFC3339),
			DurationNs: int64(s.FinishedAt.Sub(s.StartedAt)), Events: s.Events,
			Prompts: s.Prompts, Tokens: s.Tokens, Peak: s.PeakContext})
	}
	return rows, nil
}

// Candidate is a session file found on disk that can be imported.
type Candidate struct {
	Path        string `json:"path"`
	ID          string `json:"id"`
	Project     string `json:"project"`
	FirstPrompt string `json:"firstPrompt"`
	ModifiedAt  string `json:"modifiedAt"`
	SizeBytes   int64  `json:"sizeBytes"`
	Imported    bool   `json:"imported"`
}

// Discover lists Claude Code session files under ~/.claude/projects, newest first.
// ponytail: peeks at every file on each call (~256KB each); cache by mtime if it gets slow.
func (a *App) Discover() ([]Candidate, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	paths, err := filepath.Glob(filepath.Join(home, ".claude", "projects", "*", "*.jsonl"))
	if err != nil {
		return nil, err
	}
	imported := map[string]bool{}
	if list, err := a.db.ListSessions(a.ctx); err == nil {
		for _, s := range list {
			imported[s.ID] = true
		}
	}
	var out []Candidate
	for _, p := range paths {
		fi, err := os.Stat(p)
		if err != nil || fi.Size() == 0 {
			continue
		}
		id, project, prompt, err := session.Peek(p)
		if err != nil || prompt == "" {
			continue // no human prompt: an empty or aborted session
		}
		out = append(out, Candidate{Path: p, ID: id, Project: project, FirstPrompt: prompt,
			ModifiedAt: fi.ModTime().Format(time.RFC3339), SizeBytes: fi.Size(), Imported: imported[id]})
	}
	slices.SortFunc(out, func(x, y Candidate) int { return cmp.Compare(y.ModifiedAt, x.ModifiedAt) })
	return out, nil
}

// Import parses and stores session files, returning their ids.
func (a *App) Import(paths []string) ([]string, error) {
	var ids []string
	for _, p := range paths {
		s, err := session.ImportClaudeFile(p)
		if err != nil {
			return ids, err
		}
		if err := a.db.SaveSession(a.ctx, s); err != nil {
			return ids, fmt.Errorf("save %s: %w", p, err)
		}
		ids = append(ids, s.ID)
	}
	return ids, nil
}

// ImportDialog lets the user pick session files with the native file dialog.
func (a *App) ImportDialog() ([]string, error) {
	home, _ := os.UserHomeDir()
	paths, err := runtime.OpenMultipleFilesDialog(a.ctx, runtime.OpenDialogOptions{
		Title:            "Import Claude Code sessions",
		DefaultDirectory: filepath.Join(home, ".claude", "projects"),
		Filters:          []runtime.FileFilter{{DisplayName: "Session logs (*.jsonl)", Pattern: "*.jsonl"}},
	})
	if err != nil || len(paths) == 0 {
		return nil, err
	}
	return a.Import(paths)
}

// SessionView is everything the session screen needs in one call.
type SessionView struct {
	ID         string            `json:"id"`
	Project    string            `json:"project"`
	SourcePath string            `json:"sourcePath"`
	StartedAt  string            `json:"startedAt"`
	Warnings   []string          `json:"warnings"`
	Report     *analysis.Report  `json:"report"`
	Findings   []insight.Finding `json:"findings"`
}

func (a *App) OpenSession(id string) (*SessionView, error) {
	s, err := a.db.LoadSession(a.ctx, id)
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	a.cached = s
	a.mu.Unlock()
	r := analysis.Analyze(s)
	return &SessionView{ID: s.ID, Project: s.Project, SourcePath: s.SourcePath, StartedAt: s.StartedAt.Format(time.RFC3339),
		Warnings: s.Warnings, Report: r, Findings: insight.Generate(s, r)}, nil
}

// RepoRow is one project (working directory) with imported sessions.
type RepoRow struct {
	Path     string `json:"path"`
	Name     string `json:"name"`
	Sessions int    `json:"sessions"`
	LastAt   string `json:"lastAt"`
	Tokens   int    `json:"tokens"`
}

// Repos groups imported sessions by project, most recently active first.
func (a *App) Repos() ([]RepoRow, error) {
	list, err := a.db.ListSessions(a.ctx)
	if err != nil {
		return nil, err
	}
	var out []RepoRow
	idx := map[string]int{}
	for _, s := range list { // newest first, so the first row of a project is its last activity
		i, ok := idx[s.Project]
		if !ok {
			i, idx[s.Project] = len(out), len(out)
			out = append(out, RepoRow{Path: s.Project, Name: filepath.Base(s.Project), LastAt: s.StartedAt.Format(time.RFC3339)})
		}
		out[i].Sessions++
		out[i].Tokens += s.Tokens
	}
	return out, nil
}

// PromptRow is a human prompt inside a repo view.
type PromptRow struct {
	SessionID string `json:"sessionId"`
	Seq       int    `json:"seq"`
	At        string `json:"at"`
	Text      string `json:"text"`
}

// RepoView is one project: its sessions, every prompt typed in it, and the
// patterns that recur across its sessions.
type RepoView struct {
	Path     string            `json:"path"`
	Name     string            `json:"name"`
	Sessions []SessionRow      `json:"sessions"`
	Prompts  []PromptRow       `json:"prompts"`
	Findings []insight.Finding `json:"findings"`
}

func (a *App) Repo(path string) (*RepoView, error) {
	all, err := a.ListSessions()
	if err != nil {
		return nil, err
	}
	v := &RepoView{Path: path, Name: filepath.Base(path)}
	for _, s := range all {
		if s.Project == path {
			v.Sessions = append(v.Sessions, s)
		}
	}
	prompts, err := a.db.ListPrompts(a.ctx, path)
	if err != nil {
		return nil, err
	}
	for _, p := range prompts {
		v.Prompts = append(v.Prompts, PromptRow{SessionID: p.SessionID, Seq: p.Seq, At: p.At.Format(time.RFC3339), Text: p.Text})
	}
	ss, err := a.db.LoadSessions(a.ctx, path)
	if err != nil {
		return nil, err
	}
	v.Findings = insight.Across(ss)
	return v, nil
}

// RepoFindings counts the repo-level suggestions of one project.
type RepoFindings struct {
	Path  string `json:"path"`
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// GlobalView holds what applies beyond one repo: patterns spanning several
// projects (user-level CLAUDE.md, personal skills, agents, scripts), plus how
// many repo-level suggestions each project has.
type GlobalView struct {
	Findings []insight.Finding `json:"findings"`
	Repos    []RepoFindings    `json:"repos"`
}

func (a *App) Insights() (*GlobalView, error) {
	ss, err := a.db.LoadSessions(a.ctx, "")
	if err != nil {
		return nil, err
	}
	project := map[string]string{}
	byProject := map[string][]*session.Session{}
	for _, s := range ss {
		project[s.ID] = s.Project
		byProject[s.Project] = append(byProject[s.Project], s)
	}
	v := &GlobalView{}
	for _, f := range insight.Across(ss) {
		seen := map[string]bool{}
		for _, ref := range f.Sessions {
			seen[project[ref.Session]] = true
		}
		if len(seen) > 1 {
			v.Findings = append(v.Findings, f)
		}
	}
	for p, group := range byProject {
		if n := len(insight.Across(group)); n > 0 {
			v.Repos = append(v.Repos, RepoFindings{Path: p, Name: filepath.Base(p), Count: n})
		}
	}
	slices.SortFunc(v.Repos, func(x, y RepoFindings) int { return cmp.Or(cmp.Compare(y.Count, x.Count), cmp.Compare(x.Name, y.Name)) })
	return v, nil
}

// EventDetail is the full content of one event, loaded on demand.
type EventDetail struct {
	Seq         int    `json:"seq"`
	Kind        string `json:"kind"`
	Agent       string `json:"agent"`
	Timestamp   string `json:"timestamp"`
	Text        string `json:"text"`
	ToolName    string `json:"toolName"`
	Input       string `json:"input"`
	Output      string `json:"output"`
	OutputBytes int    `json:"outputBytes"`
	IsError     bool   `json:"isError"`
	DurationNs  int64  `json:"durationNs"`
}

func (a *App) Event(sessionID string, seq int) (*EventDetail, error) {
	a.mu.Lock()
	s := a.cached
	a.mu.Unlock()
	if s == nil || s.ID != sessionID {
		var err error
		if s, err = a.db.LoadSession(a.ctx, sessionID); err != nil {
			return nil, err
		}
	}
	if seq < 0 || seq >= len(s.Events) {
		return nil, fmt.Errorf("event %d out of range", seq)
	}
	e := s.Events[seq]
	d := &EventDetail{Seq: e.Seq, Kind: string(e.Kind), Agent: e.Agent, Timestamp: e.Timestamp.Format(time.RFC3339), Text: e.Text}
	if t := e.Tool; t != nil {
		var buf bytes.Buffer
		input := string(t.Input)
		if json.Indent(&buf, t.Input, "", "  ") == nil {
			input = buf.String()
		}
		d.ToolName, d.Input, d.Output, d.OutputBytes, d.IsError, d.DurationNs = t.Name, input, t.Output, t.OutputBytes, t.IsError, int64(t.Duration)
	}
	return d, nil
}
