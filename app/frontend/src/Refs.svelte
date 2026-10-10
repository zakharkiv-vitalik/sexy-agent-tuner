<script lang="ts">
  import type { analysis } from '../wailsjs/go/models'
  import { dur } from './lib/format'

  // Evidence chips: each opens the referenced event in the timeline.
  let { refs = [], seqs = [], onShow }: { refs?: analysis.Ref[] | null; seqs?: number[] | null; onShow: (seq: number) => void } = $props()
</script>

{#each refs ?? [] as r}
  <button class="ref" title={`${r.Label ?? ''} (+${dur(r.Offset)})`} onclick={() => onShow(r.Seq)}>#{r.Seq}</button>
{/each}
{#each seqs ?? [] as s}
  <button class="ref" onclick={() => onShow(s)}>#{s}</button>
{/each}
