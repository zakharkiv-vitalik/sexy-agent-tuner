// Package secrets finds credentials in session text (prompts, commands, tool
// output) so they can be flagged and redacted before anything leaves the machine.
package secrets

import (
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// Match is one secret in a text. Value is the secret itself; Start/End locate it.
type Match struct {
	Kind       string
	Value      string
	Start, End int
}

type pattern struct {
	kind string
	re   *regexp.Regexp
	// group is the submatch holding the secret; 0 means the whole match.
	group int
	// generic patterns also require the value to look random, not like a word or placeholder.
	generic bool
	// hints are lowercase substrings one of which must occur for the regex to
	// run at all; most text has none, and this keeps a scan of every event cheap.
	hints []string
}

// ponytail: regexes catch common formats, not every secret; swap in gitleaks
// rules if coverage matters more than a dependency-free scan.
var patterns = []pattern{
	{kind: "private key", re: regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?(?:-----END [A-Z ]*PRIVATE KEY-----|$)`), hints: []string{"private key"}},
	{kind: "anthropic key", re: regexp.MustCompile(`sk-ant-[A-Za-z0-9_\-]{20,}`), hints: []string{"sk-ant-"}},
	{kind: "openai key", re: regexp.MustCompile(`sk-(?:proj-)?[A-Za-z0-9_\-]{32,}`), hints: []string{"sk-"}},
	{kind: "github token", re: regexp.MustCompile(`(?:gh[pousr]_[A-Za-z0-9]{36,}|github_pat_[A-Za-z0-9_]{40,})`), hints: []string{"ghp_", "gho_", "ghu_", "ghs_", "ghr_", "github_pat_"}},
	{kind: "gitlab token", re: regexp.MustCompile(`glpat-[A-Za-z0-9_\-]{20,}`), hints: []string{"glpat-"}},
	{kind: "aws key", re: regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`), hints: []string{"akia", "asia"}},
	{kind: "slack token", re: regexp.MustCompile(`xox[abprs]-[A-Za-z0-9\-]{10,}`), hints: []string{"xox"}},
	{kind: "google key", re: regexp.MustCompile(`AIza[0-9A-Za-z_\-]{35}`), hints: []string{"aiza"}},
	{kind: "jwt", re: regexp.MustCompile(`eyJ[A-Za-z0-9_\-]{10,}\.eyJ[A-Za-z0-9_\-]{10,}\.[A-Za-z0-9_\-]{10,}`), hints: []string{"eyj"}},
	{kind: "bearer token", re: regexp.MustCompile(`(?i)\bbearer\s+([A-Za-z0-9._~+/\-]{20,}=*)`), group: 1, hints: []string{"bearer"}},
	{kind: "url password", re: regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9+.\-]*://[^\s/:@]+:([^\s@/]{3,})@`), group: 1, generic: true, hints: []string{"://"}},
	// Named credentials: api_key=…, "password": "…", X-Redmine-API-Key: … .
	{kind: "credential", re: regexp.MustCompile(`(?i)\b(?:api[_-]?key|apikey|access[_-]?token|auth[_-]?token|secret[_-]?key|client[_-]?secret|password|passwd|pwd)["']?\s*[:=]\s*["']?([A-Za-z0-9_\-./+!@#$%^*]{8,}={0,2})`), group: 1, generic: true, hints: []string{"key", "token", "secret", "pass", "pwd"}},
	// Bare key/token/secret only as key=… (URL query, env); as a JSON field name it's usually data.
	{kind: "credential", re: regexp.MustCompile(`(?i)(?:^|[?&\s;,"'])(?:key|token|secret)=([A-Za-z0-9_\-./+!@#$%^*]{8,}={0,2})`), group: 1, generic: true, hints: []string{"key=", "token=", "secret="}},
}

// Find returns the secrets in text, in order, without overlaps.
func Find(text string) []Match {
	var out []Match
	lower := strings.ToLower(text)
	for _, p := range patterns {
		if !slices.ContainsFunc(p.hints, func(h string) bool { return strings.Contains(lower, h) }) {
			continue
		}
		for _, loc := range p.re.FindAllStringSubmatchIndex(text, -1) {
			start, end := loc[2*p.group], loc[2*p.group+1]
			if start < 0 {
				continue
			}
			v := text[start:end]
			if p.generic && !looksSecret(v) {
				continue
			}
			out = append(out, Match{Kind: p.kind, Value: v, Start: start, End: end})
		}
	}
	slices.SortFunc(out, func(a, b Match) int { return a.Start - b.Start })
	// Specific patterns run first, so on overlap the earlier, more specific match wins.
	var kept []Match
	for _, m := range out {
		if len(kept) > 0 && m.Start < kept[len(kept)-1].End {
			continue
		}
		kept = append(kept, m)
	}
	return kept
}

// Redact replaces every secret's value with [REDACTED:kind].
func Redact(text string) string {
	ms := Find(text)
	if len(ms) == 0 {
		return text
	}
	var b strings.Builder
	last := 0
	for _, m := range ms {
		b.WriteString(text[last:m.Start])
		b.WriteString("[REDACTED:" + m.Kind + "]")
		last = m.End
	}
	b.WriteString(text[last:])
	return b.String()
}

// Mask shows enough of a secret to recognize it, never enough to use it.
func Mask(v string) string {
	r := []rune(v)
	if len(r) <= 8 {
		return strings.Repeat("•", len(r))
	}
	return string(r[:4]) + "…" + string(r[len(r)-4:])
}

var placeholder = regexp.MustCompile(`(?i)^(?:your|my|example|sample|test|dummy|changeme|placeholder|redacted|xxx+|\*+|<.*>|\$\{?.*\}?|%.*%|\{\{.*\}\}|true|false|null|none|undefined)`)

// looksSecret filters generic matches: a credential value has letters and
// digits and isn't a placeholder, an env reference or an ordinary word.
func looksSecret(v string) bool {
	if placeholder.MatchString(v) || strings.Contains(v, "REDACTED") {
		return false
	}
	var letters, digits int
	for _, r := range v {
		switch {
		case unicode.IsLetter(r):
			letters++
		case unicode.IsDigit(r):
			digits++
		}
	}
	return letters > 0 && digits > 0
}
