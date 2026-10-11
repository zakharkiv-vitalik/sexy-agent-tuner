package insight

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/zakharkiv-vitalik/sexy-agent-tuner/internal/secrets"
	"github.com/zakharkiv-vitalik/sexy-agent-tuner/internal/session"
)

// TargetSecurity: act by rotating a credential and keeping it out of prompts.
const TargetSecurity Target = "security"

// leak is the secrets found in one session: kind -> masked values, plus where.
type leak struct {
	kinds    map[string][]string
	seqs     []int
	inPrompt bool
}

func scanSecrets(s *session.Session) leak {
	l := leak{kinds: map[string][]string{}}
	for _, e := range s.Events {
		texts := []string{e.Text}
		if e.Tool != nil {
			texts = append(texts, string(e.Tool.Input), e.Tool.Output)
		}
		found := false
		for _, t := range texts {
			for _, m := range secrets.Find(t) {
				found = true
				if v := secrets.Mask(m.Value); !slices.Contains(l.kinds[m.Kind], v) {
					l.kinds[m.Kind] = append(l.kinds[m.Kind], v)
				}
			}
		}
		if found {
			l.seqs = append(l.seqs, e.Seq)
			l.inPrompt = l.inPrompt || e.Kind == session.KindUserPrompt
		}
	}
	return l
}

func (l leak) summary() string {
	var parts []string
	for _, k := range slices.Sorted(maps.Keys(l.kinds)) {
		parts = append(parts, fmt.Sprintf("%s (%s)", k, strings.Join(l.kinds[k], ", ")))
	}
	return strings.Join(parts, "; ")
}

const secretRule = "Never put tokens, API keys or passwords in commands or files. Read them from environment variables " +
	"(for example $REDMINE_API_KEY) and refer to them by name."

// secretsInSession flags credentials sitting in the session log.
func secretsInSession(f *facts) *Finding {
	l := scanSecrets(f.s)
	if len(l.seqs) == 0 {
		return nil
	}
	where := "commands or tool output"
	if l.inPrompt {
		where = "your prompts and in commands or tool output"
	}
	return &Finding{
		Rule: "secrets", Target: TargetSecurity, Confidence: "medium",
		Title:       fmt.Sprintf("Credentials appear in this session's log (%d event(s))", len(l.seqs)),
		Observation: fmt.Sprintf("Found in %s: %s.", where, l.summary()),
		Hypothesis:  "They were pasted or used inline, so they now sit in plain text in ~/.claude/projects and were sent to the model as context.",
		Suggestion:  "Rotate these credentials if they matter, keep them in environment variables, and add a CLAUDE.md rule so the agent does the same.",
		Snippet:     secretRule,
		Benefit:     "Keys stop spreading into logs, context and anything you share. The AI analysis redacts them, but the logs still hold them.",
		Downside:    "Pattern matching can miss unusual formats and occasionally flag a non-secret; check before rotating.",
		Evidence:    l.seqs,
	}
}

// secretsAcross lists every session with credentials in its log. Unlike other
// cross-session rules it fires on a single session: one leaked key is enough.
func secretsAcross(ss []*session.Session) []Finding {
	var refs []SessionRef
	kinds := map[string]bool{}
	events := 0
	for _, s := range ss {
		l := scanSecrets(s)
		if len(l.seqs) == 0 {
			continue
		}
		refs = append(refs, SessionRef{s.ID, l.seqs[0]})
		events += len(l.seqs)
		for k := range l.kinds {
			kinds[k] = true
		}
	}
	if len(refs) == 0 {
		return nil
	}
	return []Finding{{
		Rule: "secrets", Target: TargetSecurity, Confidence: "medium",
		Title:       fmt.Sprintf("Credentials appear in %d session log(s)", len(refs)),
		Observation: fmt.Sprintf("%d of %d sessions contain %s (%d events in total).", len(refs), len(ss), strings.Join(slices.Sorted(maps.Keys(kinds)), ", "), events),
		Hypothesis:  "Keys passed inline in commands or prompts are kept in plain text in ~/.claude/projects.",
		Suggestion:  "Rotate the ones that matter and move them to environment variables; add a rule to ~/.claude/CLAUDE.md so every repo follows it.",
		Snippet:     secretRule,
		Benefit:     "Fewer copies of live credentials on disk and in model context.",
		Downside:    "Pattern matching can miss unusual formats and occasionally flag a non-secret.",
		Sessions:    refs,
	}}
}
