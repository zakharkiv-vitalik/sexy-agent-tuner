CREATE TABLE IF NOT EXISTS sessions (
    id          TEXT PRIMARY KEY,
    provider    TEXT NOT NULL,
    project     TEXT NOT NULL DEFAULT '',
    source_path TEXT NOT NULL DEFAULT '',
    started_at  TEXT,
    finished_at TEXT,
    imported_at TEXT NOT NULL,
    warnings    TEXT NOT NULL DEFAULT '[]'  -- JSON array of parse warnings
);

-- One row per normalized event. Tool calls carry their joined result;
-- model calls carry token usage.
CREATE TABLE IF NOT EXISTS events (
    session_id     TEXT    NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    seq            INTEGER NOT NULL,
    kind           TEXT    NOT NULL,
    ts             TEXT,
    agent          TEXT    NOT NULL DEFAULT '',
    message_id     TEXT    NOT NULL DEFAULT '',
    text           TEXT    NOT NULL DEFAULT '',
    model          TEXT,
    input_tokens   INTEGER,
    output_tokens  INTEGER,
    cache_read     INTEGER,
    cache_creation INTEGER,
    tool_id        TEXT,
    tool_name      TEXT,
    tool_input     TEXT,   -- JSON
    tool_output    TEXT,
    output_bytes   INTEGER,
    is_error       INTEGER,
    has_result     INTEGER,
    duration_ms    INTEGER,
    PRIMARY KEY (session_id, seq)
);

CREATE INDEX IF NOT EXISTS events_ts   ON events(session_id, ts);
CREATE INDEX IF NOT EXISTS events_tool ON events(tool_name);
CREATE INDEX IF NOT EXISTS events_kind ON events(kind);

-- Latest LLM prompt analysis per scope (repo path, or '' for all repos).
-- input_hash lets an unchanged prompt package reuse the stored result.
CREATE TABLE IF NOT EXISTS analyses (
    scope      TEXT PRIMARY KEY,
    input_hash TEXT NOT NULL,
    model      TEXT NOT NULL,
    created_at TEXT NOT NULL,
    analyzed   INTEGER NOT NULL,
    total      INTEGER NOT NULL,
    findings   TEXT NOT NULL  -- JSON array of insight findings
);
