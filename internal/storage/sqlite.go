// Package storage persists normalized sessions in a local SQLite database.
package storage

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/zakharkiv-vitalik/sexy-agent-tuner/internal/session"
)

//go:embed schema.sql
var schema string

// ErrNotFound is returned when a session id does not exist.
var ErrNotFound = errors.New("session not found")

// DB is the application store.
type DB struct{ db *sql.DB }

// DefaultPath is the database location shared by the CLI and the desktop app.
// It keeps the pre-rename ~/.awo location so existing imports survive.
func DefaultPath() string {
	if dir, err := os.UserHomeDir(); err == nil {
		return filepath.Join(dir, ".awo", "awo.db")
	}
	return "awo.db"
}

// Open opens (creating if needed) the database at path and applies the schema.
func Open(ctx context.Context, path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite3", path+"?_foreign_keys=on&_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, err
	}
	if _, err := db.ExecContext(ctx, schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return &DB{db: db}, nil
}

func (d *DB) Close() error { return d.db.Close() }

// SaveSession replaces any stored copy of the session, so re-importing a
// session that grew since last time is safe.
func (d *DB) SaveSession(ctx context.Context, s *session.Session) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	warnings, _ := json.Marshal(s.Warnings)
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, s.ID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO sessions (id, provider, project, source_path, started_at, finished_at, imported_at, warnings)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		s.ID, s.Provider, s.Project, s.SourcePath, ts(s.StartedAt), ts(s.FinishedAt), ts(time.Now()), string(warnings),
	); err != nil {
		return fmt.Errorf("insert session: %w", err)
	}

	stmt, err := tx.PrepareContext(ctx, `INSERT INTO events (
		session_id, seq, kind, ts, agent, message_id, text,
		model, input_tokens, output_tokens, cache_read, cache_creation,
		tool_id, tool_name, tool_input, tool_output, output_bytes, is_error, has_result, duration_ms
	) VALUES (?,?,?,?,?,?,?, ?,?,?,?,?, ?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, e := range s.Events {
		args := []any{s.ID, e.Seq, string(e.Kind), ts(e.Timestamp), e.Agent, e.MessageID, e.Text}
		if u := e.Usage; u != nil {
			args = append(args, u.Model, u.Input, u.Output, u.CacheRead, u.CacheCreation)
		} else {
			args = append(args, nil, nil, nil, nil, nil)
		}
		if t := e.Tool; t != nil {
			args = append(args, t.ID, t.Name, string(t.Input), t.Output, t.OutputBytes, t.IsError, t.HasResult, t.Duration.Milliseconds())
		} else {
			args = append(args, nil, nil, nil, nil, nil, nil, nil, nil)
		}
		if _, err := stmt.ExecContext(ctx, args...); err != nil {
			return fmt.Errorf("insert event %d: %w", e.Seq, err)
		}
	}
	return tx.Commit()
}

// SessionSummary is a row of the session list.
type SessionSummary struct {
	ID, Provider, Project string
	StartedAt, FinishedAt time.Time
	Events                int
	Prompts               int
	Tokens                int // processed: input + cache + output over all model calls
	PeakContext           int
}

func (d *DB) ListSessions(ctx context.Context) ([]SessionSummary, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT s.id, s.provider, s.project, s.started_at, s.finished_at, COUNT(e.seq),
			COUNT(CASE WHEN e.kind = 'user_prompt' AND e.agent = '' THEN 1 END),
			COALESCE(SUM(e.input_tokens + e.cache_read + e.cache_creation + e.output_tokens), 0),
			COALESCE(MAX(e.input_tokens + e.cache_read + e.cache_creation), 0)
		FROM sessions s LEFT JOIN events e ON e.session_id = s.id
		GROUP BY s.id ORDER BY s.started_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SessionSummary
	for rows.Next() {
		var r SessionSummary
		var start, end sql.NullString
		if err := rows.Scan(&r.ID, &r.Provider, &r.Project, &start, &end, &r.Events, &r.Prompts, &r.Tokens, &r.PeakContext); err != nil {
			return nil, err
		}
		r.StartedAt, r.FinishedAt = parseTS(start), parseTS(end)
		out = append(out, r)
	}
	return out, rows.Err()
}

// LoadSession reads a full session with its events. An id prefix is accepted
// when it is unambiguous, like git short hashes.
func (d *DB) LoadSession(ctx context.Context, id string) (*session.Session, error) {
	full, err := d.resolveID(ctx, id)
	if err != nil {
		return nil, err
	}

	s := &session.Session{ID: full}
	var start, end sql.NullString
	var warnings string
	err = d.db.QueryRowContext(ctx,
		`SELECT provider, project, source_path, started_at, finished_at, warnings FROM sessions WHERE id = ?`, s.ID,
	).Scan(&s.Provider, &s.Project, &s.SourcePath, &start, &end, &warnings)
	if err != nil {
		return nil, err
	}
	s.StartedAt, s.FinishedAt = parseTS(start), parseTS(end)
	_ = json.Unmarshal([]byte(warnings), &s.Warnings)

	rows, err := d.db.QueryContext(ctx, `SELECT seq, kind, ts, agent, message_id, text,
		model, input_tokens, output_tokens, cache_read, cache_creation,
		tool_id, tool_name, tool_input, tool_output, output_bytes, is_error, has_result, duration_ms
		FROM events WHERE session_id = ? ORDER BY seq`, s.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var e session.Event
		var kind string
		var evTS sql.NullString
		var model, toolID, toolName, toolInput, toolOutput sql.NullString
		var in, out, cr, cc, outBytes, durMS sql.NullInt64
		var isErr, hasRes sql.NullBool
		if err := rows.Scan(&e.Seq, &kind, &evTS, &e.Agent, &e.MessageID, &e.Text,
			&model, &in, &out, &cr, &cc,
			&toolID, &toolName, &toolInput, &toolOutput, &outBytes, &isErr, &hasRes, &durMS); err != nil {
			return nil, err
		}
		e.Kind, e.Timestamp = session.Kind(kind), parseTS(evTS)
		if model.Valid || in.Valid {
			e.Usage = &session.Usage{Model: model.String, Input: int(in.Int64), Output: int(out.Int64),
				CacheRead: int(cr.Int64), CacheCreation: int(cc.Int64)}
		}
		if toolName.Valid {
			e.Tool = &session.ToolCall{ID: toolID.String, Name: toolName.String, Input: json.RawMessage(toolInput.String),
				Output: toolOutput.String, OutputBytes: int(outBytes.Int64), IsError: isErr.Bool, HasResult: hasRes.Bool,
				Duration: time.Duration(durMS.Int64) * time.Millisecond}
		}
		s.Events = append(s.Events, e)
	}
	return s, rows.Err()
}

// Prompt is one human prompt, for listing a project's requests.
type Prompt struct {
	SessionID string
	Seq       int
	At        time.Time
	Text      string
}

// ListPrompts returns the human prompts of a project's sessions, newest first.
func (d *DB) ListPrompts(ctx context.Context, project string) ([]Prompt, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT e.session_id, e.seq, e.ts, e.text
		FROM events e JOIN sessions s ON s.id = e.session_id
		WHERE s.project = ? AND e.kind = 'user_prompt' AND e.agent = ''
		ORDER BY e.ts DESC, e.seq DESC`, project)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Prompt
	for rows.Next() {
		var p Prompt
		var at sql.NullString
		if err := rows.Scan(&p.SessionID, &p.Seq, &at, &p.Text); err != nil {
			return nil, err
		}
		p.At = parseTS(at)
		out = append(out, p)
	}
	return out, rows.Err()
}

// LoadSessions reads every session, or only those of project when it is set.
// ponytail: loads everything into memory; page or pre-aggregate if the DB grows to thousands.
func (d *DB) LoadSessions(ctx context.Context, project string) ([]*session.Session, error) {
	list, err := d.ListSessions(ctx)
	if err != nil {
		return nil, err
	}
	var out []*session.Session
	for _, row := range list {
		if project != "" && row.Project != project {
			continue
		}
		s, err := d.LoadSession(ctx, row.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

func (d *DB) resolveID(ctx context.Context, id string) (string, error) {
	rows, err := d.db.QueryContext(ctx,
		`SELECT id FROM sessions WHERE id LIKE ? || '%' ORDER BY id = ? DESC LIMIT 2`, id, id)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return "", err
		}
		ids = append(ids, s)
	}
	switch {
	case len(ids) == 0:
		return "", fmt.Errorf("%w: %s", ErrNotFound, id)
	case ids[0] == id || len(ids) == 1:
		return ids[0], nil
	default:
		return "", fmt.Errorf("session id prefix %q is ambiguous", id)
	}
}

func ts(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func parseTS(s sql.NullString) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s.String)
	return t
}
