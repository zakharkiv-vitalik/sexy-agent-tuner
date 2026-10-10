<script lang="ts">
  import { OpenSession } from '../wailsjs/go/main/App'
  import type { main } from '../wailsjs/go/models'
  import { dur, tok, rel, short, date } from './lib/format'
  import Timeline from './Timeline.svelte'
  import Refs from './Refs.svelte'
  import Findings from './Findings.svelte'

  let { id, seq = null }: { id: string; seq?: number | null } = $props()

  let view = $state<main.SessionView | null>(null)
  let error = $state('')
  let tab = $state<'overview' | 'timeline' | 'tools'>('overview')
  let focus = $state<number | null>(null)
  // svelte-ignore state_referenced_locally (App remounts this view per id+seq)
  if (seq !== null) show(seq) // opened from cross-session evidence

  $effect(() => {
    OpenSession(id).then(v => (view = v)).catch(e => (error = String(e)))
  })

  // Evidence refs everywhere jump to the event in the timeline.
  function show(seq: number) {
    focus = seq
    tab = 'timeline'
  }

  const r = $derived(view?.report)
  const title = $derived(r?.Tokens.ByTurn?.[0]?.Prompt ?? '')
  const maxTurn = $derived(Math.max(1, ...(r?.Tokens.ByTurn ?? []).map(t => t.Processed)))
  const categories = $derived(
    Object.entries(r?.Tools.ByCategory ?? {}).sort((a, b) => b[1] - a[1]) as [string, number][],
  )
  const maxCat = $derived(Math.max(1, ...categories.map(c => c[1])))
  const p = (path: string) => rel(path, view?.project ?? '')

  // First screen: the four numbers that say where the cost went. The rest sits under Details.
  const keyTiles = $derived(r ? [
    ['Duration', dur(r.Duration), `${r.Turns} human turn(s)`],
    ['Processed tokens', tok(r.Tokens.Processed), `${tok(r.Tokens.CacheRead)} from cache`],
    ['Peak context', tok(r.Tokens.PeakContext), 'largest prompt the model saw'],
    ['Before first edit', r.Tokens.BeforeFirstEdit < 0 ? '—' : `${Math.round(r.Tokens.BeforeFirstEdit * 100)}%`, 'of processed tokens'],
  ] : [])
  const moreTiles = $derived(r ? [
    ['Model calls', String(r.Tokens.ModelCalls), `${tok(r.Tokens.Output)} output tokens`],
    ['Tool calls', String(r.Tools.Calls), `${r.Tools.Failed} failed`],
    ['Files', `${r.Files.UniqueRead} read`, `${(r.Files.Edited ?? []).length} edited`],
    ['Tests', `${r.Tests.Runs} runs`, `${r.Tests.Failed} failed · ${r.Tests.Style}`],
    ['MCP calls', String(r.MCP.Calls), `${r.MCP.BeforeFirstEdit} before first edit`],
  ] : [])
</script>

{#if error}
  <div class="pad"><div class="card err">{error}</div></div>
{:else if !view || !r}
  <div class="pad muted">Loading session…</div>
{:else}
  <div class="wrap">
    <div class="top">
      <h1 title={title}>{short(title, 140) || view.id}</h1>
      <div class="muted">
        {view.project} · {date(view.startedAt)} · <span class="mono">{view.id}</span>
      </div>
      <nav>
        {#each [['overview', 'Overview'], ['timeline', 'Timeline'], ['tools', 'Tools']] as [k, label]}
          <button class:active={tab === k} onclick={() => (tab = k as typeof tab)}>{label}</button>
        {/each}
      </nav>
    </div>

    {#if tab === 'timeline'}
      <Timeline {view} bind:focus />
    {:else if tab === 'tools'}
      <div class="grid2 pad">
        <section class="card">
          <h2>Tool calls by behavior</h2>
          <table>
            <tbody>
              {#each categories as [cat, n]}
                <tr>
                  <td class="w120">{cat}</td>
                  <td><span class="bar" style="width:{(n / maxCat) * 100}%"></span></td>
                  <td class="num w60">{n}</td>
                </tr>
              {/each}
            </tbody>
          </table>
          <p class="muted note">Bash calls are classified by what they do (cat → read, grep -r → search, heredoc write → edit).</p>
        </section>
        <section class="card">
          <h2>Tools</h2>
          <table>
            <thead><tr><th>Tool</th><th class="num">Calls</th><th class="num">Failed</th><th class="num">Time</th><th class="num" title="Estimated tokens added to context by results (bytes / 4)">~Result tokens</th></tr></thead>
            <tbody>
              {#each r.Tools.ByName ?? [] as t}
                <tr>
                  <td class="mono">{t.Name}</td>
                  <td class="num">{t.Calls}</td>
                  <td class="num" class:err={t.Failed > 0}>{t.Failed}</td>
                  <td class="num">{dur(t.Duration)}</td>
                  <td class="num">{tok(t.ResultTokens)}</td>
                </tr>
              {/each}
            </tbody>
          </table>
        </section>
        <section class="card span2">
          <h2>Identical calls repeated</h2>
          {#if (r.Tools.Repeated ?? []).length === 0}
            <p class="muted">None.</p>
          {:else}
            <table>
              <thead><tr><th class="num">×</th><th>Call</th><th>Evidence</th></tr></thead>
              <tbody>
                {#each r.Tools.Repeated ?? [] as rep}
                  <tr><td class="num">{rep.Count}</td><td class="mono">{short(rep.Key, 140)}</td><td><Refs seqs={rep.Seqs} onShow={show} /></td></tr>
                {/each}
              </tbody>
            </table>
          {/if}
          {#if (r.Tools.LongRunning ?? []).length}
            <p>Long-running (≥ 30s): <Refs refs={r.Tools.LongRunning} onShow={show} /></p>
          {/if}
        </section>
      </div>
    {:else}
      <div class="pad">
        <div class="tiles">
          {#each keyTiles as [label, value, sub]}
            <div class="tile card">
              <div class="muted">{label}</div>
              <div class="value">{value}</div>
              <div class="secondary">{sub}</div>
            </div>
          {/each}
        </div>

        <Findings findings={view.findings} onShow={show} limit={3} />

        <details class="more fold">
          <summary>Details <span class="muted">· all metrics, exploration, tokens by turn, tests, MCP, failures</span></summary>
          <div class="tiles">
            {#each moreTiles as [label, value, sub]}
              <div class="tile card">
                <div class="muted">{label}</div>
                <div class="value">{value}</div>
                <div class="secondary">{sub}</div>
              </div>
            {/each}
          </div>

        <div class="grid2">
          <section class="card">
            <h2>Exploration</h2>
            <p class="secondary">
              {r.Files.ReadBeforeFirstEdit} file(s) read before the first edit · {r.Files.Reads} reads of {r.Files.UniqueRead} files
              across {(r.Files.Directories ?? []).length} directories
            </p>
            <h3>Re-read without an edit in between</h3>
            {#if (r.Files.RedundantReads ?? []).length === 0}
              <p class="muted">None.</p>
            {:else}
              <table>
                <tbody>
                  {#each r.Files.RedundantReads ?? [] as rep}
                    <tr><td class="num w40">×{rep.Count}</td><td class="mono">{p(rep.Key)}</td><td><Refs seqs={rep.Seqs} onShow={show} /></td></tr>
                  {/each}
                </tbody>
              </table>
            {/if}
            <h3>Broad searches <span class="muted">(repo-wide pattern or no path)</span></h3>
            {#if (r.Files.BroadSearches ?? []).length}
              <Refs refs={r.Files.BroadSearches} onShow={show} />
            {:else}<p class="muted">None.</p>{/if}
            <h3>Edited</h3>
            {#each r.Files.Edited ?? [] as f}<div class="mono">{p(f)}</div>{:else}<p class="muted">No project files edited.</p>{/each}
          </section>

          <section class="card">
            <h2>Tokens by turn</h2>
            <table>
              <thead><tr><th class="num">#</th><th>Prompt</th><th class="num">Calls</th><th>Processed</th></tr></thead>
              <tbody>
                {#each r.Tokens.ByTurn ?? [] as t}
                  <tr>
                    <td class="num">{t.Turn}</td>
                    <td title={t.Prompt}>{short(t.Prompt, 70)}</td>
                    <td class="num">{t.ModelCalls}</td>
                    <td class="w160"><span class="track"><span class="bar" style="width:{(t.Processed / maxTurn) * 100}%"></span></span><span class="num">{tok(t.Processed)}</span></td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </section>

          <section class="card">
            <h2>Tests</h2>
            <p class="secondary">
              {r.Tests.Runs} run(s), {r.Tests.Failed} failed, {r.Tests.Repeated} repeated · {r.Tests.Style}
              {#if r.Tests.FirstEditToFirstTest > 0} · first edit → first test {dur(r.Tests.FirstEditToFirstTest)}{/if}
            </p>
            {#if (r.Tests.RunRefs ?? []).length}<Refs refs={r.Tests.RunRefs} onShow={show} />{/if}

            <h2 class="gap">MCP</h2>
            {#if r.MCP.Calls === 0}
              <p class="muted">No MCP calls.</p>
            {:else}
              <table>
                <thead><tr><th>Tool</th><th class="num">Calls</th><th class="num">Time</th></tr></thead>
                <tbody>
                  {#each r.MCP.ByTool ?? [] as t}
                    <tr><td class="mono">{t.Name}</td><td class="num">{t.Calls}</td><td class="num">{dur(t.Duration)}</td></tr>
                  {/each}
                </tbody>
              </table>
              <p class="secondary">{r.MCP.BeforeFirstEdit} of {r.MCP.Calls} happened before the first edit.
                {#each r.MCP.Repeated ?? [] as rep}Identical call ×{rep.Count}: <Refs seqs={rep.Seqs} onShow={show} />{/each}</p>
              <p class="muted note">Relevance: unknown. Metrics alone can't show whether a call was needed; that is judged later from the reconstructed strategy.</p>
            {/if}
          </section>

          <section class="card">
            <h2>Failures</h2>
            {#each r.Failures ?? [] as f}
              <div class="fail"><Refs refs={[f]} onShow={show} /> <span class="mono">{short(f.Label, 120)}</span></div>
            {:else}<p class="muted">No failed tool calls.</p>{/each}

            {#if (r.Subagents ?? []).length + (r.Skills ?? []).length}
              <h2 class="gap">Delegation</h2>
              {#each [...(r.Subagents ?? []), ...(r.Skills ?? [])] as x}
                <div><Refs refs={[x]} onShow={show} /> {short(x.Label, 120)}</div>
              {/each}
            {/if}
            {#if (view.warnings ?? []).length}
              <h2 class="gap">Parse warnings</h2>
              {#each view.warnings as w}<div class="mono muted">{w}</div>{/each}
            {/if}
          </section>
        </div>
        </details>
      </div>
    {/if}
  </div>
{/if}

<style>
  .wrap { display: flex; flex-direction: column; height: 100%; }
  .pad { padding: 16px; }
  .top { padding: 14px 16px 0; border-bottom: 1px solid var(--border); background: var(--surface-1); }
  h1 { font-size: 16px; margin: 0 0 2px; font-weight: 600; }
  nav { display: flex; gap: 2px; margin-top: 10px; }
  nav button { border: none; border-bottom: 2px solid transparent; border-radius: 0; background: none; padding: 6px 12px; color: var(--text-secondary); }
  nav button.active { border-bottom-color: var(--accent); color: var(--text-primary); }
  .tiles { display: grid; grid-template-columns: repeat(auto-fit, minmax(160px, 1fr)); gap: 8px; margin-bottom: 12px; }
  .more > summary { padding: 4px 0 12px; font-weight: 600; }
  .tile { padding: 9px 12px; }
  .tile .muted { font-size: 11px; }
  .tile .value { font-size: 18px; font-weight: 600; font-variant-numeric: tabular-nums; line-height: 1.3; }
  .tile .secondary { font-size: 11px; color: var(--text-muted); }
  .grid2 { display: grid; grid-template-columns: repeat(auto-fit, minmax(480px, 1fr)); gap: 16px; }
  .span2 { grid-column: 1 / -1; }
  h3 { font-size: 12px; font-weight: 600; color: var(--text-secondary); margin: 12px 0 4px; }
  .gap { margin-top: 16px; }
  .bar { height: 8px; background: var(--seq); border-radius: 0 4px 4px 0; display: inline-block; vertical-align: middle; min-width: 2px; }
  .track { display: inline-block; width: 110px; margin-right: 6px; vertical-align: middle; line-height: 0; }
  .w40 { width: 40px; } .w60 { width: 60px; } .w120 { width: 120px; } .w160 { width: 180px; white-space: nowrap; }
  .note { font-size: 12px; }
  .fail { margin-bottom: 4px; }
</style>
