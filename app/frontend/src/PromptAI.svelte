<script lang="ts">
  import { PromptAnalysis, AnalyzePrompts, LLMStatus } from '../wailsjs/go/main/App'
  import type { llm, main } from '../wailsjs/go/models'
  import { date } from './lib/format'
  import Findings from './Findings.svelte'

  // scope: a repo path, or '' for all repos.
  let { scope, prompts, onOpen, onApplied = () => {} }: { scope: string; prompts: number; onOpen: (id: string, seq: number) => void; onApplied?: () => void } = $props()

  let result = $state<llm.Result | null>(null)
  let status = $state<main.LLMStatus | null>(null)
  let busy = $state(false)
  let error = $state('')

  // Only the stored result is loaded on open; the API is called from the button alone.
  $effect(() => {
    PromptAnalysis(scope).then(r => (result = r)).catch(e => (error = String(e)))
    LLMStatus().then(s => (status = s))
  })

  async function run(force: boolean) {
    busy = true
    error = ''
    try {
      result = await AnalyzePrompts(scope, force)
    } catch (e) {
      error = String(e)
    } finally {
      busy = false
      status = await LLMStatus()
    }
  }
</script>

{#if result}
  <Findings
    findings={result.findings}
    onOpenSession={onOpen}
    limit={3}
    {scope}
    {onApplied}
    note={`AI analysis of ${result.analyzed} of ${result.total} prompts · ${result.model} · ${date(result.createdAt)}`}
    empty="Claude found nothing worth setting up in these prompts."
  />
  <div class="rerun">
    <button disabled={busy} onclick={() => run(true)}>{busy ? 'Analyzing…' : 'Re-run AI analysis'}</button>
    {#if error}<span class="err">{error}</span>{/if}
  </div>
{:else}
  <section class="card ai">
    <div class="head">
      <h2>AI analysis of prompts</h2>
      <span class="muted">Claude reads your prompts and suggests what to set up once: CLAUDE.md rules, skills, slash commands, subagents, scripts.</span>
    </div>
    {#if status && !status.hasKey}
      <p class="secondary">
        Needs an Anthropic API key. Save it to <code>{status.keyPath}</code> (or set <code>ANTHROPIC_API_KEY</code> before starting the app):
      </p>
      <pre>printf %s 'YOUR_KEY' &gt; {status.keyPath} && chmod 600 {status.keyPath}</pre>
    {:else}
      <div class="go">
        <button class="primary" disabled={busy || prompts === 0} onclick={() => run(false)}>
          {busy ? 'Analyzing… (up to a minute)' : `Analyze ${prompts} prompts with Claude`}
        </button>
        <span class="muted">Sends the text of these prompts and session sizes to the Anthropic API. Everything else stays local.</span>
      </div>
    {/if}
    {#if error}<p class="err">{error}</p>{/if}
  </section>
{/if}

<style>
  .head { display: flex; align-items: baseline; gap: 12px; flex-wrap: wrap; margin-bottom: 8px; }
  .head h2 { margin: 0; }
  .go { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; }
  pre { margin: 6px 0 0; padding: 8px 10px; background: var(--surface-2); border-radius: 6px; font: 12px ui-monospace, Menlo, monospace; white-space: pre-wrap; }
  code { font: 12px ui-monospace, Menlo, monospace; }
  .rerun { display: flex; align-items: center; gap: 12px; margin: -4px 0 4px; font-size: 12px; }
  .rerun button { font-size: 12px; padding: 2px 8px; }
</style>
