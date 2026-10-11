package apply

import (
	"slices"
	"sort"
	"time"

	"github.com/zakharkiv-vitalik/sexy-agent-tuner/internal/analysis"
	"github.com/zakharkiv-vitalik/sexy-agent-tuner/internal/insight"
	"github.com/zakharkiv-vitalik/sexy-agent-tuner/internal/session"
	"github.com/zakharkiv-vitalik/sexy-agent-tuner/internal/storage"
)

// minAfter is how many sessions after a change it takes before the
// comparison means anything; below it the result is shown as too early.
const (
	minAfter     = 3
	maxPerWindow = 20 // compare like with like: the most recent sessions on each side
)

// Stats summarizes a group of sessions. Medians keep one runaway session
// from deciding the result. -1 means not measurable.
type Stats struct {
	Sessions        int     `json:"sessions"`
	RuleRate        float64 `json:"ruleRate"`        // share of sessions where the applied rule fires again
	TokensPerPrompt int     `json:"tokensPerPrompt"` // median processed tokens per human prompt
	ExploreShare    float64 `json:"exploreShare"`    // median share of tokens before the first edit
	FailRate        float64 `json:"failRate"`        // median share of failed tool calls
}

// Experiment is an applied change with sessions before and after it.
type Experiment struct {
	Rule      string `json:"rule"`
	Title     string `json:"title"`
	Path      string `json:"path"`
	AppliedAt string `json:"appliedAt"`
	Before    Stats  `json:"before"`
	After     Stats  `json:"after"`
	TooEarly  bool   `json:"tooEarly"`
}

// Compare splits sessions at the change and measures both sides. Sessions
// must already be limited to the change's scope.
func Compare(ss []*session.Session, c storage.AppliedChange) Experiment {
	var before, after []*session.Session
	for _, s := range ss {
		if s.StartedAt.Before(c.AppliedAt) {
			before = append(before, s)
		} else {
			after = append(after, s)
		}
	}
	byStart := func(a, b *session.Session) int { return a.StartedAt.Compare(b.StartedAt) }
	slices.SortFunc(before, byStart)
	slices.SortFunc(after, byStart)
	before = before[max(0, len(before)-maxPerWindow):] // closest to the change
	after = after[:min(len(after), maxPerWindow)]

	return Experiment{
		Rule: c.Rule, Title: c.Title, Path: c.Path, AppliedAt: c.AppliedAt.Format(time.RFC3339),
		Before: stats(before, c.Rule), After: stats(after, c.Rule), TooEarly: len(after) < minAfter,
	}
}

func stats(ss []*session.Session, rule string) Stats {
	st := Stats{Sessions: len(ss), RuleRate: -1, ExploreShare: -1}
	if len(ss) == 0 {
		return st
	}
	var perPrompt, explore, fail []float64
	fires, measurable := 0, perSessionRule(rule)
	for _, s := range ss {
		r := analysis.Analyze(s)
		if r.Turns > 0 {
			perPrompt = append(perPrompt, float64(r.Tokens.Processed)/float64(r.Turns))
		}
		if r.Tokens.BeforeFirstEdit >= 0 {
			explore = append(explore, r.Tokens.BeforeFirstEdit)
		}
		if r.Tools.Calls > 0 {
			fail = append(fail, float64(r.Tools.Failed)/float64(r.Tools.Calls))
		}
		if measurable && slices.ContainsFunc(insight.Generate(s, r), func(f insight.Finding) bool { return f.Rule == rule }) {
			fires++
		}
	}
	if measurable {
		st.RuleRate = float64(fires) / float64(len(ss))
	}
	st.TokensPerPrompt = int(median(perPrompt))
	if len(explore) > 0 {
		st.ExploreShare = median(explore)
	}
	st.FailRate = median(fail)
	return st
}

// perSessionRule reports rules that Generate can re-check on one session;
// cross-session and AI findings can't be re-checked this way.
func perSessionRule(rule string) bool {
	switch rule {
	case "redundant-reads", "broad-search", "exploration-heavy", "mcp-before-edit", "mcp-heavy", "repeated-calls",
		"test-failures", "no-tests", "heavy-turn", "large-outputs", "fresh-session", "outdated-model",
		"explore-on-top-model", "routine-on-top-model", "struggle-on-smaller-model", "secrets":
		return true
	}
	return false
}

func median(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	s := slices.Clone(v)
	sort.Float64s(s)
	if n := len(s); n%2 == 1 {
		return s[n/2]
	} else {
		return (s[n/2-1] + s[n/2]) / 2
	}
}
