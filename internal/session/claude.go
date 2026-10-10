package session

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	ProviderClaude = "claude-code"
	maxTextLen     = 4000 // stored text/output is truncated; analysis only needs a preview
)

// claudeLine is the subset of a Claude Code JSONL record we read.
type claudeLine struct {
	Type      string          `json:"type"`
	SessionID string          `json:"sessionId"`
	AgentID   string          `json:"agentId"`
	Cwd       string          `json:"cwd"`
	Timestamp time.Time       `json:"timestamp"`
	IsMeta    bool            `json:"isMeta"`
	Message   json.RawMessage `json:"message"`
}

type claudeMessage struct {
	ID      string          `json:"id"`
	Role    string          `json:"role"`
	Model   string          `json:"model"`
	Content json.RawMessage `json:"content"`
	Usage   *struct {
		Input         int `json:"input_tokens"`
		Output        int `json:"output_tokens"`
		CacheRead     int `json:"cache_read_input_tokens"`
		CacheCreation int `json:"cache_creation_input_tokens"`
	} `json:"usage"`
}

type claudeBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	IsError   bool            `json:"is_error"`
	Content   json.RawMessage `json:"content"`
}

// ImportClaudeFile parses a Claude Code session file and, when present, the
// subagent transcripts stored next to it in <session-id>/subagents/*.jsonl.
func ImportClaudeFile(path string) (*Session, error) {
	s := &Session{Provider: ProviderClaude, SourcePath: path}
	if err := parseClaudeFile(s, path, ""); err != nil {
		return nil, err
	}
	if s.ID == "" {
		s.ID = strings.TrimSuffix(filepath.Base(path), ".jsonl")
	}

	subs, _ := filepath.Glob(filepath.Join(strings.TrimSuffix(path, ".jsonl"), "subagents", "*.jsonl"))
	for _, sub := range subs {
		agent := strings.TrimPrefix(strings.TrimSuffix(filepath.Base(sub), ".jsonl"), "agent-")
		if err := parseClaudeFile(s, sub, agent); err != nil {
			return nil, err
		}
	}
	if len(subs) > 0 {
		// Interleave subagent work with the main trajectory.
		sort.SliceStable(s.Events, func(i, j int) bool { return s.Events[i].Timestamp.Before(s.Events[j].Timestamp) })
		for i := range s.Events {
			s.Events[i].Seq = i
		}
	}
	return s, nil
}

func parseClaudeFile(s *Session, path, agent string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := ParseClaude(s, f, agent); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return nil
}

// ParseClaude streams Claude Code JSONL from r and appends normalized events
// to s. Lines are read one at a time, so memory is bounded by the normalized
// (truncated) events rather than the file size. Malformed lines are recorded
// in s.Warnings and skipped.
func ParseClaude(s *Session, r io.Reader, agent string) error {
	br := bufio.NewReaderSize(r, 1<<20)
	pending := map[string]int{}  // tool_use id -> index in s.Events
	seenMsg := map[string]bool{} // one assistant message spans several lines
	for lineNo := 1; ; lineNo++ {
		raw, err := br.ReadBytes('\n')
		if len(strings.TrimSpace(string(raw))) > 0 {
			var l claudeLine
			if jerr := json.Unmarshal(raw, &l); jerr != nil {
				s.Warnings = append(s.Warnings, fmt.Sprintf("%s line %d: %v", agentLabel(agent), lineNo, jerr))
			} else {
				s.handle(l, agent, pending, seenMsg)
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func agentLabel(agent string) string {
	if agent == "" {
		return "main"
	}
	return "agent " + agent
}

func (s *Session) handle(l claudeLine, agent string, pending map[string]int, seenMsg map[string]bool) {
	if l.Type != "user" && l.Type != "assistant" {
		return // attachments, mode switches, snapshots etc. carry no trajectory data
	}
	if agent == "" {
		if s.ID == "" {
			s.ID = l.SessionID
		}
		if s.Project == "" {
			s.Project = l.Cwd
		}
	}
	if !l.Timestamp.IsZero() {
		if s.StartedAt.IsZero() || l.Timestamp.Before(s.StartedAt) {
			s.StartedAt = l.Timestamp
		}
		if l.Timestamp.After(s.FinishedAt) {
			s.FinishedAt = l.Timestamp
		}
	}

	var m claudeMessage
	if json.Unmarshal(l.Message, &m) != nil {
		return
	}
	add := func(e Event) {
		e.Seq, e.Timestamp, e.Agent = len(s.Events), l.Timestamp, agent
		s.Events = append(s.Events, e)
	}

	// User content is either a plain string (a prompt) or a list of blocks.
	var text string
	if json.Unmarshal(m.Content, &text) == nil {
		if l.Type == "user" && !l.IsMeta && !isSyntheticPrompt(text) {
			add(Event{Kind: KindUserPrompt, Text: truncate(text)})
		}
		return
	}
	var blocks []claudeBlock
	if json.Unmarshal(m.Content, &blocks) != nil {
		return
	}

	if l.Type == "assistant" && m.ID != "" && !seenMsg[m.ID] && m.Usage != nil {
		seenMsg[m.ID] = true
		u := m.Usage
		add(Event{Kind: KindModelCall, MessageID: m.ID, Usage: &Usage{
			Model: m.Model, Input: u.Input, Output: u.Output, CacheRead: u.CacheRead, CacheCreation: u.CacheCreation,
		}})
	}

	for _, b := range blocks {
		switch b.Type {
		case "text":
			if l.Type == "assistant" {
				add(Event{Kind: KindAssistantText, MessageID: m.ID, Text: truncate(b.Text)})
			} else if !l.IsMeta && !isSyntheticPrompt(b.Text) {
				add(Event{Kind: KindUserPrompt, Text: truncate(b.Text)})
			}
		case "tool_use":
			pending[b.ID] = len(s.Events)
			add(Event{Kind: KindToolCall, MessageID: m.ID, Tool: &ToolCall{ID: b.ID, Name: b.Name, Input: b.Input}})
		case "tool_result":
			i, ok := pending[b.ToolUseID]
			if !ok {
				continue
			}
			delete(pending, b.ToolUseID)
			t := s.Events[i].Tool
			out := resultText(b.Content)
			t.HasResult, t.IsError, t.OutputBytes, t.Output = true, b.IsError, len(out), truncate(out)
			if !l.Timestamp.IsZero() {
				t.Duration = l.Timestamp.Sub(s.Events[i].Timestamp)
			}
		}
	}
}

// resultText flattens a tool_result content, which is a string or a list of
// {type:"text", text} blocks.
func resultText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var parts []claudeBlock
	if json.Unmarshal(raw, &parts) != nil {
		return ""
	}
	var b strings.Builder
	for _, p := range parts {
		if p.Type == "text" {
			b.WriteString(p.Text)
		}
	}
	return b.String()
}

// isSyntheticPrompt reports user-role text injected by the harness rather than
// typed by the human (command wrappers, interrupts, reminders).
func isSyntheticPrompt(t string) bool {
	t = strings.TrimSpace(t)
	for _, p := range []string{"<command-", "<local-command-", "<system-reminder>", "<task-notification>", "[Request interrupted", "[SYSTEM NOTIFICATION", "Caveat:", "This session is being continued from a previous conversation"} {
		if strings.HasPrefix(t, p) {
			return true
		}
	}
	return t == ""
}

func truncate(s string) string {
	if len(s) <= maxTextLen {
		return s
	}
	return strings.ToValidUTF8(s[:maxTextLen], "") + "…"
}

// peekBytes bounds how much of a file Peek reads.
const peekBytes = 256 << 10

// Peek reads only the head of a session file to describe it (id, project,
// first prompt) without a full import.
func Peek(path string) (id, project, firstPrompt string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", "", "", err
	}
	defer f.Close()
	s := &Session{}
	// The limit usually cuts the last line; that warning is irrelevant here.
	if err := ParseClaude(s, io.LimitReader(f, peekBytes), ""); err != nil {
		return "", "", "", err
	}
	for _, e := range s.Events {
		if e.Kind == KindUserPrompt {
			firstPrompt = e.Text
			break
		}
	}
	return firstNonEmpty(s.ID, strings.TrimSuffix(filepath.Base(path), ".jsonl")), s.Project, firstPrompt, nil
}

func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}
