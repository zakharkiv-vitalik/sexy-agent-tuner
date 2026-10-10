package session

import (
	"strings"
	"testing"
	"time"
)

func TestImportClaudeFixture(t *testing.T) {
	s, err := ImportClaudeFile("testdata/valid_to_9999.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	counts := map[Kind]int{}
	for _, e := range s.Events {
		counts[e.Kind]++
	}
	checks := []struct {
		name      string
		got, want any
	}{
		{"id", s.ID, "valid-to-9999"},
		{"project", s.Project, "/work/snapshots-svc"},
		{"prompts", counts[KindUserPrompt], 1},
		{"model calls (split message deduped)", counts[KindModelCall], 18},
		{"tool calls", counts[KindToolCall], 17},
		{"warnings (malformed line)", len(s.Warnings), 1},
		{"duration", s.FinishedAt.Sub(s.StartedAt) > 5*time.Minute, true},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
	for _, e := range s.Events {
		if e.Kind == KindToolCall && !e.Tool.HasResult {
			t.Errorf("tool %s (%s) has no result joined", e.Tool.ID, e.Tool.Name)
		}
	}
}

func TestParseClaude(t *testing.T) {
	tests := []struct {
		name     string
		in       string
		kinds    []Kind
		warnings int
		check    func(t *testing.T, s *Session)
	}{
		{
			name:  "string prompt",
			in:    `{"type":"user","timestamp":"2026-01-01T00:00:00Z","message":{"role":"user","content":"hello"}}`,
			kinds: []Kind{KindUserPrompt},
		},
		{
			name:  "synthetic and meta prompts skipped",
			in:    `{"type":"user","message":{"role":"user","content":"<command-name>/clear</command-name>"}}` + "\n" + `{"type":"user","isMeta":true,"message":{"role":"user","content":"x"}}` + "\n" + `{"type":"user","message":{"role":"user","content":"<task-notification><task-id>a1</task-id></task-notification>"}}`,
			kinds: nil,
		},
		{
			name:     "malformed line is a warning, parsing continues",
			in:       "{not json\n" + `{"type":"user","message":{"role":"user","content":"hi"}}`,
			kinds:    []Kind{KindUserPrompt},
			warnings: 1,
		},
		{
			name:  "non-trajectory types ignored",
			in:    `{"type":"attachment"}` + "\n" + `{"type":"permission-mode","permissionMode":"auto"}`,
			kinds: nil,
		},
		{
			name: "tool result joined, error flag and duration",
			in: `{"type":"assistant","timestamp":"2026-01-01T00:00:00Z","message":{"id":"m1","role":"assistant","usage":{"input_tokens":1,"output_tokens":2,"cache_read_input_tokens":3},"content":[{"type":"tool_use","id":"t1","name":"mcp__postgres__query","input":{"sql":"select 1"}}]}}
{"type":"user","timestamp":"2026-01-01T00:00:05Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","is_error":true,"content":[{"type":"text","text":"boom"}]}]}}`,
			kinds: []Kind{KindModelCall, KindToolCall},
			check: func(t *testing.T, s *Session) {
				tc := s.Events[1].Tool
				if !tc.IsError || tc.Output != "boom" || tc.Duration != 5*time.Second {
					t.Errorf("tool = %+v", tc)
				}
				if srv, name, ok := tc.MCP(); !ok || srv != "postgres" || name != "query" {
					t.Errorf("MCP() = %q %q %v", srv, name, ok)
				}
				if got := s.Events[0].Usage.Context(); got != 4 {
					t.Errorf("context = %d, want 4", got)
				}
			},
		},
		{
			name: "usage counted once per message id",
			in: `{"type":"assistant","message":{"id":"m1","role":"assistant","usage":{"output_tokens":7},"content":[{"type":"text","text":"a"}]}}
{"type":"assistant","message":{"id":"m1","role":"assistant","usage":{"output_tokens":7},"content":[{"type":"text","text":"b"}]}}`,
			kinds: []Kind{KindModelCall, KindAssistantText, KindAssistantText},
		},
		{
			name:  "orphan tool result ignored",
			in:    `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"nope","content":"x"}]}}`,
			kinds: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Session{}
			if err := ParseClaude(s, strings.NewReader(tt.in), ""); err != nil {
				t.Fatal(err)
			}
			var kinds []Kind
			for _, e := range s.Events {
				kinds = append(kinds, e.Kind)
			}
			if strings.Join(toStrings(kinds), ",") != strings.Join(toStrings(tt.kinds), ",") {
				t.Errorf("kinds = %v, want %v", kinds, tt.kinds)
			}
			if len(s.Warnings) != tt.warnings {
				t.Errorf("warnings = %v, want %d", s.Warnings, tt.warnings)
			}
			if tt.check != nil {
				tt.check(t, s)
			}
		})
	}
}

func toStrings(ks []Kind) []string {
	out := make([]string, len(ks))
	for i, k := range ks {
		out[i] = string(k)
	}
	return out
}
