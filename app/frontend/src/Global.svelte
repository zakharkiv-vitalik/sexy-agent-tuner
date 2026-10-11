<script lang="ts">
  import { Insights } from '../wailsjs/go/main/App'
  import type { main } from '../wailsjs/go/models'
  import Findings from './Findings.svelte'
  import PromptAI from './PromptAI.svelte'
  import Changes from './Changes.svelte'

  let { onOpen, onRepo }: { onOpen: (id: string, seq: number) => void; onRepo: (path: string) => void } = $props()

  let view = $state<main.GlobalView | null>(null)
  let error = $state('')
  let applied = $state(0)

  Insights().then(v => (view = v)).catch(e => (error = String(e)))
</script>

<div class="page">
  {#if error}
    <div class="card err">{error}</div>
  {:else if !view}
    <p class="muted">Analyzing all repos…</p>
  {:else}
    <Findings
      findings={view.findings}
      onOpenSession={onOpen}
      limit={3}
      scope=""
      onApplied={() => applied++}
      note="Patterns that span several repos: candidates for ~/.claude/CLAUDE.md, a personal skill, a subagent or a script you reuse everywhere."
      empty="Nothing recurs across repos yet. Import more sessions to widen the sample."
    />

    <PromptAI scope="" prompts={view.prompts} onOpen={onOpen} onApplied={() => applied++} />

    <Changes scope="" version={applied} />

    <section class="card">
      <h2>Per repo</h2>
      {#each view.repos ?? [] as r (r.path)}
        <button class="repo" title={r.path} onclick={() => onRepo(r.path)}>
          <span>{r.name}</span>
          <span class="muted">{r.count} suggestion{r.count === 1 ? '' : 's'}</span>
        </button>
      {:else}
        <p class="muted">No repo has recurring patterns yet.</p>
      {/each}
    </section>
  {/if}
</div>

<style>
  .page { padding: 16px; display: flex; flex-direction: column; gap: 12px; max-width: 1400px; }
  .repo { display: flex; justify-content: space-between; width: 100%; border: none; border-radius: 6px; background: none; padding: 4px 6px; }
  .repo:hover span:first-child { color: var(--accent); }
</style>
