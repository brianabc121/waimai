<script setup lang="ts">
// A small time-series chart drawn by hand: stacked columns or lines over one
// shared axis, with a legend, a hover readout for every series, arrow-key
// navigation and the same numbers as a table for screen readers.
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { t } from '../../../i18n'

export interface ChartSeries { key: string; label: string; color: string; values: number[] }

const props = withDefaults(defineProps<{
  kind: 'bars' | 'lines'
  times: string[] // start of every point, ISO
  stepSeconds: number
  series: ChartSeries[]
  format: (v: number) => string
  height?: number
  label: string // what the chart shows, for assistive technology
  minMax?: number // the axis never tops out below this
}>(), { height: 190, minMax: 0 })

const box = ref<HTMLElement>()
const width = ref(480)
let observer: ResizeObserver | undefined
onMounted(() => {
  if (!box.value) return
  width.value = box.value.clientWidth || 480
  observer = new ResizeObserver(e => { width.value = Math.max(240, Math.floor(e[0].contentRect.width)) })
  observer.observe(box.value)
})
onUnmounted(() => observer?.disconnect())

const M = { top: 8, right: 10, bottom: 22, left: 46 }
const n = computed(() => props.times.length)
const plotW = computed(() => width.value - M.left - M.right)
const plotH = computed(() => props.height - M.top - M.bottom)
const band = computed(() => (n.value ? plotW.value / n.value : plotW.value))

// 1, 2 or 5 times a power of ten, so that the axis reads in round numbers
function nice(v: number): number {
  if (v <= 0) return 1
  const p = Math.pow(10, Math.floor(Math.log10(v)))
  const f = v / p
  return (f <= 1 ? 1 : f <= 2 ? 2 : f <= 5 ? 5 : 10) * p
}
const top = computed(() => {
  let max = props.minMax
  for (let i = 0; i < n.value; i++) {
    const vals = props.series.map(s => s.values[i] ?? 0)
    max = Math.max(max, props.kind === 'bars' ? vals.reduce((a, b) => a + b, 0) : Math.max(0, ...vals))
  }
  return nice(max)
})
const empty = computed(() => props.series.every(s => s.values.every(v => !v)))
const y = (v: number) => M.top + plotH.value * (1 - Math.min(v, top.value) / top.value)
const xMid = (i: number) => M.left + band.value * (i + 0.5)
const ticks = computed(() => [0, top.value / 2, top.value])

const pad = (v: number) => String(v).padStart(2, '0')
function clock(iso: string, long = false): string {
  const d = new Date(iso)
  const day = `${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
  const time = `${pad(d.getHours())}:${pad(d.getMinutes())}`
  if (props.stepSeconds >= 3600) return long ? `${day} ${time}` : day
  return long && props.stepSeconds > 60 ? `${day} ${time}` : time
}
// about five labels along the bottom, never the same text twice in a row
const xLabels = computed(() => {
  const out: { x: number; text: string }[] = []
  if (!n.value) return out
  const every = Math.max(1, Math.round(n.value / 5))
  for (let i = Math.floor(every / 2); i < n.value; i += every) {
    const text = clock(props.times[i])
    if (out.length === 0 || out[out.length - 1].text !== text) out.push({ x: xMid(i), text })
  }
  return out
})

// stacked columns, bottom series first
const columns = computed(() => {
  const w = Math.max(1, Math.min(24, band.value - 2))
  const out: { x: number; y: number; h: number; w: number; color: string; r: number }[] = []
  for (let i = 0; i < n.value; i++) {
    let base = 0
    const parts = props.series.map(s => s.values[i] ?? 0)
    const last = parts.reduce((acc, v, k) => (v > 0 ? k : acc), -1)
    parts.forEach((v, k) => {
      if (v <= 0) return
      const y0 = y(base), y1 = y(base + v)
      base += v
      // a hairline of the surface between touching segments, when they are tall enough to spare it
      const gap = k > 0 && y0 - y1 > 3 ? 1 : 0
      out.push({ x: xMid(i) - w / 2, y: y1, h: Math.max(1, y0 - y1 - gap), w, color: props.series[k].color, r: k === last ? Math.min(2, w / 2) : 0 })
    })
  }
  return out
})
const lines = computed(() => props.series.map(s => ({
  s,
  d: s.values.map((v, i) => `${i ? 'L' : 'M'}${xMid(i).toFixed(1)} ${y(v).toFixed(1)}`).join(' '),
  endX: xMid(n.value - 1), endY: y(s.values[n.value - 1] ?? 0),
})))
const area = computed(() => {
  if (props.kind !== 'lines' || props.series.length !== 1 || !n.value) return ''
  return `${lines.value[0].d} L${xMid(n.value - 1).toFixed(1)} ${y(0)} L${xMid(0).toFixed(1)} ${y(0)} Z`
})

// ---- hover ----
const at = ref(-1)
function move(e: PointerEvent) {
  const r = (e.currentTarget as SVGElement).getBoundingClientRect()
  const i = Math.floor((e.clientX - r.left - M.left) / band.value)
  at.value = i >= 0 && i < n.value ? i : -1
}
function key(e: KeyboardEvent) {
  if (e.key !== 'ArrowLeft' && e.key !== 'ArrowRight') return
  e.preventDefault()
  const start = at.value < 0 ? n.value - 1 : at.value
  at.value = Math.max(0, Math.min(n.value - 1, start + (e.key === 'ArrowLeft' ? -1 : 1)))
}
const tip = computed(() => {
  const i = at.value
  if (i < 0) return null
  const rows = props.series.map(s => ({ key: s.key, label: s.label, color: s.color, value: props.format(s.values[i] ?? 0) }))
  const left = xMid(i)
  return { rows, time: clock(props.times[i], true), left, flip: left > width.value * 0.6 }
})
</script>

<template>
  <div ref="box" class="chart">
    <div v-if="series.length > 1" class="legend">
      <span v-for="s in series" :key="s.key"><i :class="kind" :style="{ background: s.color }"></i>{{ s.label }}</span>
    </div>
    <svg :width="width" :height="height" role="img" :aria-label="label" tabindex="0"
      @pointermove="move" @pointerleave="at = -1" @keydown="key" @blur="at = -1">
      <g class="grid">
        <template v-for="v in ticks" :key="v">
          <line :x1="M.left" :x2="width - M.right" :y1="y(v)" :y2="y(v)" />
          <text :x="M.left - 6" :y="y(v) + 4" text-anchor="end">{{ format(v) }}</text>
        </template>
        <text v-for="l in xLabels" :key="l.x" :x="l.x" :y="height - 6" text-anchor="middle">{{ l.text }}</text>
      </g>
      <rect v-if="at >= 0 && kind === 'bars'" class="wash" :x="M.left + band * at" :y="M.top" :width="band" :height="plotH" />
      <template v-if="kind === 'bars'">
        <path v-for="(c, i) in columns" :key="i" :fill="c.color"
          :d="`M${c.x} ${c.y + c.h} V${c.y + c.r} Q${c.x} ${c.y} ${c.x + c.r} ${c.y} H${c.x + c.w - c.r} Q${c.x + c.w} ${c.y} ${c.x + c.w} ${c.y + c.r} V${c.y + c.h} Z`" />
      </template>
      <template v-else>
        <path v-if="area" :d="area" :fill="series[0].color" fill-opacity="0.1" />
        <line v-if="at >= 0" class="cross" :x1="xMid(at)" :x2="xMid(at)" :y1="M.top" :y2="M.top + plotH" />
        <path v-for="l in lines" :key="l.s.key" :d="l.d" fill="none" :stroke="l.s.color" stroke-width="2" stroke-linejoin="round" stroke-linecap="round" />
        <template v-if="n">
          <circle v-for="l in lines" :key="l.s.key" :cx="at >= 0 ? xMid(at) : l.endX" :cy="at >= 0 ? y(l.s.values[at] ?? 0) : l.endY" r="4" :fill="l.s.color" stroke="#fff" stroke-width="2" />
        </template>
      </template>
      <text v-if="empty" class="none" :x="M.left + plotW / 2" :y="M.top + plotH / 2" text-anchor="middle">{{ t('ad.no_traffic') }}</text>
    </svg>
    <div v-if="tip" class="tip" :style="tip.flip ? { right: width - tip.left + 12 + 'px' } : { left: tip.left + 12 + 'px' }">
      <div class="when">{{ tip.time }}</div>
      <div v-for="r in tip.rows" :key="r.key" class="r"><i :style="{ background: r.color }"></i><b>{{ r.value }}</b><span>{{ r.label }}</span></div>
    </div>
    <div class="sr-only"><table>
      <caption>{{ label }}</caption>
      <thead><tr><th>{{ t('ad.time') }}</th><th v-for="s in series" :key="s.key">{{ s.label }}</th></tr></thead>
      <tbody><tr v-for="(tm, i) in times" :key="tm"><td>{{ clock(tm, true) }}</td><td v-for="s in series" :key="s.key">{{ format(s.values[i] ?? 0) }}</td></tr></tbody>
    </table></div>
  </div>
</template>
