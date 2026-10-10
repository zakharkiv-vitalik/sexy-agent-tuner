// Package llm interprets a user's prompts with Claude: recurring intents and
// habits, and where each belongs (CLAUDE.md, skill, slash command, subagent,
// script). It sees only a compact package built from stored prompts and
// session sizes, never raw JSONL, and every finding cites prompt ids that are
// mapped back to session events.
package llm

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/zakharkiv-vitalik/sexy-agent-tuner/internal/insight"
	"github.com/zakharkiv-vitalik/sexy-agent-tuner/internal/storage"
)

const (
	Model = "claude-opus-5-5"
	// Budget for the prompt package. Prompts are taken newest first until it is
	// spent; the result reports how many were analyzed so nothing is cut silently.
	maxInputChars  = 120_000
	maxPromptChars = 600 // the opening of a long pasted template is enough to spot repeats
)

// ErrNoAPIKey means neither ANTHROPIC_API_KEY nor the key file is set.
var ErrNoAPIKey = errors.New("no Anthropic API key: set ANTHROPIC_API_KEY or save the key to ~/.awo/anthropic_api_key")

// KeyPath is the fallback key location. A desktop app started from Finder
// does not inherit shell variables, so the env var alone is not enough.
func KeyPath() string { return filepath.Join(filepath.Dir(storage.DefaultPath()), "anthropic_api_key") }

func apiKey() string {
	if k := os.Getenv("ANTHROPIC_API_KEY"); k != "" {
		return k
	}
	b, _ := os.ReadFile(KeyPath())
	return strings.TrimSpace(string(b))
}

// HasAPIKey reports whether an analysis can run.
func HasAPIKey() bool { return apiKey() != "" }

// Result is a prompt analysis of one scope: a repo path, or "" for all repos.
type Result struct {
	Scope     string            `json:"scope"`
	Findings  []insight.Finding `json:"findings"`
	Analyzed  int               `json:"analyzed"` // prompts sent
	Total     int               `json:"total"`    // prompts in scope
	Model     string            `json:"model"`
	CreatedAt string            `json:"createdAt"`
	Cached    bool              `json:"cached"`
}

// Cached returns the stored analysis of scope, or nil. It never calls the API.
func Cached(ctx context.Context, db *storage.DB, scope string) (*Result, error) {
	a, err := db.LoadAnalysis(ctx, scope)
	if err != nil || a == nil {
		return nil, err
	}
	r := &Result{Scope: scope, Analyzed: a.Analyzed, Total: a.Total, Model: a.Model,
		CreatedAt: a.CreatedAt.Format(time.RFC3339), Cached: true}
	return r, json.Unmarshal(a.Findings, &r.Findings)
}

// Analyze sends scope's prompts to Claude and stores the result. Unless force
// is set, an unchanged prompt package returns the stored result without a call.
func Analyze(ctx context.Context, db *storage.DB, scope string, force bool) (*Result, error) {
	in, err := gather(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	if len(in.ids) == 0 {
		return nil, errors.New("no prompts to analyze in this scope")
	}
	hash := in.hash()
	if !force {
		if a, err := db.LoadAnalysis(ctx, scope); err == nil && a != nil && a.InputHash == hash {
			return Cached(ctx, db, scope)
		}
	}
	key := apiKey()
	if key == "" {
		return nil, ErrNoAPIKey
	}

	findings, err := call(ctx, anthropic.NewClient(option.WithAPIKey(key)), in)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(findings)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	if err := db.SaveAnalysis(ctx, storage.Analysis{Scope: scope, InputHash: hash, Model: Model, CreatedAt: now,
		Analyzed: len(in.ids), Total: in.total, Findings: raw}); err != nil {
		return nil, err
	}
	return &Result{Scope: scope, Findings: findings, Analyzed: len(in.ids), Total: in.total, Model: Model,
		CreatedAt: now.Format(time.RFC3339)}, nil
}

// input is the rendered prompt package plus the id -> event mapping.
type input struct {
	text  string
	ids   map[string]insight.SessionRef
	total int
}

func (in input) hash() string {
	h := sha256.Sum256([]byte(Model + "\x00" + systemPrompt + "\x00" + in.text))
	return hex.EncodeToString(h[:])
}

var pasted = regexp.MustCompile(`</?pasted_content[^>]*>`)

func gather(ctx context.Context, db *storage.DB, scope string) (input, error) {
	sessions, err := db.ListSessions(ctx)
	if err != nil {
		return input{}, err
	}
	bySession := map[string]storage.SessionSummary{}
	for _, s := range sessions {
		bySession[s.ID] = s
	}
	prompts, err := db.ListPrompts(ctx, scope) // newest first
	if err != nil {
		return input{}, err
	}
	return render(prompts, bySession, scope == ""), nil
}

// render keeps the newest prompts that fit the budget and lays them out by
// session, oldest session first, so the model reads each session as a story.
func render(prompts []storage.Prompt, bySession map[string]storage.SessionSummary, showRepo bool) input {
	in := input{ids: map[string]insight.SessionRef{}, total: len(prompts)}
	type line struct {
		p    storage.Prompt
		text string
	}
	var kept []line
	used := 0
	for _, p := range prompts {
		t := strings.Join(strings.Fields(pasted.ReplaceAllString(p.Text, " ")), " ")
		if r := []rune(t); len(r) > maxPromptChars {
			t = string(r[:maxPromptChars]) + "…"
		}
		if used+len(t) > maxInputChars {
			break
		}
		used += len(t)
		kept = append(kept, line{p, t})
	}
	slices.SortStableFunc(kept, func(a, b line) int {
		return cmp.Or(bySession[a.p.SessionID].StartedAt.Compare(bySession[b.p.SessionID].StartedAt),
			cmp.Compare(a.p.SessionID, b.p.SessionID), cmp.Compare(a.p.Seq, b.p.Seq))
	})

	var b strings.Builder
	session := ""
	for i, l := range kept {
		if l.p.SessionID != session {
			session = l.p.SessionID
			s := bySession[session]
			fmt.Fprintf(&b, "\n## session %s · %s · %s tokens processed", session[:min(8, len(session))],
				s.StartedAt.Format("2006-01-02"), tokens(s.Tokens))
			if showRepo {
				fmt.Fprintf(&b, " · repo %s", filepath.Base(s.Project))
			}
			b.WriteString("\n")
		}
		id := fmt.Sprintf("p%d", i+1)
		in.ids[id] = insight.SessionRef{Session: l.p.SessionID, Seq: l.p.Seq}
		fmt.Fprintf(&b, "%s: %s\n", id, l.text)
	}
	in.text = b.String()
	return in
}

func tokens(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1000:
		return fmt.Sprintf("%.0fk", float64(n)/1e3)
	}
	return fmt.Sprint(n)
}

const systemPrompt = `You review how one developer prompts a coding agent (Claude Code). You get their prompts, grouped by session, with each session's size in processed tokens. Your job: find what they could set up once instead of repeating, and prompt habits that cost them tokens or rework.

Look for:
- Recurring instructions or intents across sessions, even when worded differently or in different languages (prompts are often in Ukrainian or mixed with English).
- Recurring procedures (review this repo, run tests and show results, commit and open a PR, explain architecture).
- Habits that correlate with large sessions: vague or open-ended requests, many short follow-ups like "show the result" or "do it", missing done-criteria.

For each finding choose where it belongs:
- claude_md: a standing rule that applies to every session in the repo (or every repo, if it recurs across repos).
- skill: a multi-step procedure needed only for some tasks; loaded on demand when its description matches.
- slash_command: a template the user invokes explicitly by name.
- subagent: a heavy, self-contained investigation whose raw output the main session does not need, only a summary.
- script: deterministic steps that need no model judgment (a script, Makefile target or hook).
- prompt_habit: a change in how the user phrases requests.

Rules:
- Base every finding on the prompts given. Cite the prompt ids (p1, p2, …) that show it in evidence; a recurring pattern needs at least two.
- Prefer a few solid findings over many weak ones. Return an empty list if nothing qualifies.
- observation states what the prompts show (counts, which sessions); hypothesis is your interpretation; keep them separate.
- snippet is ready to paste: the CLAUDE.md rule, the SKILL.md body, the command template, or a better prompt. Write the snippet in the language the user writes their prompts in; write the other fields in English.
- confidence is "medium" when the evidence is clear and repeated, otherwise "low".`

// llmFinding is the response schema. Field names match the JSON schema below.
type llmFinding struct {
	Kind        string   `json:"kind"`
	Title       string   `json:"title"`
	Observation string   `json:"observation"`
	Hypothesis  string   `json:"hypothesis"`
	Suggestion  string   `json:"suggestion"`
	Snippet     string   `json:"snippet"`
	Benefit     string   `json:"benefit"`
	Downside    string   `json:"downside"`
	Confidence  string   `json:"confidence"`
	Evidence    []string `json:"evidence"`
}

var responseSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["findings"],
  "properties": {
    "findings": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["kind", "title", "observation", "hypothesis", "suggestion", "snippet", "benefit", "downside", "confidence", "evidence"],
        "properties": {
          "kind": {"type": "string", "enum": ["claude_md", "skill", "slash_command", "subagent", "script", "prompt_habit"]},
          "title": {"type": "string"},
          "observation": {"type": "string"},
          "hypothesis": {"type": "string"},
          "suggestion": {"type": "string"},
          "snippet": {"type": "string"},
          "benefit": {"type": "string"},
          "downside": {"type": "string"},
          "confidence": {"type": "string", "enum": ["medium", "low"]},
          "evidence": {"type": "array", "items": {"type": "string"}}
        }
      }
    }
  }
}`)

func call(ctx context.Context, client anthropic.Client, in input) ([]insight.Finding, error) {
	stream := client.Beta.Messages.NewStreaming(ctx, anthropic.BetaMessageNewParams{
		Model:     Model,
		MaxTokens: 64000,
		System:    []anthropic.BetaTextBlockParam{{Text: systemPrompt}},
		Messages: []anthropic.BetaMessageParam{
			anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("<prompts>" + in.text + "</prompts>")),
		},
		OutputConfig: anthropic.BetaOutputConfigParam{
			Effort: anthropic.BetaOutputConfigEffortHigh,
			Format: anthropic.BetaJSONOutputFormatParam{Schema: responseSchema},
		},
		// A declined request is re-served by Anthropic's recommended fallback model.
		Fallbacks: anthropic.BetaFallbacksParamOfDefault(),
		Betas:     []anthropic.AnthropicBeta{anthropic.AnthropicBetaServerSideFallback2026_07_01},
	})
	msg := anthropic.BetaMessage{}
	for stream.Next() {
		if err := msg.Accumulate(stream.Current()); err != nil {
			return nil, err
		}
	}
	if err := stream.Err(); err != nil {
		return nil, fmt.Errorf("claude: %w", err)
	}
	switch msg.StopReason {
	case anthropic.BetaStopReasonRefusal:
		return nil, errors.New("claude declined to analyze these prompts")
	case anthropic.BetaStopReasonMaxTokens:
		return nil, errors.New("claude's answer hit the output limit")
	}
	var text strings.Builder
	for _, block := range msg.Content {
		if t, ok := block.AsAny().(anthropic.BetaTextBlock); ok {
			text.WriteString(t.Text)
		}
	}
	return parse(text.String(), in.ids)
}

var target = map[string]insight.Target{
	"claude_md":     insight.TargetContext,
	"skill":         "skill",
	"slash_command": "slash-command",
	"subagent":      "subagent",
	"script":        "script",
	"prompt_habit":  insight.TargetPrompt,
}

// parse turns the structured answer into findings, keeping only evidence ids
// that exist in the package so a hallucinated id can't point anywhere.
func parse(text string, ids map[string]insight.SessionRef) ([]insight.Finding, error) {
	var out struct {
		Findings []llmFinding `json:"findings"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		return nil, fmt.Errorf("parse claude answer: %w", err)
	}
	findings := []insight.Finding{}
	for _, f := range out.Findings {
		var refs []insight.SessionRef
		for _, id := range f.Evidence {
			if ref, ok := ids[strings.TrimSpace(id)]; ok {
				refs = append(refs, ref)
			}
		}
		if len(refs) == 0 {
			continue // a finding we can't trace back to a prompt isn't shown
		}
		findings = append(findings, insight.Finding{
			Rule: "llm-" + f.Kind, Target: cmp.Or(target[f.Kind], insight.TargetPrompt),
			Title: f.Title, Observation: f.Observation, Hypothesis: f.Hypothesis, Suggestion: f.Suggestion,
			Snippet: f.Snippet, Benefit: f.Benefit, Downside: f.Downside, Confidence: f.Confidence, Sessions: refs,
		})
	}
	return findings, nil
}
