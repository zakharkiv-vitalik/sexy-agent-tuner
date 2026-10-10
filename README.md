# Sexy Agent Tuner (sat)

Local-first analysis of coding-agent sessions: what happened, where the time and
tokens went, and (later) what to change in the prompt / context / workflow.

## Pipeline

```
JSONL ──stream──▶ session.ParseClaude ──▶ session.Session (provider-independent)
                                              │
                                   storage (SQLite, ~/.awo/awo.db)
                                              │
                                   analysis.Analyze  ── deterministic FACTS, evidence = event Seq
                                              │
             (next) strategy phases ─▶ LLM postmortem ─▶ recommendations ─▶ experiments
```

Deterministic analysis establishes facts; the LLM layer (Phase 5) only interprets
a compact evidence package built from `analysis.Report`, never the raw JSONL.

## Packages

| package | role |
|---|---|
| `internal/session` | domain model (`Session`, `Event`, `ToolCall`, `Usage`) + Claude Code adapter. Streams line by line, dedupes usage of assistant messages split across lines, joins tool results to calls, pulls in `<id>/subagents/*.jsonl`. Malformed lines become `Warnings`. |
| `internal/storage` | SQLite, one `events` table (kind-tagged rows), schema embedded, re-import replaces. |
| `internal/analysis` | `Classify` maps tool calls to behavior (read/search/edit/test/mcp/…) incl. Bash equivalents (`cat`, `sed -n`, `grep -r`, heredoc writes, `go test`…). `Analyze` computes tokens, tools, files, tests, MCP, failures, timeline. |
| `internal/insight` | Improvement suggestions from deterministic rules. Each finding keeps the measured FACT apart from the HYPOTHESIS, with a copyable snippet, expected benefit, possible downside, confidence and evidence seqs. `Across` works over many sessions: instructions you keep retyping (→ CLAUDE.md or skill/slash command), files every session re-explores (→ CLAUDE.md map), identical tool chains (→ subagent, skill or script). |
| `cmd/sat` | Cobra CLI. |

Event kinds: `user_prompt`, `model_call` (carries usage), `assistant_text`, `tool_call` (carries joined result).

## Commands

```
go build -o bin/sat ./cmd/sat
bin/sat import ~/.claude/projects/<project>/<session>.jsonl
bin/sat sessions
bin/sat show <id-prefix>             # metrics report
bin/sat show <id-prefix> --timeline  # chronological trajectory
bin/sat show <id-prefix> --json      # full report for tooling / GUI
bin/sat insights [--project <dir>]   # patterns recurring across all imported sessions
go test ./...
```

## Known limits (deliberate)

- Bash classification uses a whitespace tokenizer, not a shell parser.
- MCP relevance is always `unknown` until strategy reconstruction can judge it.
- Analysis is recomputed on demand, not stored; store it when the GUI needs history.

## Desktop app (Wails + Svelte)

```
cd app && wails build          # → app/build/bin/sat.app
cd app && wails dev            # live reload; also serves the UI at http://localhost:34115
```

Screens: **Repos** (imported sessions grouped by project; import discovered sessions one by one or all at once),
**Repo** (patterns recurring across its sessions + every prompt, grouped by session; a click opens the session at that prompt),
**All repos** (patterns spanning several repos: user-level CLAUDE.md, personal skills, subagents, scripts),
**Session → Overview** (key numbers and top findings; the rest under Details), **Timeline** (behavior lanes + context
growth; click a mark or row for the event's full input/output), **Tools**.
The GUI and CLI share `~/.awo/awo.db`. `go build ./...` needs `app/frontend/dist` built once.
