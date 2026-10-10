<script lang="ts">
  import { Event } from '../wailsjs/go/main/App'
  import type { main, analysis } from '../wailsjs/go/models'
  import { dur, tok, short, bytes } from './lib/format'
  import { LANES, laneOf, laneColor, laneLabel, type Lane } from './lib/lanes'

  let { view, focus = $bindable(null) }: { view: main.SessionView; focus?: number | null } = $props()

  type Point = { e: analysis.TimelineEntry; lane: Lane; i: number }

  const LEFT = 96, RIGHT = 12, TOP = 4, LANE_H = 16, GAP = 14, CTX_H = 48, AXIS = 18, MIN_STEP = 4

  const entries = $derived(view.report?.Timeline ?? [])
  const points = $derived.by(() => {
    const out: Point[] = []
    for (const e of entries) {
      const lane = laneOf(e)
      if (lane) out.push({ e, lane, i: out.length })
    }
    return out
  })
  const bySeq = $derived(new Map(points.map(p => [p.e.Seq, p])))
  const counts = $derived(Object.fromEntries(LANES.map(l => [l.id, points.filter(p => p.lane === l.id).length])))
  // Empty lanes (no MCP, no subagents…) take no space.
  const lanes = $derived(LANES.filter(l => counts[l.id] > 0))
  const hasSub = $derived(points.some(p => p.e.Agent))
  const hasErr = $derived(points.some(p => p.e.IsError))

  let mode = $state<'step' | 'time'>('step')
  let width = $state(1000)
  let hover = $state<{ p: Point; x: number; y: number } | null>(null)
  let scroller: HTMLDivElement | undefined = $state()

  const total = $derived(Math.max(1, ...points.map(p => p.e.Offset)))
  const stepW = $derived(Math.max((width - LEFT - RIGHT) / Math.max(points.length, 1), MIN_STEP))
  const svgW = $derived(mode === 'step' ? LEFT + points.length * stepW + RIGHT : width)
  const plotW = $derived(svgW - LEFT - RIGHT)
  const lanesH = $derived(lanes.length * LANE_H)
  const ctxTop = $derived(TOP + lanesH + GAP)
  const svgH = $derived(ctxTop + CTX_H + AXIS)
  const maxCtx = $derived(Math.max(1, ...points.map(p => p.e.Context)))

  const xOf = (p: Point) => (mode === 'step' ? LEFT + (p.i + 0.5) * stepW : LEFT + (p.e.Offset / total) * plotW)
  const laneY = (l: Lane) => TOP + lanes.findIndex(x => x.id === l) * LANE_H + LANE_H / 2
  const ctxY = (c: number) => ctxTop + CTX_H - (c / maxCtx) * CTX_H

  const ctxPath = $derived(points.filter(p => p.e.Context > 0).map((p, k) => `${k ? 'L' : 'M'}${xOf(p).toFixed(1)},${ctxY(p.e.Context).toFixed(1)}`).join(''))

  const ticks = $derived.by(() => {
    if (!points.length) return []
    return [0, 1, 2, 3, 4].map(k => {
      if (mode === 'time') return { x: LEFT + (k / 4) * plotW, label: '+' + dur((total * k) / 4) }
      const p = points[Math.round((k / 4) * (points.length - 1))]
      return { x: xOf(p), label: `#${p.e.Seq} · +${dur(p.e.Offset)}` }
    })
  })

  // Hit-testing picks the nearest point by x, so the target is far larger than the 9px mark.
  function nearest(clientX: number, svg: SVGSVGElement): Point | null {
    const x = clientX - svg.getBoundingClientRect().left
    let best: Point | null = null, bestD = Infinity
    for (const p of points) {
      const d = Math.abs(xOf(p) - x)
      if (d < bestD) { best = p; bestD = d }
    }
    return bestD < 40 ? best : null
  }
  function onMove(ev: MouseEvent) {
    const p = nearest(ev.clientX, ev.currentTarget as SVGSVGElement)
    hover = p ? { p, x: xOf(p), y: laneY(p.lane) } : null
  }
  function onClick(ev: MouseEvent) {
    const p = nearest(ev.clientX, ev.currentTarget as SVGSVGElement)
    if (p) focus = p.e.Seq
  }

  // --- event list ---
  let hidden = $state<Record<string, boolean>>({})
  let showText = $state(false)
  let errorsOnly = $state(false)
  let query = $state('')
  const rows = $derived(
    entries.filter(e => {
      const lane = laneOf(e)
      if (lane ? hidden[lane] : !showText) return false
      if (errorsOnly && !e.IsError) return false
      return !query || e.Label.toLowerCase().includes(query.toLowerCase())
    }),
  )

  function move(delta: number) {
    const i = rows.findIndex(r => r.Seq === focus)
    const next = rows[Math.min(rows.length - 1, Math.max(0, i + delta))]
    if (next) focus = next.Seq
  }

  // --- detail ---
  let detail = $state<main.EventDetail | null>(null)
  let detailErr = $state('')
  $effect(() => {
    const seq = focus
    if (seq == null) return
    let live = true
    detailErr = ''
    Event(view.id, seq).then(d => { if (live) detail = d }).catch(e => { if (live) detailErr = String(e) })
    document.getElementById(`ev-${seq}`)?.scrollIntoView({ block: 'nearest' })
    const p = bySeq.get(seq)
    if (p && scroller) {
      const x = xOf(p)
      if (x < scroller.scrollLeft || x > scroller.scrollLeft + scroller.clientWidth) scroller.scrollLeft = x - scroller.clientWidth / 2
    }
    return () => { live = false }
  })
  const focusPoint = $derived(focus == null ? undefined : bySeq.get(focus))
</script>

<div class="tl">
  <div class="bar">
    <div class="seg">
      <button class:on={mode === 'step'} onclick={() => (mode = 'step')} title="One slot per action; long idle gaps collapse">By step</button>
      <button class:on={mode === 'time'} onclick={() => (mode = 'time')} title="Real elapsed time">By time</button>
    </div>
    <span class="legend muted">
      {#if hasSub}<svg width="12" height="12"><circle cx="6" cy="6" r="3.5" fill="none" stroke="var(--text-secondary)" stroke-width="1.5" /></svg> subagent{/if}
      {#if hasErr}<svg width="14" height="14"><circle cx="7" cy="7" r="5.5" fill="none" stroke="var(--critical)" stroke-width="1.5" /></svg> failed{/if}
    </span>
    <span class="muted right">{points.length} actions · {dur(view.report?.Duration ?? 0)}</span>
  </div>

  <div class="chart" bind:this={scroller} bind:clientWidth={width}>
    <!-- Mouse-only by design: the event list below is the keyboard path (↑/↓) to the same data. -->
    <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_noninteractive_element_interactions -->
    <svg width={svgW} height={svgH} role="img" aria-label="Session timeline by behavior lane and context size"
      onmousemove={onMove} onmouseleave={() => (hover = null)} onclick={onClick}>
      {#each lanes as l, k}
        <line x1={LEFT} x2={svgW - RIGHT} y1={TOP + k * LANE_H + LANE_H / 2} y2={TOP + k * LANE_H + LANE_H / 2} class="grid" />
      {/each}
      <line x1={LEFT} x2={svgW - RIGHT} y1={ctxTop + CTX_H} y2={ctxTop + CTX_H} class="axis" />
      <line x1={LEFT} x2={svgW - RIGHT} y1={ctxTop} y2={ctxTop} class="grid" />
      <line x1={LEFT} x2={svgW - RIGHT} y1={ctxTop + CTX_H / 2} y2={ctxTop + CTX_H / 2} class="grid" />

      {#if focusPoint}
        <line x1={xOf(focusPoint)} x2={xOf(focusPoint)} y1={TOP} y2={ctxTop + CTX_H} class="focus" />
      {/if}
      {#if hover}
        <line x1={hover.x} x2={hover.x} y1={TOP} y2={ctxTop + CTX_H} class="cross" />
      {/if}

      <path d={ctxPath} class="ctx" />
      {#if hover && hover.p.e.Context > 0}
        <circle cx={hover.x} cy={ctxY(hover.p.e.Context)} r="4" fill="var(--seq)" stroke="var(--surface-solid)" stroke-width="2" />
      {/if}

      {#each points as p (p.e.Seq)}
        {@const x = xOf(p)}
        {@const y = laneY(p.lane)}
        {#if p.e.IsError}
          <circle cx={x} cy={y} r="6" fill="none" stroke="var(--critical)" stroke-width="1.5" />
        {/if}
        {#if p.e.Agent}
          <circle cx={x} cy={y} r="3" fill="var(--surface-solid)" stroke={laneColor(p.lane)} stroke-width="1.5" />
        {:else}
          <circle cx={x} cy={y} r="3.5" fill={laneColor(p.lane)} stroke="var(--surface-solid)" stroke-width="1" />
        {/if}
      {/each}

      <!-- sticky-ish labels: drawn last so marks never cover them -->
      <rect x="0" y="0" width={LEFT - 6} height={svgH} class="labelbg" />
      {#each lanes as l, k}
        <text x={LEFT - 12} y={TOP + k * LANE_H + LANE_H / 2 + 4} class="lane">{l.label}</text>
      {/each}
      <text x={LEFT - 12} y={ctxTop + 10} class="lane">Context</text>
      <text x={LEFT - 12} y={ctxTop + 22} class="tick">{tok(maxCtx)}</text>
      <text x={LEFT - 12} y={ctxTop + CTX_H + 4} class="tick">0</text>
      {#each ticks as t}
        <text x={t.x} y={svgH - 6} class="tick mid">{t.label}</text>
      {/each}
    </svg>

    {#if hover}
      <div class="tip" style="left:{Math.min(hover.x + 14, svgW - 330)}px; top:{hover.y + 10}px">
        <div><span class="dot" style="background:{laneColor(hover.p.lane)}"></span><b>{laneLabel(hover.p.lane)}</b>
          <span class="muted">#{hover.p.e.Seq} · +{dur(hover.p.e.Offset)}{hover.p.e.Agent ? ' · subagent' : ''}</span></div>
        <div class="mono">{short(hover.p.e.Label, 160)}</div>
        <div class="muted">
          {#if hover.p.e.IsError}<span class="err">✗ failed</span> · {/if}
          {#if hover.p.e.Duration}took {dur(hover.p.e.Duration)} · {/if}context {tok(hover.p.e.Context)}
        </div>
      </div>
    {/if}
  </div>

  <div class="lower" class:split={focus != null}>
    <div class="list">
      <div class="filters">
        {#each lanes as l}
          <button class="chip" class:off={hidden[l.id]} aria-pressed={!hidden[l.id]} title="Show/hide {l.label}"
            onclick={() => (hidden[l.id] = !hidden[l.id])}>
            <span class="dot" style="background:{laneColor(l.id)}"></span>{l.label} <span class="muted">{counts[l.id]}</span>
          </button>
        {/each}
        <button class="chip" class:off={!showText} aria-pressed={showText} onclick={() => (showText = !showText)}>Agent text</button>
        {#if hasErr}
          <button class="chip" class:off={!errorsOnly} aria-pressed={errorsOnly} onclick={() => (errorsOnly = !errorsOnly)}><span class="err">✗</span> Failed only</button>
        {/if}
        <input type="search" placeholder="Search events" bind:value={query} />
      </div>
      <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
      <div class="rows" tabindex="0" role="listbox" aria-label="Events"
        onkeydown={(e) => { if (e.key === 'ArrowDown') { move(1); e.preventDefault() } if (e.key === 'ArrowUp') { move(-1); e.preventDefault() } }}>
        {#each rows as e (e.Seq)}
          {@const lane = laneOf(e)}
          <button id="ev-{e.Seq}" class="row" class:sel={e.Seq === focus} class:says={!lane} role="option" aria-selected={e.Seq === focus}
            onclick={() => (focus = e.Seq)}>
            <span class="mono muted seq">#{e.Seq}</span>
            <span class="mono muted off">+{dur(e.Offset)}</span>
            <span class="kind">
              {#if lane}<span class="dot" style="background:{laneColor(lane)}"></span>{laneLabel(lane)}{:else}<span class="muted">says</span>{/if}
            </span>
            <span class="lbl">{#if e.IsError}<span class="err">✗ </span>{/if}{#if e.Agent}<span class="muted">[sub] </span>{/if}{short(e.Label, 220)}</span>
            <span class="mono muted ctxc">{e.Context ? tok(e.Context) : ''}</span>
          </button>
        {:else}
          <p class="muted pad">No events match the filters.</p>
        {/each}
      </div>
    </div>

    {#if focus != null}
    <aside class="detail">
      {#if detailErr}
        <p class="err">{detailErr}</p>
      {:else if !detail}
        <p class="muted">Loading…</p>
      {:else}
        <div class="dhead">
          <button class="close" title="Close" onclick={() => (focus = null)}>✕</button>
          <b>#{detail.seq}</b> <span class="secondary">{detail.toolName || detail.kind}</span>
          {#if detail.agent}<span class="muted">subagent {detail.agent.slice(0, 8)}</span>{/if}
          {#if detail.isError}<span class="err">✗ failed</span>{/if}
        </div>
        <div class="muted">
          {new Date(detail.timestamp).toLocaleTimeString()}
          {#if detail.durationNs} · took {dur(detail.durationNs)}{/if}
          {#if detail.outputBytes} · result {bytes(detail.outputBytes)}{/if}
        </div>
        {#if detail.text}<h3>Text</h3><pre>{detail.text}</pre>{/if}
        {#if detail.input}<h3>Input</h3><pre>{detail.input}</pre>{/if}
        {#if detail.toolName}
          <h3>Output {#if detail.outputBytes > detail.output.length}<span class="muted">(first {bytes(detail.output.length)} stored)</span>{/if}</h3>
          <pre class:errout={detail.isError}>{detail.output || '(empty)'}</pre>
        {/if}
      {/if}
    </aside>
    {/if}
  </div>
</div>

<style>
  .tl { display: flex; flex-direction: column; flex: 1; min-height: 0; }
  .bar { display: flex; align-items: center; gap: 12px; padding: 6px 16px; font-size: 12px; }
  .bar button { padding: 2px 8px; }
  .seg { display: flex; }
  .seg button { border-radius: 0; }
  .seg button:first-child { border-radius: 6px 0 0 6px; }
  .seg button:last-child { border-radius: 0 6px 6px 0; border-left: none; }
  .seg button.on { background: var(--surface-2); font-weight: 600; }
  .legend { display: flex; align-items: center; gap: 4px; font-size: 12px; }
  .legend svg { margin-left: 8px; }
  .right { margin-left: auto; }

  .chart { position: relative; overflow-x: auto; overflow-y: hidden; margin: 0 16px; background: var(--surface-1); border: 1px solid var(--border); border-radius: 8px; flex: none; }
  svg { display: block; cursor: crosshair; }
  .grid { stroke: var(--grid); stroke-width: 1; }
  .axis { stroke: var(--border); stroke-width: 1; }
  .cross { stroke: var(--text-muted); stroke-width: 1; }
  .focus { stroke: var(--text-primary); stroke-width: 1.5; stroke-dasharray: 3 3; }
  .ctx { fill: none; stroke: var(--seq); stroke-width: 2; stroke-linejoin: round; filter: drop-shadow(0 0 4px rgb(62 197 255 / .7)); }
  .labelbg { fill: var(--surface-solid); }
  .lane { fill: var(--text-secondary); font-size: 11px; text-anchor: end; }
  .tick { fill: var(--text-muted); font-size: 10px; text-anchor: end; font-variant-numeric: tabular-nums; }
  .tick.mid { text-anchor: middle; }
  .tip {
    position: absolute; width: 320px; pointer-events: none; z-index: 2;
    background: var(--surface-solid); border: 1px solid var(--border); border-radius: 6px; padding: 6px 8px;
    box-shadow: 0 4px 16px rgb(0 0 0 / .15); font-size: 12px;
  }
  .tip .mono { word-break: break-all; margin: 2px 0; }

  .lower { flex: 1; min-height: 320px; display: grid; grid-template-columns: minmax(0, 1fr); gap: 10px; padding: 10px 16px 16px; }
  .lower.split { grid-template-columns: minmax(0, 1fr) minmax(320px, 42%); }
  .list { display: flex; flex-direction: column; min-height: 0; background: var(--surface-1); border: 1px solid var(--border); border-radius: 8px; }
  .filters { display: flex; flex-wrap: wrap; gap: 4px; align-items: center; padding: 6px 8px; border-bottom: 1px solid var(--border); font-size: 12px; }
  .chip { display: flex; align-items: center; gap: 2px; padding: 1px 8px; border-radius: 10px; border-color: transparent; background: var(--surface-2); font-size: 12px; }
  .chip.off { background: none; opacity: .5; }
  .filters input[type=search] { margin-left: auto; width: 180px; }
  .rows { overflow: auto; flex: 1; outline: none; }
  .row {
    display: grid; grid-template-columns: 52px 64px 110px minmax(0, 1fr) 52px; gap: 8px; width: 100%;
    text-align: left; border: none; border-bottom: 1px solid var(--grid); border-radius: 0; background: none; padding: 2px 10px; font-size: 12px;
  }
  .row.sel { background: var(--surface-2); box-shadow: inset 3px 0 0 var(--accent); }
  .row.says .lbl { color: var(--text-secondary); font-style: italic; }
  .lbl { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .ctxc { text-align: right; }
  .kind { white-space: nowrap; }
  .pad { padding: 10px; }

  .detail { overflow: auto; min-height: 0; background: var(--surface-1); border: 1px solid var(--border); border-radius: 8px; padding: 10px 12px; }
  .dhead { display: flex; gap: 8px; align-items: baseline; }
  .close { order: 9; margin-left: auto; border: none; padding: 0 4px; color: var(--text-muted); }
  h3 { font-size: 12px; font-weight: 600; color: var(--text-secondary); margin: 12px 0 4px; }
  pre { margin: 0; padding: 8px; background: var(--surface-2); border-radius: 6px; white-space: pre-wrap; word-break: break-word; font-size: 12px; max-height: 50vh; overflow: auto; }
  pre.errout { box-shadow: inset 3px 0 0 var(--critical); }
</style>
