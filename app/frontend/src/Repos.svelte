<script lang="ts">
  import { Repos, Discover, Import, ImportDialog } from '../wailsjs/go/main/App'
  import type { main } from '../wailsjs/go/models'
  import { date, short, bytes, tok } from './lib/format'

  let { onOpen, onRepo }: { onOpen: (id: string) => void; onRepo: (path: string) => void } = $props()

  let repos = $state<main.RepoRow[]>([])
  let found = $state<main.Candidate[] | null>(null)
  let filter = $state('')
  let busy = $state<string | null>(null) // path or 'dialog' being imported
  let error = $state('')

  const visible = $derived(
    (found ?? []).filter(c => {
      const q = filter.toLowerCase()
      return !q || c.firstPrompt.toLowerCase().includes(q) || c.project.toLowerCase().includes(q)
    }),
  )

  const pending = $derived((found ?? []).filter(c => !c.imported))

  async function refresh() {
    repos = (await Repos()) ?? []
  }

  async function run(key: string, f: () => Promise<string[]>) {
    busy = key
    error = ''
    try {
      const ids = (await f()) ?? []
      await refresh()
      if (found) found = found.map(c => (ids.includes(c.id) ? { ...c, imported: true } : c)) as main.Candidate[]
      if (ids.length === 1) onOpen(ids[0])
    } catch (e) {
      error = String(e)
    } finally {
      busy = null
    }
  }

  refresh()
  Discover().then(r => (found = r ?? [])).catch(e => (error = String(e)))
</script>

<div class="page">
  {#if error}<div class="card err">{error}</div>{/if}

  <section class="card">
    <div class="head">
      <h2>Repos</h2>
      <button onclick={() => run('dialog', ImportDialog)} disabled={busy !== null}>
        {busy === 'dialog' ? 'Importing…' : 'Import file…'}
      </button>
    </div>
    {#if repos.length === 0}
      <p class="muted">Nothing imported yet. Import sessions below or pick a .jsonl file.</p>
    {:else}
      <table>
        <thead><tr><th>Repo</th><th>Last session</th><th class="num">Sessions</th><th class="num">Tokens</th></tr></thead>
        <tbody>
          {#each repos as r (r.path)}
            <tr class="row" onclick={() => onRepo(r.path)}>
              <td title={r.path}><b>{r.name || r.path}</b> <span class="muted path">{r.path}</span></td>
              <td class="nowrap">{date(r.lastAt)}</td>
              <td class="num">{r.sessions}</td>
              <td class="num">{tok(r.tokens)}</td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
  </section>

  <details class="card fold" open={repos.length === 0}>
    <summary>
      <h2>Import more</h2>
      <span class="muted">{found === null ? 'scanning ~/.claude/projects…' : `${pending.length} of ${found.length} sessions on this machine not imported`}</span>
    </summary>
    <div class="head">
      <span></span>
      {#if pending.length > 0}
        <button class="primary" disabled={busy !== null} onclick={() => run('all', () => Import(pending.map(c => c.path)))}>
          {busy === 'all' ? 'Importing…' : `Import all ${pending.length}`}
        </button>
      {/if}
      <input type="search" placeholder="Filter by prompt or project" bind:value={filter} />
    </div>
    {#if found === null}
      <p class="muted">Scanning ~/.claude/projects…</p>
    {:else if visible.length === 0}
      <p class="muted">No sessions found.</p>
    {:else}
      <table>
        <thead><tr><th>Modified</th><th>Project</th><th>First prompt</th><th class="num">Size</th><th></th></tr></thead>
        <tbody>
          {#each visible as c (c.path)}
            <tr>
              <td class="nowrap">{date(c.modifiedAt)}</td>
              <td class="nowrap">{short(c.project, 40)}</td>
              <td title={c.firstPrompt}>{short(c.firstPrompt, 110)}</td>
              <td class="num nowrap">{bytes(c.sizeBytes)}</td>
              <td class="nowrap">
                {#if c.imported}
                  <button onclick={() => onOpen(c.id)}>Open</button>
                {/if}
                <button class:primary={!c.imported} disabled={busy !== null} onclick={() => run(c.path, () => Import([c.path]))}>
                  {busy === c.path ? 'Importing…' : c.imported ? 'Re-import' : 'Import'}
                </button>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
  </details>
</div>

<style>
  .page { padding: 16px; display: flex; flex-direction: column; gap: 16px; max-width: 1400px; }
  .head { display: flex; align-items: center; gap: 12px; margin-bottom: 8px; }
  .head h2 { margin: 0; flex: 1; }
  .head > span { flex: 1; }
  summary h2 { margin: 0; }
  details[open] > summary { margin-bottom: 8px; }
  .head input { width: 280px; }
  .row { cursor: pointer; }
  .row:hover td { background: var(--surface-2); }
  .nowrap { white-space: nowrap; }
  .path { font-size: 11px; margin-left: 6px; }
</style>
