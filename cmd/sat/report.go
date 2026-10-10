package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/zakharkiv-vitalik/sexy-agent-tuner/internal/analysis"
	"github.com/zakharkiv-vitalik/sexy-agent-tuner/internal/insight"
	"github.com/zakharkiv-vitalik/sexy-agent-tuner/internal/llm"
	"github.com/zakharkiv-vitalik/sexy-agent-tuner/internal/storage"
)

func reportCmd(openDB func(*cobra.Command) (*storage.DB, error)) *cobra.Command {
	var asJSON, timeline bool
	cmd := &cobra.Command{
		Use:   "show <session-id>",
		Short: "Show deterministic metrics for a session (id prefix accepted)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := openDB(cmd)
			if err != nil {
				return err
			}
			defer db.Close()
			s, err := db.LoadSession(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			r := analysis.Analyze(s)
			findings := insight.Generate(s, r)
			switch {
			case asJSON:
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(struct {
					Report   *analysis.Report
					Findings []insight.Finding
				}{r, findings})
			case timeline:
				printTimeline(os.Stdout, r)
			default:
				printReport(os.Stdout, r, s.Project)
				printFindings(os.Stdout, findings)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the full report as JSON")
	cmd.Flags().BoolVar(&timeline, "timeline", false, "print the chronological trajectory")
	return cmd
}

func insightsCmd(openDB func(*cobra.Command) (*storage.DB, error)) *cobra.Command {
	var asJSON bool
	var project string
	cmd := &cobra.Command{
		Use:   "insights",
		Short: "Find patterns that recur across imported sessions (CLAUDE.md, skill, subagent or script candidates)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			db, err := openDB(cmd)
			if err != nil {
				return err
			}
			defer db.Close()
			ss, err := db.LoadSessions(cmd.Context(), project)
			if err != nil {
				return err
			}
			findings := insight.Across(ss)
			if asJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(findings)
			}
			fmt.Printf("Across %d sessions (evidence: session#seq, open with `sat show <session> --timeline`)\n", len(ss))
			printFindings(os.Stdout, findings)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print findings as JSON")
	cmd.Flags().StringVar(&project, "project", "", "only sessions of this project directory")
	return cmd
}

func analyzeCmd(openDB func(*cobra.Command) (*storage.DB, error)) *cobra.Command {
	var project string
	var force, asJSON bool
	cmd := &cobra.Command{
		Use:   "analyze",
		Short: "Analyze your prompts with Claude (sends prompt text to the Anthropic API)",
		Long: "Sends the text of your prompts (one repo with --project, otherwise all repos) and session sizes to the\n" +
			"Anthropic API and suggests what to move into CLAUDE.md, skills, slash commands, subagents or scripts.\n" +
			"Needs ANTHROPIC_API_KEY or a key in " + llm.KeyPath() + ". An unchanged prompt set reuses the stored result.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			db, err := openDB(cmd)
			if err != nil {
				return err
			}
			defer db.Close()
			r, err := llm.Analyze(cmd.Context(), db, project, force)
			if err != nil {
				return err
			}
			if asJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(r)
			}
			from := "fresh"
			if r.Cached {
				from = "cached, use --force to re-run"
			}
			fmt.Printf("%s analyzed %d of %d prompts (%s)\n", r.Model, r.Analyzed, r.Total, from)
			printFindings(os.Stdout, r.Findings)
			return nil
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "only prompts of this project directory")
	cmd.Flags().BoolVar(&force, "force", false, "call the API even if the prompt set is unchanged")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the result as JSON")
	return cmd
}

func printReport(w io.Writer, r *analysis.Report, project string) {
	p := func(format string, a ...any) { fmt.Fprintf(w, format+"\n", a...) }
	rel := func(path string) string { return strings.TrimPrefix(path, project+"/") }
	refs := func(rs []analysis.Ref) string {
		var out []string
		for _, x := range rs {
			out = append(out, fmt.Sprintf("#%d", x.Seq))
		}
		return strings.Join(out, " ")
	}

	p("Session %s", r.SessionID)
	p("  duration %s · %d human turn(s) · %d model calls · %d tool calls (%d failed)",
		r.Duration.Round(time.Second), r.Turns, r.Tokens.ModelCalls, r.Tools.Calls, r.Tools.Failed)

	t := r.Tokens
	p("\nTokens")
	p("  output %s · fresh input %s · cache write %s · cache read %s · processed %s",
		k(t.Output), k(t.Input), k(t.CacheCreation), k(t.CacheRead), k(t.Processed))
	p("  peak context %s", k(t.PeakContext))
	if t.BeforeFirstEdit >= 0 {
		p("  %.0f%% of processed tokens spent before the first edit", t.BeforeFirstEdit*100)
	} else {
		p("  no file was edited")
	}
	for _, tt := range t.ByTurn {
		p("  turn %d: %d calls, %s processed — %q", tt.Turn, tt.ModelCalls, k(tt.Processed), short(tt.Prompt, 60))
	}

	p("\nTools (by behavior)")
	for _, c := range []analysis.Category{analysis.CatRead, analysis.CatSearch, analysis.CatEdit, analysis.CatTest,
		analysis.CatMCP, analysis.CatShell, analysis.CatAgent, analysis.CatSkill, analysis.CatPlan, analysis.CatWeb, analysis.CatOther} {
		if n := r.Tools.ByCategory[c]; n > 0 {
			fmt.Fprintf(w, "  %s=%d", c, n)
		}
	}
	p("")
	p("  %-32s %6s %6s %9s %12s", "TOOL", "CALLS", "FAILED", "TIME", "~RESULT TOK")
	for _, c := range r.Tools.ByName {
		p("  %-32s %6d %6d %9s %12s", short(c.Name, 32), c.Calls, c.Failed, c.Duration.Round(time.Second), k(c.ResultTokens))
	}
	for _, rep := range r.Tools.Repeated {
		p("  repeated ×%d: %s  %v", rep.Count, short(rep.Key, 70), rep.Seqs)
	}
	if len(r.Tools.LongRunning) > 0 {
		p("  long-running (≥%s): %s", analysis.LongRunning, refs(r.Tools.LongRunning))
	}

	f := r.Files
	p("\nFiles")
	p("  %d unique read (%d reads) · %d read before first edit · %d edited · %d directories",
		f.UniqueRead, f.Reads, f.ReadBeforeFirstEdit, len(f.Edited), len(f.Directories))
	for _, rep := range f.RedundantReads {
		p("  re-read without edit ×%d: %s  %v", rep.Count, rel(rep.Key), rep.Seqs)
	}
	if len(f.BroadSearches) > 0 {
		p("  broad searches: %s", refs(f.BroadSearches))
	}
	for _, e := range f.Edited {
		p("  edited: %s", rel(e))
	}

	ts := r.Tests
	p("\nTests")
	p("  %d run(s), %d failed, %d repeated · style: %s", ts.Runs, ts.Failed, ts.Repeated, ts.Style)
	if ts.FirstEditToFirstTest > 0 {
		p("  first edit → first test: %s", ts.FirstEditToFirstTest.Round(time.Second))
	}

	if r.MCP.Calls > 0 {
		p("\nMCP")
		p("  %d call(s), %d before the first edit", r.MCP.Calls, r.MCP.BeforeFirstEdit)
		for _, c := range r.MCP.ByTool {
			p("  %-32s %d call(s) %s", c.Name, c.Calls, c.Duration.Round(time.Second))
		}
		for _, rep := range r.MCP.Repeated {
			p("  identical call ×%d: %v", rep.Count, rep.Seqs)
		}
		p("  relevance: unknown (needs strategy context; not judged from metrics alone)")
	}

	if len(r.Subagents)+len(r.Skills) > 0 {
		p("\nDelegation")
		for _, x := range r.Subagents {
			p("  subagent #%d %s", x.Seq, short(x.Label, 80))
		}
		for _, x := range r.Skills {
			p("  skill    #%d %s", x.Seq, short(x.Label, 80))
		}
	}

	if len(r.Failures) > 0 {
		p("\nFailures")
		for _, x := range r.Failures {
			p("  #%-4d +%-7s %s", x.Seq, x.Offset.Round(time.Second), short(x.Label, 100))
		}
	}
}

func printFindings(w io.Writer, fs []insight.Finding) {
	fmt.Fprintln(w, "\nWhat to improve (hypotheses to test, not proven fixes)")
	if len(fs) == 0 {
		fmt.Fprintln(w, "  No pattern crossed its threshold.")
	}
	for i, f := range fs {
		fmt.Fprintf(w, "\n  %d. %s  [%s · %s confidence]\n", i+1, f.Title, f.Target, f.Confidence)
		fmt.Fprintf(w, "     FACT        %s\n", f.Observation)
		fmt.Fprintf(w, "     HYPOTHESIS  %s\n", f.Hypothesis)
		fmt.Fprintf(w, "     TRY         %s\n", f.Suggestion)
		if f.Snippet != "" {
			fmt.Fprintf(w, "     SNIPPET     %s\n", strings.ReplaceAll(f.Snippet, "\n", "\n                 "))
		}
		fmt.Fprintf(w, "     BENEFIT     %s\n     DOWNSIDE    %s\n", f.Benefit, f.Downside)
		var ev []string
		for _, seq := range f.Evidence {
			ev = append(ev, fmt.Sprintf("#%d", seq))
		}
		for _, ref := range f.Sessions {
			ev = append(ev, fmt.Sprintf("%s#%d", ref.Session[:min(8, len(ref.Session))], ref.Seq))
		}
		fmt.Fprintf(w, "     EVIDENCE    %s\n", strings.Join(ev, " "))
	}
}

func printTimeline(w io.Writer, r *analysis.Report) {
	for _, e := range r.Timeline {
		kind := string(e.Category)
		switch e.Kind {
		case "user_prompt":
			kind = "PROMPT"
		case "assistant_text":
			kind = "says"
		}
		mark := " "
		if e.IsError {
			mark = "✗"
		}
		agent := ""
		if e.Agent != "" {
			agent = "[" + short(e.Agent, 8) + "] "
		}
		fmt.Fprintf(w, "#%-5d +%-8s %s %-8s %6s  %s%s\n", e.Seq, e.Offset.Round(time.Second), mark, kind, k(e.Context), agent, short(e.Label, 110))
	}
}

// k formats token counts compactly.
func k(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1000:
		return fmt.Sprintf("%.1fk", float64(n)/1e3)
	}
	return fmt.Sprint(n)
}
