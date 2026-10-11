// Package importer keeps the store in step with Claude Code's session logs:
// new sessions are imported and changed ones (still running, or resumed)
// are re-imported, so nobody has to import by hand.
package importer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zakharkiv-vitalik/sexy-agent-tuner/internal/session"
	"github.com/zakharkiv-vitalik/sexy-agent-tuner/internal/storage"
)

// DefaultRoot is where Claude Code writes session logs.
func DefaultRoot() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "projects")
}

// Result counts what a sync did. Failed files are skipped, not fatal: one
// unreadable log shouldn't block the rest.
type Result struct {
	Imported, Updated, Failed int
}

// Sync imports every session under root that is new or changed since it was
// last imported. Sessions without a human prompt (empty or aborted) are skipped.
func Sync(ctx context.Context, db *storage.DB, root string) (Result, error) {
	var r Result
	paths, err := filepath.Glob(filepath.Join(root, "*", "*.jsonl"))
	if err != nil {
		return r, err
	}
	have, err := db.ImportedSources(ctx)
	if err != nil {
		return r, err
	}
	for _, p := range paths {
		if err := ctx.Err(); err != nil {
			return r, err
		}
		mod, ok := lastModified(p)
		at, imported := have[p]
		if !ok || (imported && !mod.After(at)) {
			continue
		}
		if _, _, prompt, err := session.Peek(p); err != nil || prompt == "" {
			continue
		}
		s, err := session.ImportClaudeFile(p)
		if err != nil {
			r.Failed++
			continue
		}
		if err := db.SaveSession(ctx, s); err != nil {
			return r, fmt.Errorf("save %s: %w", p, err)
		}
		if imported {
			r.Updated++
		} else {
			r.Imported++
		}
	}
	return r, nil
}

// lastModified is the newest mtime of a session log and its subagent logs.
// ok is false for missing or empty files.
func lastModified(path string) (time.Time, bool) {
	fi, err := os.Stat(path)
	if err != nil || fi.Size() == 0 {
		return time.Time{}, false
	}
	mod := fi.ModTime()
	subs, _ := filepath.Glob(filepath.Join(strings.TrimSuffix(path, ".jsonl"), "subagents", "*.jsonl"))
	for _, sub := range subs {
		if si, err := os.Stat(sub); err == nil && si.ModTime().After(mod) {
			mod = si.ModTime()
		}
	}
	return mod, true
}
