package secrets

import (
	"strings"
	"testing"
)

func TestFindAndRedact(t *testing.T) {
	tests := []struct {
		name, in, want string // want: Redact output
	}{
		{"url query key", `curl -sk "https://redmine.example.com/issues/1.json?key=f00dcafe0123456789abcdef0123456789beef42&include=journals"`,
			`curl -sk "https://redmine.example.com/issues/1.json?key=[REDACTED:credential]&include=journals"`},
		{"anthropic key", "export ANTHROPIC_API_KEY=sk-ant-api03-abcdefghijklmnopqrstuvwxyz0123",
			"export ANTHROPIC_API_KEY=[REDACTED:anthropic key]"},
		{"github token", "token ghp_0123456789abcdefghijklmnopqrstuvwxyzAB in text", "token [REDACTED:github token] in text"},
		{"bearer", "Authorization: Bearer abcdef0123456789abcdef0123", "Authorization: Bearer [REDACTED:bearer token]"},
		{"dsn password", "postgres://app:s3cretPass9@db:5432/main", "postgres://app:[REDACTED:url password]@db:5432/main"},
		{"aws", "AKIAIOSFODNN7EXAMPLE", "[REDACTED:aws key]"},
		{"password field", `{"password": "hunter2hunter2X9"}`, `{"password": "[REDACTED:credential]"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Redact(tt.in); got != tt.want {
				t.Errorf("Redact:\n got %s\nwant %s", got, tt.want)
			}
		})
	}
}

func TestNoFalsePositives(t *testing.T) {
	for _, in := range []string{
		"the key=value pairs in the map",
		"password: YOUR_PASSWORD",
		"token=${GITHUB_TOKEN}",
		"api_key: <your key here>",
		"secret = true",
		"git push origin main",
		"key: sort_order",
		"https://github.com/zakharkiv-vitalik/sexy-agent-tuner",
	} {
		if ms := Find(in); len(ms) != 0 {
			t.Errorf("false positive in %q: %+v", in, ms)
		}
	}
}

func TestMask(t *testing.T) {
	if m := Mask("f00dcafe0123456789abcdef0123456789beef42"); m != "f00d…ef42" || strings.Contains(m, "cafe") {
		t.Errorf("Mask = %q", m)
	}
	if m := Mask("short"); m != "•••••" {
		t.Errorf("Mask(short) = %q", m)
	}
}
