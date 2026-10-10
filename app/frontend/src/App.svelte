<script lang="ts">
  import Repos from './Repos.svelte'
  import Repo from './Repo.svelte'
  import SessionView from './SessionView.svelte'
  import Global from './Global.svelte'

  let page = $state<'repos' | 'global'>('repos')
  let repo = $state<string | null>(null)
  let openId = $state<string | null>(null)
  let openSeq = $state<number | null>(null)

  function open(id: string, seq: number | null = null) {
    openId = id
    openSeq = seq
  }
  function go(p: typeof page, r: string | null = null) {
    page = p
    repo = r
    openId = null
  }
</script>

<div class="shell">
  <header>
    <button class="brand" onclick={() => go('repos')}>Sexy Agent Tuner</button>
    {#each [['repos', 'Repos'], ['global', 'All repos']] as [k, label]}
      <button class="nav" class:active={!openId && !repo && page === k} onclick={() => go(k as typeof page)}>{label}</button>
    {/each}
    {#if repo}
      <span class="muted">/</span>
      <button class="crumb" class:current={!openId} onclick={() => (openId = null)}>{repo.split('/').pop()}</button>
    {/if}
    {#if openId}
      <span class="muted">/</span>
      <span class="mono secondary">{openId.slice(0, 8)}</span>
    {/if}
    <span class="local muted" title="Sessions are parsed and stored only on this machine (~/.awo/awo.db). Nothing is sent to an LLM.">
      <span class="ok">●</span> local only
    </span>
  </header>
  <main>
    {#if openId}
      {#key `${openId}#${openSeq}`}
        <SessionView id={openId} seq={openSeq} />
      {/key}
    {:else if repo}
      {#key repo}
        <Repo path={repo} onOpen={(id, seq) => open(id, seq ?? null)} />
      {/key}
    {:else if page === 'global'}
      <Global onOpen={open} onRepo={(r) => go('repos', r)} />
    {:else}
      <Repos onOpen={(id) => open(id)} onRepo={(r) => go('repos', r)} />
    {/if}
  </main>
</div>

<style>
  .shell { display: flex; flex-direction: column; height: 100%; }
  header {
    display: flex; align-items: center; gap: 8px; padding: 8px 16px;
    border-bottom: 1px solid var(--border); background: var(--surface-0);
  }
  .brand { border: none; background: none; font-weight: 600; padding: 0; margin-right: 8px; color: var(--accent); }
  .brand:hover { background: none; }
  .nav { border: none; background: none; color: var(--text-secondary); padding: 2px 8px; }
  .nav:hover { color: var(--text-primary); }
  .crumb { border: none; background: none; padding: 2px 4px; color: var(--text-secondary); }
  .crumb.current { color: var(--text-primary); }
  .nav.active { color: var(--text-primary); background: var(--surface-2); }
  .local { margin-left: auto; font-size: 12px; }
  .ok { color: var(--accent); }
  main { flex: 1; min-height: 0; overflow: auto; }
</style>
