package importer

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zakharkiv-vitalik/sexy-agent-tuner/internal/storage"
)

func TestSync(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db, err := storage.Open(ctx, filepath.Join(t.TempDir(), "awo.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	fixture, err := os.ReadFile("../session/testdata/valid_to_9999.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	proj := filepath.Join(root, "-work-snapshots-svc")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(proj, "valid-to-9999.jsonl")
	write := func(name string, b []byte) {
		if err := os.WriteFile(name, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(log, fixture)
	write(filepath.Join(proj, "empty.jsonl"), nil)
	write(filepath.Join(proj, "no-prompt.jsonl"), []byte(`{"type":"assistant","message":{"role":"assistant","content":"hi"}}`+"\n"))

	sync := func() Result {
		t.Helper()
		r, err := Sync(ctx, db, root)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	if r := sync(); r != (Result{Imported: 1}) {
		t.Fatalf("first sync = %+v, want 1 imported (empty and prompt-less logs skipped)", r)
	}
	if r := sync(); r != (Result{}) {
		t.Fatalf("unchanged sync = %+v, want nothing", r)
	}
	future := time.Now().Add(time.Hour) // the log grew after it was imported
	if err := os.Chtimes(log, future, future); err != nil {
		t.Fatal(err)
	}
	if r := sync(); r != (Result{Updated: 1}) {
		t.Fatalf("changed sync = %+v, want 1 updated", r)
	}
}
