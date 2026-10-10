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
    <button class="brand" onclick={() => go('repos')}><img src="/logo.svg" alt="" width="22" height="22" />Sexy Agent Tuner</button>
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
    border-bottom: 1px solid var(--border); background: rgb(7 6 11 / .7); backdrop-filter: blur(10px); -webkit-backdrop-filter: blur(10px);
  }
  .brand { display: flex; align-items: center; gap: 7px; border: none; background: none; color: #ff7fbf; font-weight: 500; padding: 0; margin-right: 8px; }
  .brand img { margin: -4px -2px -4px -4px; } /* the icon's own transparent margin */
  .brand:hover { background: none; }
  .nav { border: none; background: none; color: var(--text-secondary); padding: 2px 8px; }
  .nav:hover { color: var(--text-primary); }
  .crumb { border: none; background: none; padding: 2px 4px; color: var(--text-secondary); }
  .crumb.current { color: var(--text-primary); }
  /* translucent pink with soft, blurred edges */
  .nav.active { color: var(--text-primary); background: rgb(255 62 165 / .13); box-shadow: 0 0 12px 3px rgb(255 62 165 / .13); backdrop-filter: blur(6px); }
  .local { margin-left: auto; font-size: 12px; }
  .ok { color: var(--ok); text-shadow: var(--glow-green); }
  main { flex: 1; min-height: 0; overflow: auto; }
</style>
