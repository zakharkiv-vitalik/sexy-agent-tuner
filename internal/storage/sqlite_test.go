package storage

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/zakharkiv-vitalik/sexy-agent-tuner/internal/session"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "awo.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	s, err := session.ImportClaudeFile("../session/testdata/valid_to_9999.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	for range 2 { // re-import must replace, not duplicate
		if err := db.SaveSession(ctx, s); err != nil {
			t.Fatal(err)
		}
	}

	got, err := db.LoadSession(ctx, "valid-to") // prefix lookup
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Events) != len(s.Events) {
		t.Fatalf("events = %d, want %d", len(got.Events), len(s.Events))
	}
	for i := range s.Events {
		w, g := s.Events[i], got.Events[i]
		if w.Kind != g.Kind || !w.Timestamp.Equal(g.Timestamp) || (w.Tool == nil) != (g.Tool == nil) || (w.Usage == nil) != (g.Usage == nil) {
			t.Fatalf("event %d: got %+v, want %+v", i, g, w)
		}
		if w.Tool != nil && (w.Tool.Name != g.Tool.Name || w.Tool.IsError != g.Tool.IsError || w.Tool.Duration != g.Tool.Duration || string(w.Tool.Input) != string(g.Tool.Input)) {
			t.Fatalf("event %d tool: got %+v, want %+v", i, g.Tool, w.Tool)
		}
		if w.Usage != nil && *w.Usage != *g.Usage {
			t.Fatalf("event %d usage: got %+v, want %+v", i, g.Usage, w.Usage)
		}
	}
	if len(got.Warnings) != 1 {
		t.Errorf("warnings = %v", got.Warnings)
	}

	list, err := db.ListSessions(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %v, %v", list, err)
	}
	if _, err := db.LoadSession(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing session err = %v", err)
	}
}
