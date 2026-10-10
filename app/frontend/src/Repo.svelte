<script lang="ts">
  import { Repo } from '../wailsjs/go/main/App'
  import type { main } from '../wailsjs/go/models'
  import { dur, date, short, tok } from './lib/format'
  import Findings from './Findings.svelte'
  import PromptAI from './PromptAI.svelte'

  let { path, onOpen }: { path: string; onOpen: (id: string, seq?: number) => void } = $props()

  let view = $state<main.RepoView | null>(null)
  let error = $state('')
  let query = $state('')
  let order = $state<'newest' | 'largest'>('newest')

  $effect(() => {
    Repo(path).then(v => (view = v)).catch(e => (error = String(e)))
  })

  // Prompts grouped by session, sessions newest first (the backend already sorts prompts newest first).
  const groups = $derived.by(() => {
    const q = query.toLowerCase()
    const bySession = new Map((view?.sessions ?? []).map(s => [s.id, s]))
    const out: { s: main.SessionRow | undefined; id: string; prompts: main.PromptRow[] }[] = []
    for (const p of view?.prompts ?? []) {
      if (q && !p.text.toLowerCase().includes(q)) continue
      let g = out.find(g => g.id === p.sessionId)
      if (!g) out.push((g = { s: bySession.get(p.sessionId), id: p.sessionId, prompts: [] }))
      g.prompts.unshift(p) // chronological inside a session
    }
    if (order === 'largest') out.sort((a, b) => (b.s?.tokens ?? 0) - (a.s?.tokens ?? 0))
    return out
  })
  const maxTokens = $derived(Math.max(1, ...(view?.sessions ?? []).map(s => s.tokens)))
  // "Big" is relative to this repo: at least half of its largest session.
  const big = (s?: main.SessionRow) => !!s && s.tokens >= maxTokens / 2
  const time = (iso: string) => new Date(iso).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
</script>

<div class="page">
  {#if error}
    <div class="card err">{error}</div>
  {:else if !view}
    <p class="muted">Loading repo…</p>
  {:else}
    <div class="top">
      <h1>{view.name}</h1>
      <span class="muted">{view.path} · {(view.sessions ?? []).length} sessions · {(view.prompts ?? []).length} prompts</span>
    </div>

    <Findings
      findings={view.findings}
      onOpenSession={onOpen}
      limit={3}
      note="Patterns that recur across this repo's sessions."
      empty="Nothing recurs in 3 or more sessions of this repo yet."
    />

    <PromptAI scope={view.path} prompts={(view.prompts ?? []).length} onOpen={(id, seq) => onOpen(id, seq)} />

    <section class="card">
      <div class="head">
        <h2>Prompts</h2>
        <div class="seg">
          <button class:on={order === 'newest'} onclick={() => (order = 'newest')}>Newest</button>
          <button class:on={order === 'largest'} onclick={() => (order = 'largest')}>Largest</button>
        </div>
        <input type="search" placeholder="Search prompts" bind:value={query} />
      </div>
      {#each groups as g (g.id)}
        <div class="session">
          <button class="shead" onclick={() => onOpen(g.id)} title="Open session">
            <span>{g.s ? date(g.s.startedAt) : g.id.slice(0, 8)}</span>
            {#if g.s}<span class="muted">{dur(g.s.durationNs)}</span>{/if}
            <span class="muted">{g.prompts.length} prompt{g.prompts.length === 1 ? '' : 's'}</span>
            {#if g.s}
              <span class="size" title="Processed tokens; peak context = largest prompt the model saw">
                <span class="track"><span class="fill" class:big={big(g.s)} style="width:{(g.s.tokens / maxTokens) * 100}%"></span></span>
                <span class="mono" class:bigtext={big(g.s)}>{tok(g.s.tokens)}</span>
                <span class="muted">peak {tok(g.s.peak)}</span>
              </span>
            {/if}
            <span class="mono muted id">{g.id.slice(0, 8)}</span>
          </button>
          {#each g.prompts as p (p.seq)}
            <button class="prompt" title={p.text} onclick={() => onOpen(p.sessionId, p.seq)}>
              <span class="mono muted">{time(p.at)}</span>
              <span class="text">{short(p.text, 240)}</span>
            </button>
          {/each}
        </div>
      {:else}
        <p class="muted">{query ? 'No prompts match.' : 'No prompts in this repo.'}</p>
      {/each}
    </section>
  {/if}
</div>

<style>
  .page { padding: 16px; display: flex; flex-direction: column; gap: 12px; max-width: 1400px; }
  .top h1 { font-size: 16px; font-weight: 600; margin: 0 0 2px; }
  .head { display: flex; align-items: center; gap: 12px; margin-bottom: 4px; }
  .head h2 { margin: 0; flex: 1; }
  .head input { width: 280px; }
  .session { padding: 6px 0; border-top: 1px solid var(--grid); }
  .session:first-of-type { border-top: none; }
  button.shead, button.prompt { display: flex; gap: 10px; width: 100%; text-align: left; border: none; border-radius: 6px; background: none; }
  .shead { padding: 3px 6px; font-size: 12px; font-weight: 600; }
  .shead .id { margin-left: 12px; }
  .size { margin-left: auto; display: flex; align-items: center; gap: 8px; font-weight: 400; }
  .track { width: 120px; height: 5px; border-radius: 3px; background: var(--surface-2); overflow: hidden; }
  .fill { display: block; height: 100%; background: var(--blue); border-radius: 3px; box-shadow: var(--glow-blue); }
  .fill.big { background: var(--pink); box-shadow: var(--glow-pink); }
  .bigtext { color: var(--pink); text-shadow: var(--glow-pink); }
  .seg { display: flex; }
  .seg button { padding: 2px 8px; font-size: 12px; border-radius: 0; }
  .seg button:first-child { border-radius: 6px 0 0 6px; }
  .seg button:last-child { border-radius: 0 6px 6px 0; border-left: none; }
  .seg button.on { background: var(--surface-2); color: var(--text-primary); }
  .prompt { padding: 3px 6px 3px 18px; align-items: baseline; }
  .prompt .text { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .prompt:hover .text { color: var(--accent); }
</style>
