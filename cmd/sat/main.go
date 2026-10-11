// Command sat is the Sexy Agent Tuner CLI.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/zakharkiv-vitalik/sexy-agent-tuner/internal/importer"
	"github.com/zakharkiv-vitalik/sexy-agent-tuner/internal/session"
	"github.com/zakharkiv-vitalik/sexy-agent-tuner/internal/storage"
)

func main() {
	var dbPath string
	root := &cobra.Command{Use: "sat", Short: "Analyze coding-agent sessions and improve your agent workflow", SilenceUsage: true}
	root.PersistentFlags().StringVar(&dbPath, "db", storage.DefaultPath(), "SQLite database path")

	openDB := func(cmd *cobra.Command) (*storage.DB, error) {
		return storage.Open(cmd.Context(), dbPath)
	}

	root.AddCommand(&cobra.Command{
		Use:   "import <session.jsonl>...",
		Short: "Import Claude Code session files",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := openDB(cmd)
			if err != nil {
				return err
			}
			defer db.Close()
			for _, path := range args {
				s, err := session.ImportClaudeFile(path)
				if err != nil {
					return err
				}
				if err := db.SaveSession(cmd.Context(), s); err != nil {
					return fmt.Errorf("save %s: %w", path, err)
				}
				fmt.Printf("imported %s  events=%d  warnings=%d  project=%s\n", s.ID, len(s.Events), len(s.Warnings), s.Project)
			}
			return nil
		},
	})

	var syncRoot string
	var quiet bool
	syncCmd := &cobra.Command{
		Use:   "sync",
		Short: "Import new and changed Claude Code sessions (safe to run from a SessionEnd hook)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			db, err := openDB(cmd)
			if err != nil {
				return err
			}
			defer db.Close()
			r, err := importer.Sync(cmd.Context(), db, syncRoot)
			if err != nil {
				return err
			}
			if !quiet {
				fmt.Printf("imported %d new, updated %d, failed %d\n", r.Imported, r.Updated, r.Failed)
			}
			return nil
		},
	}
	syncCmd.Flags().StringVar(&syncRoot, "root", importer.DefaultRoot(), "Claude Code projects directory")
	syncCmd.Flags().BoolVarP(&quiet, "quiet", "q", false, "print nothing on success")
	root.AddCommand(syncCmd)

	root.AddCommand(&cobra.Command{
		Use:   "sessions",
		Short: "List imported sessions",
		RunE: func(cmd *cobra.Command, _ []string) error {
			db, err := openDB(cmd)
			if err != nil {
				return err
			}
			defer db.Close()
			list, err := db.ListSessions(cmd.Context())
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tSTARTED\tDURATION\tEVENTS\tPROJECT")
			for _, s := range list {
				fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\n", s.ID, s.StartedAt.Local().Format("2006-01-02 15:04"),
					s.FinishedAt.Sub(s.StartedAt).Round(time.Second), s.Events, s.Project)
			}
			return w.Flush()
		},
	})

	root.AddCommand(reportCmd(openDB), insightsCmd(openDB), analyzeCmd(openDB))

	if err := root.ExecuteContext(context.Background()); err != nil {
		os.Exit(1)
	}
}

func short(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len([]rune(s)) <= n {
		return s
	}
	return string([]rune(s)[:n-1]) + "…"
}
