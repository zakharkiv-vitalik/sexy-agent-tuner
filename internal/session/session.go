// Package session defines the provider-independent model of a coding-agent
// session and the adapters that build it from provider-specific logs.
package session

import (
	"encoding/json"
	"strings"
	"time"
)

// Kind classifies a normalized event.
type Kind string

const (
	KindUserPrompt    Kind = "user_prompt"    // text typed by the human
	KindModelCall     Kind = "model_call"     // one LLM response; carries token usage
	KindAssistantText Kind = "assistant_text" // visible text the agent produced
	KindToolCall      Kind = "tool_call"      // tool invocation, joined with its result
)

// Session is one agent run, flattened into an ordered list of events.
// Subagent (sidechain) events are included and tagged with their Agent.
type Session struct {
	ID         string
	Provider   string
	Project    string // working directory of the agent
	SourcePath string
	StartedAt  time.Time
	FinishedAt time.Time
	Events     []Event
	Warnings   []string // malformed or unrecognized lines, kept for transparency
}

// Event is a single step of the trajectory. Exactly one of the optional
// payloads is set depending on Kind.
type Event struct {
	Seq       int
	Kind      Kind
	Timestamp time.Time
	Agent     string // "" for the main agent, subagent id otherwise
	MessageID string // provider message id, groups events of one model call
	Text      string // prompt or assistant text (truncated)
	Usage     *Usage
	Tool      *ToolCall
}

// Usage is token accounting for one model call.
type Usage struct {
	Model         string
	Input         int
	Output        int
	CacheRead     int
	CacheCreation int
}

// Context is the full prompt size the model saw on this call.
func (u Usage) Context() int { return u.Input + u.CacheRead + u.CacheCreation }

// ToolCall is a tool invocation together with its result.
type ToolCall struct {
	ID          string
	Name        string
	Input       json.RawMessage
	Output      string // truncated result text
	OutputBytes int    // full result size before truncation
	IsError     bool
	HasResult   bool
	Duration    time.Duration
}

// MCP splits "mcp__server__tool" names. ok is false for built-in tools.
func (t ToolCall) MCP() (server, tool string, ok bool) {
	rest, found := strings.CutPrefix(t.Name, "mcp__")
	if !found {
		return "", "", false
	}
	server, tool, _ = strings.Cut(rest, "__")
	return server, tool, true
}

// Str returns a string field of the tool input, or "".
func (t ToolCall) Str(field string) string {
	var m map[string]any
	if json.Unmarshal(t.Input, &m) != nil {
		return ""
	}
	s, _ := m[field].(string)
	return s
}
