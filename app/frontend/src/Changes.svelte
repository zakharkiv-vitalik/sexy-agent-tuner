<script lang="ts">
  import { Experiments } from '../wailsjs/go/main/App'
  import type { apply } from '../wailsjs/go/models'
  import { date, tok } from './lib/format'

  // scope: repo path, or '' for user-level changes. version: bump to reload after an Apply.
  let { scope, version = 0 }: { scope: string; version?: number } = $props()

  let list = $state<apply.Experiment[]>([])
  $effect(() => {
    version // reload when it changes
    Experiments(scope).then(r => (list = r ?? []))
  })

  const pct = (v: number) => (v < 0 ? '—' : `${Math.round(v * 100)}%`)
  // Every metric here is better when lower.
  const ROWS: { label: string; get: (s: apply.Stats) => number; fmt: (v: number) => string }[] = [
    { label: 'Same finding again', get: s => s.ruleRate, fmt: pct },
    { label: 'Tokens per prompt', get: s => s.tokensPerPrompt, fmt: v => tok(v) },
    { label: 'Exploration before first edit', get: s => s.exploreShare, fmt: pct },
    { label: 'Failed tool calls', get: s => s.failRate, fmt: pct },
  ]
  function trend(before: number, after: number) {
    if (before < 0 || after < 0 || before === after) return ''
    return after < before ? 'better' : 'worse'
  }
</script>

{#if list.length}
  <section class="card">
    <div class="head">
      <h2>Applied changes</h2>
      <span class="muted">Sessions after each change compared with the ones before it (medians, up to 20 on each side). A trend, not proof.</span>
    </div>
    {#each list as e (e.appliedAt + e.path)}
      <div class="change">
        <div class="title"><b>{e.title}</b> <span class="muted">· {date(e.appliedAt)} · <code>{e.path}</code></span></div>
        {#if e.tooEarly}
          <p class="muted">{e.after.sessions} session{e.after.sessions === 1 ? '' : 's'} since the change; the comparison starts after 3.</p>
        {:else}
          <table>
            <thead><tr><th></th><th class="num">Before ({e.before.sessions})</th><th class="num">After ({e.after.sessions})</th></tr></thead>
            <tbody>
              {#each ROWS as r}
                {@const b = r.get(e.before)}
                {@const a = r.get(e.after)}
                {#if b >= 0 || a >= 0}
                  <tr>
                    <td>{r.label}</td>
                    <td class="num">{r.fmt(b)}</td>
                    <td class="num {trend(b, a)}">{r.fmt(a)}</td>
                  </tr>
                {/if}
              {/each}
            </tbody>
          </table>
        {/if}
      </div>
    {/each}
  </section>
{/if}

<style>
  .head { display: flex; align-items: baseline; gap: 12px; flex-wrap: wrap; margin-bottom: 6px; }
  .head h2 { margin: 0; }
  .change { border-top: 1px solid var(--grid); padding: 8px 0; }
  .title { margin-bottom: 4px; }
  code { font: 11px ui-monospace, Menlo, monospace; }
  table { max-width: 560px; }
  .better { color: var(--ok); }
  .worse { color: var(--critical); }
</style>
