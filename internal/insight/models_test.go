package insight

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/zakharkiv-vitalik/sexy-agent-tuner/internal/session"
)

// modelSession builds a session on one model from tool calls (name, input, failed).
func modelSession(model string, calls ...[3]string) *builder {
	b := newBuilder()
	b.prompt("do the routine thing for me please")
	for _, c := range calls {
		in := json.RawMessage(c[1])
		b.add(session.Event{Kind: session.KindModelCall, Usage: &session.Usage{Model: model, CacheRead: 30_000, Output: 100}})
		b.add(session.Event{Kind: session.KindToolCall, Tool: &session.ToolCall{Name: c[0], Input: in, IsError: c[2] == "x", HasResult: true}})
	}
	return b
}

func ruleSet(b *builder) []string { return rules(b.findings()) }

func TestModelRules(t *testing.T) {
	git := [3]string{"Bash", `{"command":"git status"}`, ""}
	commit := [3]string{"Bash", `{"command":"git commit -m x"}`, ""}
	push := [3]string{"Bash", `{"command":"git push"}`, ""}

	if got := ruleSet(modelSession("claude-opus-5-5", git, commit, push)); !slices.Contains(got, "routine-on-top-model") {
		t.Errorf("routine on opus: %v", got)
	}
	if got := ruleSet(modelSession("claude-sonnet-5-5", git, commit, push)); slices.Contains(got, "routine-on-top-model") {
		t.Errorf("routine on sonnet should not fire: %v", got)
	}

	old := ruleSet(modelSession("claude-haiku-4-5-20251001", git, commit, push))
	if !slices.Contains(old, "outdated-model") {
		t.Errorf("haiku 4.5 with a date suffix should be outdated: %v", old)
	}
	if got := ruleSet(modelSession("claude-haiku-5-5", git, commit, push)); slices.Contains(got, "outdated-model") {
		t.Errorf("current model flagged as outdated: %v", got)
	}

	fail := [3]string{"Bash", `{"command":"make build"}`, "x"}
	var many [][3]string
	for range 6 {
		many = append(many, fail, git)
	}
	if got := ruleSet(modelSession("claude-sonnet-5-5", many...)); !slices.Contains(got, "struggle-on-smaller-model") {
		t.Errorf("failures on sonnet: %v", got)
	}
	if got := ruleSet(modelSession("claude-opus-5-5", many...)); slices.Contains(got, "struggle-on-smaller-model") {
		t.Errorf("struggle rule is for smaller models only: %v", got)
	}
}

func TestSecretsInSession(t *testing.T) {
	key := "f00dcafe0123456789abcdef0123456789beef42"
	b := modelSession("claude-opus-5-5", [3]string{"Bash", `{"command":"curl -H 'X-Redmine-API-Key: ` + key + `' https://x"}`, ""})
	var f *Finding
	for _, fd := range b.findings() {
		if fd.Rule == "secrets" {
			f = &fd
		}
	}
	if f == nil {
		t.Fatal("no secrets finding")
	}
	if strings.Contains(f.Observation, key) || !strings.Contains(f.Observation, "f00d…ef42") || f.Target != TargetSecurity {
		t.Errorf("finding = %+v", f)
	}
	if len(secretsAcross([]*session.Session{&b.s})) != 1 {
		t.Error("one session with a secret is enough for the cross-session finding")
	}
}
