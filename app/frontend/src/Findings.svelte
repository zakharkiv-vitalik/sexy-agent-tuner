<script lang="ts">
  import type { insight } from '../wailsjs/go/models'
  import { ClipboardSetText } from '../wailsjs/runtime/runtime'
  import Refs from './Refs.svelte'

  let {
    findings,
    onShow = () => {},
    onOpenSession = () => {},
    note = "Hypotheses from this session. Try them on a similar task and compare; they aren't proven fixes.",
    limit = Infinity,
    empty = "No pattern crossed its threshold in this session. That doesn't prove the session was efficient; the rules are deliberately conservative.",
  }: {
    findings: insight.Finding[] | null
    onShow?: (seq: number) => void
    onOpenSession?: (id: string, seq: number) => void
    note?: string
    limit?: number
    empty?: string
  } = $props()

  const TARGET: Record<string, string> = {
    prompt: 'Prompt',
    context: 'CLAUDE.md / AGENTS.md',
    mcp: 'MCP',
    workflow: 'Workflow',
    tests: 'Tests',
    'tool-output': 'Tool output',
    skill: 'Skill',
    'slash-command': 'Slash command',
    subagent: 'Subagent',
    script: 'Script / hook',
  }

  let showAll = $state(false)
  const shown = $derived(showAll ? (findings ?? []) : (findings ?? []).slice(0, limit))
  const hidden = $derived((findings ?? []).length - shown.length)

  let copied = $state<number | null>(null)
  async function copy(f: insight.Finding, i: number) {
    await ClipboardSetText(f.snippet)
    copied = i
    setTimeout(() => (copied = copied === i ? null : copied), 1500)
  }
</script>

<section class="card findings">
  <div class="head">
    <h2>What to improve</h2>
    <span class="muted">{note}</span>
  </div>

  {#each shown as f, i (i)}
    <details class="finding fold">
      <summary>
        <span class="n">{i + 1}</span>
        <span class="title">{f.title}</span>
        <span class="chip">{TARGET[f.target] ?? f.target}</span>
        <span class="chip" class:low={f.confidence === 'low'}>{f.confidence} confidence</span>
      </summary>
      <div class="body">
        <div class="line"><span class="tag fact">Fact</span><span>{f.observation}</span></div>
        <div class="line"><span class="tag try">Try</span><span><b>{f.suggestion}</b></span></div>
        {#if f.snippet}
          <div class="snippet">
            <pre>{f.snippet}</pre>
            <button onclick={() => copy(f, i)}>{copied === i ? 'Copied ✓' : 'Copy'}</button>
          </div>
        {/if}
        <details class="why fold">
          <summary class="muted">Why, trade-offs, evidence</summary>
          <div class="line"><span class="tag hyp">Hypothesis</span><span class="secondary">{f.hypothesis}</span></div>
          <div class="tradeoff">
            <div><span class="muted">Expected benefit</span><br />{f.benefit}</div>
            <div><span class="muted">Possible downside</span><br />{f.downside}</div>
          </div>
          <div class="line">
            <span class="tag">Evidence</span>
            <span>
              <Refs seqs={f.evidence} {onShow} />
              {#each f.sessions ?? [] as ref}
                <button class="ref" title="Open this session at the event" onclick={() => onOpenSession(ref.session, ref.seq)}>{ref.session.slice(0, 8)}#{ref.seq}</button>
              {/each}
            </span>
          </div>
        </details>
      </div>
    </details>
  {:else}
    <p class="muted">{empty}</p>
  {/each}
  {#if hidden > 0}
    <button class="all" onclick={() => (showAll = true)}>Show {hidden} more</button>
  {/if}
</section>

<style>
  .findings { margin-bottom: 16px; }
  .all { margin-top: 8px; color: var(--accent); }
  .head { display: flex; align-items: baseline; gap: 12px; flex-wrap: wrap; margin-bottom: 6px; }
  .head h2 { margin: 0; }
  .finding { border-top: 1px solid var(--grid); }
  .finding > summary { padding: 7px 0; }
  .why { display: flex; flex-direction: column; gap: 6px; }
  .why > summary { font-size: 12px; padding: 2px 0; margin-left: 92px; }
  .why[open] { gap: 6px; }
  .n { font-variant-numeric: tabular-nums; color: var(--text-muted); width: 16px; }
  .title { font-weight: 600; flex: 1; }
  .chip { font-size: 11px; padding: 1px 7px; border-radius: 10px; background: var(--surface-2); color: var(--text-secondary); white-space: nowrap; }
  .chip.low { color: var(--text-muted); }
  .body { padding: 0 0 12px 34px; display: flex; flex-direction: column; gap: 6px; }
  .line { display: grid; grid-template-columns: 84px 1fr; gap: 8px; align-items: baseline; }
  .tag { font-size: 10px; font-weight: 600; letter-spacing: .04em; text-transform: uppercase; color: var(--text-muted); }
  .tag.fact { color: var(--text-primary); }
  .tag.try { color: var(--accent); }
  .snippet { position: relative; margin-left: 92px; }
  .snippet pre {
    margin: 0; padding: 8px 70px 8px 10px; background: var(--surface-2); border-radius: 6px;
    white-space: pre-wrap; font: 12px ui-monospace, Menlo, monospace;
  }
  .snippet button { position: absolute; top: 5px; right: 5px; font-size: 12px; padding: 2px 8px; }
  .tradeoff { margin-left: 92px; display: grid; grid-template-columns: 1fr 1fr; gap: 12px; font-size: 12px; }
</style>
