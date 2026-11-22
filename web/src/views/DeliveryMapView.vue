<script setup lang="ts">
// The merchant's delivery map: store location, zone outlines, overlap order,
// gaps and tolerance. Design: docs/MAP_ZONES_DESIGN.md.
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { del, get, post, put } from '../api'
import { both, money, pick, t, toMajor, toMinor } from '../i18n'
import { session } from '../session'
import { mapsKey as loadMapsKey, type Spot as Place } from '../maps'
import * as geo from '../geo'
import * as cutter from '../cut'
import type { LatLng, Pt } from '../geo'
import type { Branch, Zone } from '../types'
import MapCanvas from './MapCanvas.vue'
import './mapStrings'

type Mode = 'browse' | 'draw' | 'circle' | 'overlap' | 'edit' | 'location' | 'gaps'
type Place2 = 'new_over' | 'old_over'

const COLORS = ['#2a78d6', '#18794e', '#7c3aed', '#c2255c', '#e0a100', '#0b7285']
const DEFAULT_CENTER: LatLng = [14.5995, 120.9842] // Manila, until the store has a location
const SNAP_PX = 12

const bid = session.branchId!
const readOnly = computed(() => !!session.user?.is_platform_admin)
const key = ref<string | null>(null)
const branch = ref<Branch | null>(null)
const zones = ref<Zone[]>([])
const mode = ref<Mode>('browse')
const error = ref(''), notice = ref('')
const map = ref<InstanceType<typeof MapCanvas>>()
const mapReady = ref(false)
const narrow = ref(false)
const mapType = ref<'roadmap' | 'satellite'>('roadmap')
const selected = ref(0)

const cur = computed(() => branch.value?.currency ?? 'PHP')
const store = computed<LatLng | null>(() => branch.value?.lat != null && branch.value.lng != null ? [branch.value.lat, branch.value.lng] : null)
const colorOf = (z: Zone) => z.color || COLORS[zones.value.findIndex(x => x.id === z.id) % COLORS.length]
const zname = (z: { name: string; name_en: string }) => pick(z.name, z.name_en || z.name)
const drawn = computed(() => zones.value.filter(z => z.polygon.length >= 3))

// One plane for all geometry on this page, fixed while the page is open.
let planeRef: LatLng | null = null
function pl() {
  planeRef ??= store.value ?? drawn.value[0]?.polygon[0] ?? DEFAULT_CENTER
  return geo.plane(planeRef)
}
const rings = computed(() => drawn.value.map(z => ({ id: z.id, zone: z, ring: pl().ring(z.polygon) })))

async function load() {
  const m = await get(`/merchant/branches/${bid}/menu`)
  branch.value = m.branch; zones.value = m.zones
}

const mq = window.matchMedia('(max-width: 767px)')
const onMq = () => { narrow.value = mq.matches }
onMounted(async () => {
  onMq(); mq.addEventListener('change', onMq)
  window.addEventListener('keydown', onKey); window.addEventListener('keyup', onKeyUp)
  try {
    const [k] = await Promise.all([loadMapsKey(), load()])
    key.value = k
    if (k && !store.value && !readOnly.value) startLocation()
  } catch (e) { error.value = (e as Error).message; key.value = '' }
})
onBeforeUnmount(() => {
  mq.removeEventListener('change', onMq)
  window.removeEventListener('keydown', onKey); window.removeEventListener('keyup', onKeyUp)
})

const initialView = computed<geo.View>(() => ({ center: store.value ?? drawn.value[0]?.polygon[0] ?? DEFAULT_CENTER, zoom: store.value ? 15 : 12 }))
function fitAll() {
  const ptsAll = [...drawn.value.flatMap(z => z.polygon), ...(store.value ? [store.value] : [])]
  if (ptsAll.length > 1) map.value?.fit(ptsAll, 16)
}
function onReady() { mapReady.value = true; fitAll(); void fillRegion() }

async function run<T>(fn: () => Promise<T>): Promise<T | undefined> {
  error.value = ''
  try { return await fn() } catch (e) { error.value = errText(e); return undefined }
}
function errText(e: unknown): string {
  const c = (e as { code?: string }).code
  if (c === 'MAP_NOT_READY') return t('mz.not_ready')
  if (c === 'ZONE_SELF_INTERSECTS') return t('mz.crossing')
  return (e as Error).message
}

// ---- drawing ----
const pts = ref<LatLng[]>([])
const hover = ref<LatLng | null>(null)
const snapOn = ref(true)
const altDown = ref(false)
const snapMsg = ref('')
const drawFor = ref<Zone | null>(null) // an existing zone that has no outline yet
const radiusKm = ref(1)

// snapTo pulls a point onto a nearby corner or border of another zone.
function snapTo(at: LatLng, exclude = 0): { at: LatLng; msg: string } {
  if (!snapOn.value || altDown.value || !map.value) return { at, msg: '' }
  const others = rings.value.filter(r => r.id !== exclude)
  const s = geo.snap(pl().to(at), others, SNAP_PX * map.value.mpp())
  if (!s) return { at, msg: '' }
  const r = others.find(x => x.id === s.zone)!
  const i = r.ring.indexOf(s.p)
  // A corner snap reuses the neighbour's own coordinates, so both outlines
  // hold exactly the same point.
  return { at: i >= 0 ? [...r.zone.polygon[i]] as LatLng : pl().from(s.p), msg: t(s.kind === 'vertex' ? 'mz.snapped_vertex' : 'mz.snapped_edge', { name: zname(r.zone) }) }
}
const pxDist = (a: LatLng, b: LatLng) => { const p = map.value!.px(a), q = map.value!.px(b); return Math.hypot(p.x - q.x, p.y - q.y) }

function startDraw(zone: Zone | null = null) {
  if (readOnly.value || narrow.value) return
  error.value = ''; notice.value = ''
  pts.value = []; hover.value = null; snapMsg.value = ''; drawFor.value = zone
  mode.value = 'draw'
}
function startCircle() { if (!readOnly.value && store.value) { error.value = ''; drawFor.value = null; mode.value = 'circle' } }
function makeCircle() {
  const r = Math.max(0.1, Math.min(30, Number(radiusKm.value) || 1)) * 1000
  pts.value = pl().back(geo.circle(pl().to(store.value!), r))
  finishShape()
}
function cancelDraw() { pts.value = []; mode.value = 'browse'; error.value = '' }
function undoPoint() { pts.value.pop(); error.value = '' }

function onClick(at: LatLng) {
  if (mode.value === 'draw') {
    if (pts.value.length >= 3 && pxDist(at, pts.value[0]) <= SNAP_PX) return finishShape()
    const s = snapTo(at)
    const last = pts.value[pts.value.length - 1]
    if (last && pxDist(s.at, last) < 3) return // the second click of a double-click
    pts.value.push(s.at); error.value = ''
  } else if (mode.value === 'location') {
    setPin(at)
  } else if (mode.value === 'browse') {
    selected.value = 0
  }
}
// A pen while drawing (a pointing hand over the first corner, where a click
// closes the outline), a pin while placing the store.
const cursor = computed(() => {
  if (mode.value === 'location') return 'pin'
  if (mode.value !== 'draw') return ''
  return pts.value.length >= 3 && hover.value && pxDist(hover.value, pts.value[0]) <= SNAP_PX ? 'pointer' : 'pen'
})
function onDblClick() { if (mode.value === 'draw' && pts.value.length >= 3) finishShape() }
function onMove(at: LatLng) {
  if (mode.value !== 'draw') return
  const s = snapTo(at)
  hover.value = s.at; snapMsg.value = s.msg
}
function onKey(e: KeyboardEvent) {
  if (e.key === 'Alt') altDown.value = true
  if ((e.target as HTMLElement).closest?.('input, textarea, select')) return
  if (mode.value === 'draw') {
    if (e.key === 'Backspace') { e.preventDefault(); undoPoint() }
    if (e.key === 'Escape') cancelDraw()
  }
}
function onKeyUp(e: KeyboardEvent) { if (e.key === 'Alt') altDown.value = false }

const badEdges = ref<number[]>([])
function checkShape(poly: LatLng[]): boolean {
  badEdges.value = []
  if (poly.length < 3) { error.value = t('mz.need3'); return false }
  const ring = pl().ring(poly)
  const x = geo.selfIntersections(ring)
  if (x.length) { badEdges.value = [...new Set(x.flat())]; error.value = t('mz.crossing'); return false }
  if (store.value && geo.maxReach(pl().to(store.value), ring) > 30000) { error.value = t('mz.too_far'); return false }
  return true
}
function finishShape() {
  if (!checkShape(pts.value)) { if (mode.value === 'circle') pts.value = []; return }
  const z = drawFor.value
  const used = new Set(zones.value.map(colorOf))
  openEditor(z ?? { id: 0, name: '', name_en: '', fee: 0, min_order: 0, active: true, sort: 0, polygon: [], eta_minutes: null,
    color: COLORS.find(c => !used.has(c)) ?? COLORS[zones.value.length % COLORS.length] }, pts.value, true)
  pts.value = []
}

// ---- editing one zone ----
const ed = reactive({ id: 0, name: '', name_en: '', fee: 0, min_order: 0, eta: '' as number | '', color: COLORS[0], active: true, polygon: [] as LatLng[] })
const place = ref<Place2 | null>(null)
const choice = ref<Place2 | null>(null)
let overlapBefore = new Set<number>()
let fresh = false // the outline was just drawn and is not saved yet

const edRing = computed(() => pl().ring(ed.polygon))
// The polygon arithmetic can refuse a degenerate outline; the plain tests in
// geo.ts stand in then, and nothing is cut.
function safe<T>(fn: () => T, fallback: () => T): T { try { return fn() } catch { return fallback() } }
const ringsOverlap = (a: Pt[], b: Pt[]) => safe(() => cutter.overlaps(a, b), () => geo.overlaps(a, b))
const overlapZones = computed(() => ['edit', 'overlap'].includes(mode.value) && ed.polygon.length >= 3
  ? rings.value.filter(r => r.id !== ed.id && ringsOverlap(edRing.value, r.ring)).map(r => r.zone) : [])
const overlapNames = computed(() => overlapZones.value.map(zname).join(pick('、', ', ')))
const overlapM2 = computed(() => overlapZones.value.reduce((s, z) => { const r = pl().ring(z.polygon)
  return s + safe(() => cutter.sharedArea(edRing.value, r), () => geo.overlapArea(edRing.value, r)) }, 0))

// ---- cutting the overlap out of the zone that loses it ----
// zone null is the zone being edited. pieces[0] stays that zone; further
// pieces exist when the cut splits it.
interface Reshape { zone: Zone | null; pieces: LatLng[][] }
interface CutPlan { reshapes: Reshape[]; gone: boolean }
const split = ref<'keep' | 'drop'>('keep')
// Corners that already exist keep their exact coordinates, so that two zones
// along one border hold the very same points.
function backExact(ring: Pt[]): LatLng[] {
  const known: [Pt, LatLng][] = [...edRing.value.map((p, i) => [p, ed.polygon[i]] as [Pt, LatLng]),
    ...rings.value.flatMap(r => r.ring.map((p, i) => [p, r.zone.polygon[i]] as [Pt, LatLng]))]
  return ring.map(p => { const k = known.find(([q]) => q === p || geo.dist(q, p) < 1e-3); return k ? [...k[1]] as LatLng : pl().from(p) })
}
function planFor(c: Place2): CutPlan {
  const plan: CutPlan = { reshapes: [], gone: false }
  const others = overlapZones.value.map(z => ({ zone: z, ring: pl().ring(z.polygon) }))
  try {
    if (c === 'new_over') {
      for (const o of others) {
        const cut = cutter.subtract(o.ring, [edRing.value])
        if (cut.holes) continue // this zone lies inside the other: the list order decides
        if (!cut.pieces.length) plan.gone = true
        else plan.reshapes.push({ zone: o.zone, pieces: cut.pieces.map(backExact) })
      }
    } else {
      const clips = others.filter(o => !cutter.subtract(edRing.value, [o.ring]).holes).map(o => o.ring)
      if (clips.length) {
        const cut = cutter.subtract(edRing.value, clips)
        if (!cut.holes) {
          if (!cut.pieces.length) plan.gone = true
          else plan.reshapes.push({ zone: null, pieces: cut.pieces.map(backExact) })
        }
      }
    }
  } catch { return { reshapes: [], gone: false } }
  return plan
}
const canChoose = computed(() => mode.value === 'overlap'
  ? { new_over: !planFor('new_over').gone, old_over: !planFor('old_over').gone } : { new_over: true, old_over: true })
const plan = computed<CutPlan | null>(() => place.value && mode.value === 'edit' && overlapZones.value.length ? planFor(place.value) : null)
const isCut = computed(() => !!plan.value?.reshapes.length)
const hasSplit = computed(() => !!plan.value?.reshapes.some(r => r.pieces.length > 1))
// What to draw for a zone while the cut is previewed: [kept piece, ...split-off pieces].
const shapesOf = (z: Zone): LatLng[][] => plan.value?.reshapes.find(r => r.zone?.id === z.id)?.pieces ?? [z.polygon]
const edShapes = computed<LatLng[][]>(() => plan.value?.reshapes.find(r => !r.zone)?.pieces ?? [ed.polygon])
const edStats = computed(() => ({ n: ed.polygon.length, area: geo.area(edRing.value), reach: store.value ? geo.maxReach(pl().to(store.value), edRing.value) : 0 }))

function openEditor(z: Zone, polygon: LatLng[], isFresh: boolean) {
  Object.assign(ed, { id: z.id, name: z.name, name_en: z.name_en, fee: toMajor(z.fee), min_order: toMajor(z.min_order),
    eta: z.eta_minutes ?? '', color: z.color || colorOf(z), active: z.active, polygon: polygon.map(p => [...p] as LatLng) })
  fresh = isFresh; place.value = null; choice.value = null; split.value = 'keep'; error.value = ''; notice.value = ''; badEdges.value = []
  cutUndo.value = null
  mode.value = 'edit'
  // What it overlapped before this edit needs no new decision.
  overlapBefore = new Set(isFresh ? [] : overlapZones.value.map(x => x.id))
  if (isFresh && overlapZones.value.length) mode.value = 'overlap'
  selected.value = z.id
}
function editZone(z: Zone) {
  if (readOnly.value) return
  if (z.polygon.length < 3) { if (narrow.value) openEditor(z, [], false); else startDraw(z); return }
  openEditor(z, z.polygon, false)
  map.value?.fit(z.polygon, 16)
}
function choose(c: Place2) { if (canChoose.value[c]) choice.value = c }
function confirmChoice() { if (choice.value) { place.value = choice.value; mode.value = 'edit' } }
function rechoose() { choice.value = place.value; mode.value = 'overlap' }
function redraw() { const z = zones.value.find(x => x.id === ed.id) ?? null; startDraw(z) }
function cancelEdit() { mode.value = 'browse'; error.value = ''; badEdges.value = [] }

let pressAt = 0
function dragVertex(i: number, e: PointerEvent) {
  if (e.button !== 0) return
  pressAt = Date.now()
  map.value!.drag(e, at => { const s = snapTo(at, ed.id); ed.polygon[i] = s.at; snapMsg.value = s.msg }, moved => {
    snapMsg.value = ''
    if (!moved && Date.now() - pressAt > 600) removeVertex(i) // long press on a touch screen
    else if (moved) checkShape(ed.polygon)
  })
}
function addVertex(i: number, e: PointerEvent) {
  if (e.button !== 0) return
  const a = ed.polygon[i], b = ed.polygon[(i + 1) % ed.polygon.length]
  ed.polygon.splice(i + 1, 0, [(a[0] + b[0]) / 2, (a[1] + b[1]) / 2])
  dragVertex(i + 1, e)
}
function removeVertex(i: number) {
  if (ed.polygon.length <= 3) { error.value = t('mz.min3'); return }
  ed.polygon.splice(i, 1); checkShape(ed.polygon)
}

// newOrder puts `id` where the overlap choice says: above every zone it
// overlaps, or below all of them. Without a choice the order is kept (a new
// zone goes last).
function newOrder(id: number): number[] {
  const ids = zones.value.map(z => z.id)
  if (!place.value || !overlapZones.value.length) return ids.includes(id) ? ids : [...ids, id]
  const rest = ids.filter(x => x !== id), over = overlapZones.value.map(z => rest.indexOf(z.id))
  const at = place.value === 'new_over' ? Math.min(...over) : Math.max(...over) + 1
  return [...rest.slice(0, at), id, ...rest.slice(at)]
}
async function saveZone() {
  if (!ed.name.trim()) { error.value = t('mz.name') + ' ?'; return }
  if (ed.polygon.length && !checkShape(ed.polygon)) return
  // An overlap that was not there before needs a decision first.
  if (!place.value && overlapZones.value.some(z => !overlapBefore.has(z.id))) { choice.value = null; mode.value = 'overlap'; return }
  const mine = { name: ed.name.trim(), name_en: ed.name_en.trim(), fee: toMinor(ed.fee), min_order: toMinor(ed.min_order), active: ed.active,
    sort: zones.value.find(z => z.id === ed.id)?.sort ?? zones.value.length, color: ed.color, eta_minutes: ed.eta === '' ? null : Number(ed.eta) }
  const inputOf = (z: Zone) => ({ name: z.name, name_en: z.name_en, fee: z.fee, min_order: z.min_order, active: z.active, sort: z.sort, color: z.color, eta_minutes: z.eta_minutes })
  // The zone itself first, then the zones it cuts, then pieces split off as
  // zones of their own (each named after where it came from).
  const cuts = plan.value?.reshapes ?? []
  const changes: Record<string, unknown>[] = [{ id: ed.id, ...mine, polygon: ed.polygon.length ? edShapes.value[0] : null }]
  const undo: Record<string, unknown>[] = [{ ...mine, polygon: ed.polygon.length ? ed.polygon.map(p => [...p]) : null }]
  for (const r of cuts) if (r.zone) {
    changes.push({ id: r.zone.id, ...inputOf(r.zone), polygon: r.pieces[0] })
    undo.push({ id: r.zone.id, ...inputOf(r.zone), polygon: r.zone.polygon })
  }
  const pieceOf: number[] = [] // for each split-off piece, the index in `changes` of the zone it came from
  if (split.value === 'keep') for (const r of cuts) r.pieces.slice(1).forEach((piece, k) => {
    const src = r.zone ? inputOf(r.zone) : mine
    pieceOf.push(r.zone ? changes.findIndex(c => c.id === r.zone!.id) : 0)
    changes.push({ id: 0, ...src, name: `${src.name}-${k + 2}`, name_en: src.name_en ? `${src.name_en}-${k + 2}` : '', polygon: piece })
  })
  if (changes.some(c => ((c.polygon as LatLng[] | null)?.length ?? 0) > 200)) { error.value = t('mz.too_many'); return }
  const first = changes.length - pieceOf.length
  const ok = await run(async () => {
    const { ids } = await put<{ ids: number[] }>(`/merchant/branches/${bid}/zone-batch`, { zones: changes })
    // The choice also sets the list order, which still decides whatever a cut cannot.
    let order = newOrder(ids[0])
    pieceOf.forEach((src, k) => { order = order.filter(x => x !== ids[first + k]); order.splice(order.indexOf(ids[src]) + 1, 0, ids[first + k]) })
    const before = zones.value.map(z => z.id)
    if (order.length !== before.length || order.some((x, i) => x !== before[i])) await put(`/merchant/branches/${bid}/zone-order`, { ids: order })
    await load()
    return ids
  })
  if (ok === undefined) return
  const id = ok[0]
  mode.value = 'browse'; selected.value = id
  const n = gaps.value.filter(g => g.a === id || g.b === id).length
  if (cuts.length) {
    undo[0].id = id
    cutUndo.value = { zones: undo, delete: ok.slice(first) }
    notice.value = t('mz.cut_done')
  } else notice.value = n ? t('mz.gaps_found', { n }) : t('mz.saved')
}
// One step back after a cut: every zone it touched gets its outline back and
// the pieces it split off are removed. The zones overlap again, as before.
const cutUndo = ref<{ zones: Record<string, unknown>[]; delete: number[] } | null>(null)
async function undoCut() {
  const u = cutUndo.value
  if (!u) return
  // The list keeps the order it has now: only the outlines go back.
  const order = zones.value.map(z => z.id).filter(id => !u.delete.includes(id))
  if (await run(async () => {
    await put(`/merchant/branches/${bid}/zone-batch`, u)
    await put(`/merchant/branches/${bid}/zone-order`, { ids: order })
    await load(); return true
  })) { cutUndo.value = null; notice.value = t('mz.cut_undone') }
}

// ---- overlaps that are already there ----
// Zones saved before overlaps were cut, or moved since. A zone lying inside
// another and listed above it is fine as it is: that is how an island works.
const overlapPairs = computed(() => {
  const out: { a: Zone; b: Zone }[] = [], rs = rings.value
  for (let i = 0; i < rs.length; i++) for (let j = i + 1; j < rs.length; j++) {
    if (!ringsOverlap(rs[i].ring, rs[j].ring)) continue
    if (safe(() => cutter.subtract(rs[j].ring, [rs[i].ring]).holes, () => false)) continue
    out.push({ a: rs[i].zone, b: rs[j].zone })
  }
  return out
})
// The lower zone is opened and the question is asked at once.
function resolveOverlap(p: { a: Zone; b: Zone }) {
  if (readOnly.value) return
  openEditor(p.b, p.b.polygon, false)
  overlapBefore = new Set()
  mode.value = 'overlap'
  map.value?.fit([...p.a.polygon, ...p.b.polygon], 16)
}
async function removeZone() {
  const z = zones.value.find(x => x.id === ed.id)
  if (!z) return cancelEdit()
  if (!confirm(t('mz.delete_confirm', { name: zname(z) }) + (z.polygon.length >= 3 ? '\n' + t('mz.delete_hole') : ''))) return
  if (await run(async () => { await del(`/merchant/branches/${bid}/zones/${z.id}`); await load(); return true })) { mode.value = 'browse'; notice.value = '' }
}

// ---- list: order, mode ----
async function reorder(ids: number[]) {
  await run(async () => { await put(`/merchant/branches/${bid}/zone-order`, { ids }); await load() })
}
function nudge(i: number, d: number) {
  const ids = zones.value.map(z => z.id), j = i + d
  if (j < 0 || j >= ids.length) return
  ;[ids[i], ids[j]] = [ids[j], ids[i]]
  reorder(ids)
}
const dragRow = ref(-1), overRow = ref(-1)
function dropRow(i: number) {
  const from = dragRow.value
  dragRow.value = overRow.value = -1
  if (from < 0 || from === i) return
  const ids = zones.value.map(z => z.id), [m] = ids.splice(from, 1)
  ids.splice(i, 0, m)
  reorder(ids)
}
const tol = ref(200)
async function setMode(m: 'list' | 'map') {
  if (readOnly.value || branch.value?.zone_mode === m) return
  const b = await run(() => put<Branch>(`/merchant/branches/${bid}/zone-mode`, { zone_mode: m, zone_tolerance_m: branch.value!.zone_tolerance_m }))
  if (b) branch.value = b
}

// ---- store location ----
const loc = reactive({ lat: 0, lng: 0, address: '' })
const pin = computed<LatLng>(() => [Number(loc.lat) || 0, Number(loc.lng) || 0])
function startLocation() {
  const c = store.value ?? map.value?.view.center ?? DEFAULT_CENTER
  Object.assign(loc, { lat: round6(c[0]), lng: round6(c[1]), address: branch.value?.address ?? '' })
  error.value = ''; notice.value = ''; mode.value = 'location'
}
const round6 = (v: number) => Math.round(v * 1e6) / 1e6
function setPin(at: LatLng) { loc.lat = round6(at[0]); loc.lng = round6(at[1]) }
const movedM = computed(() => store.value ? geo.dist(pl().to(store.value), pl().to(pin.value)) : 0)
// With the location goes what the map says about it: country, province, city,
// district and postcode. The customer's address search is kept to that country.
async function saveLocation() {
  const region = await map.value!.resolve(pin.value)
  const b = await run(() => put<Branch>(`/merchant/branches/${bid}/location`, { lat: pin.value[0], lng: pin.value[1], address: loc.address, region: region ?? undefined }))
  if (b) { branch.value = b; mode.value = 'browse'; notice.value = t('mz.saved') }
}
// A store whose location was saved before this was read off the map (or while
// the map could not tell) gets it when the page is opened.
const resolving = ref(false)
async function fillRegion() {
  const b = branch.value
  if (!b || !store.value || b.country_code || readOnly.value || resolving.value) return
  resolving.value = true
  try {
    const region = await map.value?.resolve(store.value)
    if (region) branch.value = await put<Branch>(`/merchant/branches/${bid}/location`, { lat: store.value[0], lng: store.value[1], address: '', region })
  } catch { /* stays unknown; the line under the address says so */ }
  resolving.value = false
}
const regionText = computed(() => { const b = branch.value; return b ? [b.country, b.province, b.city, b.district].filter(Boolean).join(' · ') + (b.postal_code ? ` (${b.postal_code})` : '') : '' })

// ---- search ----
const q = ref(''), hits = ref<Place[] | null>(null)
async function search() {
  if (!q.value.trim()) { hits.value = null; return }
  hits.value = await map.value!.search(q.value)
  if (hits.value.length === 1) go(hits.value[0])
}
function go(p: Place) {
  hits.value = null
  map.value!.setView({ center: p.at, zoom: Math.max(16, map.value!.view.zoom) })
  if (mode.value === 'location') { setPin(p.at); if (!geoLike(p.label)) loc.address = p.label }
}
const geoLike = (s: string) => /^-?\d+(\.\d+)?\s*[,，\s]\s*-?\d+(\.\d+)?$/.test(s.trim())

// ---- gaps and tolerance ----
// Strips up to the tolerance are harmless (the nearest zone takes them);
// looking a bit further also finds the ones that would lose orders.
const gapReach = computed(() => Math.max(branch.value?.zone_tolerance_m ?? 0, 100))
const gaps = computed(() => geo.findGaps(rings.value.filter(r => r.zone.active), gapReach.value))
const gapsWide = computed(() => gaps.value.some(g => g.width > (branch.value?.zone_tolerance_m ?? 0)))
const zoneById = (id: number) => zones.value.find(z => z.id === id)!
let undo: { id: number; polygon: LatLng[] } | null = null
const canUndo = ref(false)
function openGaps() { tol.value = branch.value?.zone_tolerance_m ?? 200; error.value = ''; notice.value = ''; mode.value = 'gaps' }
function showGap(g: geo.Gap) { map.value?.fit(pl().back(g.shapes.flat()), 18) }
async function putPolygon(z: Zone, polygon: LatLng[]) {
  await put(`/merchant/branches/${bid}/zones/${z.id}`, { name: z.name, name_en: z.name_en, fee: z.fee, min_order: z.min_order, active: z.active,
    sort: z.sort, polygon, color: z.color, eta_minutes: z.eta_minutes })
  await load()
}
async function closeGap(g: geo.Gap) {
  const high = rings.value.find(r => r.id === g.a)!, low = rings.value.find(r => r.id === g.b)!
  const fixed = geo.closeGap(low.ring, high.ring, gapReach.value)
  if (!fixed) { error.value = t('mz.close_failed'); return }
  // Corners moved onto the neighbour's corners take its exact coordinates.
  const polygon = fixed.map(p => { const i = high.ring.findIndex(h => geo.dist(h, p) < 1e-6); return i >= 0 ? [...high.zone.polygon[i]] as LatLng : pl().from(p) })
  const old = low.zone.polygon
  if (await run(async () => { await putPolygon(low.zone, polygon); return true })) {
    undo = { id: low.id, polygon: old }; canUndo.value = true
    notice.value = t('mz.closed', { a: zname(high.zone), b: zname(low.zone) })
  }
}
async function undoClose() {
  if (!undo) return
  const u = undo
  if (await run(async () => { await putPolygon(zoneById(u.id), u.polygon); return true })) { undo = null; canUndo.value = false; notice.value = '' }
}
async function saveTolerance() {
  const b = await run(() => put<Branch>(`/merchant/branches/${bid}/zone-mode`, { zone_mode: branch.value!.zone_mode, zone_tolerance_m: Math.round(Number(tol.value) || 0) }))
  if (b) { branch.value = b; notice.value = t('mz.saved') }
}

// ---- drawing helpers for the template ----
type Px = (l: LatLng) => Pt
const poly = (ps: LatLng[], px: Px) => ps.map(p => { const s = px(p); return `${s.x.toFixed(1)},${s.y.toFixed(1)}` }).join(' ')
const mid = (a: LatLng, b: LatLng): LatLng => [(a[0] + b[0]) / 2, (a[1] + b[1]) / 2]
// A name that would sit on the store's pin moves below it.
const labelAt = (ps: LatLng[], px: Px) => {
  const p = px(pl().from(geo.labelPoint(pl().ring(ps)))), s = store.value && px(store.value)
  return s && Math.abs(p.x - s.x) < 90 && p.y > s.y - 60 && p.y < s.y + 44 ? { x: p.x, y: s.y + 50 } : p
}
const textW = (s: string, size: number) => [...s].reduce((w, ch) => w + (ch.charCodeAt(0) > 255 ? size : size * 0.58), 0)
const feeLine = (z: Zone) => `${money(z.fee, cur.value)} · ${t('mz.min')} ${money(z.min_order, cur.value)}`
// Zones above z in the list cover it where they overlap; their shapes are cut
// out of z's fill so that the overlap shows the winner's colour.
const above = (z: Zone) => { const i = zones.value.indexOf(z); return drawn.value.filter(x => x.active && zones.value.indexOf(x) < i) }
const shown = computed(() => [...drawn.value].reverse().filter(z => !(['edit', 'overlap'].includes(mode.value) && z.id === ed.id)))
const dimmed = computed(() => mode.value !== 'browse' && mode.value !== 'gaps')
const edColor = computed(() => ed.color)
const hint = computed(() => mode.value === 'edit' && ed.polygon.length ? t('mz.hint_edit') : mode.value === 'location' ? t('mz.drag_pin') : '')
const undrawnCount = computed(() => zones.value.filter(z => z.active && z.polygon.length < 3).length)

// ---- text-only fallback when the platform has no map key ----
const fz = reactive({ id: 0, name: '', name_en: '', fee: 0, min_order: 0, active: true })
function fbEdit(z: Zone | null) {
  Object.assign(fz, z ? { id: z.id, name: z.name, name_en: z.name_en, fee: toMajor(z.fee), min_order: toMajor(z.min_order), active: z.active }
    : { id: 0, name: '', name_en: '', fee: 0, min_order: 0, active: true })
}
async function fbSave() {
  const z = zones.value.find(x => x.id === fz.id)
  const body = { name: fz.name, name_en: fz.name_en, fee: toMinor(fz.fee), min_order: toMinor(fz.min_order), active: fz.active,
    sort: z?.sort ?? zones.value.length, polygon: z?.polygon.length ? z.polygon : null, color: z?.color ?? '', eta_minutes: z?.eta_minutes ?? null }
  if (await run(async () => { await (fz.id ? put(`/merchant/branches/${bid}/zones/${fz.id}`, body) : post(`/merchant/branches/${bid}/zones`, body)); await load(); return true })) fbEdit(null)
}
</script>

<template>
  <div v-if="key === null" class="page muted">{{ t('mz.loading') }}</div>

  <div v-else-if="!key" class="page stack" data-testid="zones-fallback">
    <div class="banner info">{{ t('mz.no_key') }}</div>
    <div v-if="error" class="banner bad">{{ error }}</div>
    <div class="card stack" v-if="branch">
      <h2>{{ t('mz.zones') }}</h2>
      <table class="data">
        <thead><tr><th>{{ t('mz.name') }}</th><th>{{ t('mz.fee') }}</th><th>{{ t('mz.min') }}</th><th></th><th></th></tr></thead>
        <tbody><tr v-for="z in zones" :key="z.id">
          <td>{{ both(z.name, z.name_en) }}</td><td class="price">{{ money(z.fee, cur) }}</td>
          <td class="price">{{ z.min_order ? money(z.min_order, cur) : '—' }}</td>
          <td><span class="tag" :class="z.active ? 'ok' : ''">{{ z.active ? t('mz.on') : t('mz.off') }}</span></td>
          <td class="right"><button v-if="!readOnly" class="btn small" @click="fbEdit(z)">{{ t('mz.edit') }}</button></td>
        </tr></tbody>
      </table>
      <form v-if="!readOnly" class="row wrap" @submit.prevent="fbSave">
        <input v-model="fz.name" :placeholder="t('mz.name')" required style="flex:1 1 140px" />
        <input v-model="fz.name_en" :placeholder="t('mz.name_en')" style="flex:1 1 140px" />
        <input type="number" min="0" step="0.01" v-model.number="fz.fee" :title="t('mz.fee')" style="flex:0 1 100px" />
        <input type="number" min="0" step="0.01" v-model.number="fz.min_order" :title="t('mz.min')" style="flex:0 1 100px" />
        <label class="check" style="margin:0"><input type="checkbox" v-model="fz.active" />{{ t('mz.on') }}</label>
        <button class="btn primary">{{ fz.id ? t('mz.save') : t('new_zone') }}</button>
        <button v-if="fz.id" type="button" class="btn" @click="fbEdit(null)">{{ t('mz.cancel') }}</button>
      </form>
    </div>
  </div>

  <div v-else-if="branch" class="dmap" :data-mode="mode">
    <MapCanvas ref="map" :maps-key="key" :initial="initialView" :cursor="cursor" data-testid="map"
      @ready="onReady" @click="onClick" @dblclick="onDblClick" @move="onMove">
      <template #default="{ px }">
        <defs>
          <pattern id="dm-hatch" width="8" height="8" patternUnits="userSpaceOnUse" patternTransform="rotate(45)">
            <rect width="8" height="8" fill="#ffd43b" /><rect width="3.5" height="8" fill="#1f2328" />
          </pattern>
          <mask v-for="z in shown" :key="'m' + z.id" :id="'dm-m' + z.id" maskUnits="userSpaceOnUse" x="-10000" y="-10000" width="20000" height="20000">
            <rect x="-10000" y="-10000" width="20000" height="20000" fill="#fff" />
            <polygon v-for="a in above(z)" :key="a.id" :points="poly(a.polygon, px)" fill="#000" />
          </mask>
          <clipPath id="dm-ov"><polygon v-for="z in overlapZones" :key="z.id" :points="poly(z.polygon, px)" /></clipPath>
        </defs>

        <!-- saved zones, lowest priority first so the winner is drawn last -->
        <g v-for="z in shown" :key="z.id" :opacity="dimmed ? 0.5 : z.active ? 1 : 0.45" :data-zone="z.id">
          <polygon :points="poly(shapesOf(z)[0], px)" :fill="colorOf(z)" :fill-opacity="selected === z.id && !dimmed ? 0.32 : 0.18" :mask="`url(#dm-m${z.id})`"
            :class="{ hit: mode === 'browse' }" @click.stop="selected = z.id" />
          <polygon :points="poly(shapesOf(z)[0], px)" fill="none" :stroke="colorOf(z)" :stroke-width="selected === z.id && !dimmed ? 3 : 2"
            stroke-linejoin="round" :stroke-dasharray="z.active ? undefined : '6 5'" />
          <!-- pieces the cut splits off: kept as a zone of their own, or dropped -->
          <polygon v-for="(piece, k) in shapesOf(z).slice(1)" :key="k" :points="poly(piece, px)" :fill="split === 'keep' ? colorOf(z) : '#b42318'" fill-opacity="0.12"
            :stroke="split === 'keep' ? colorOf(z) : '#b42318'" stroke-width="2" stroke-dasharray="6 5" stroke-linejoin="round" data-testid="split-piece" />
        </g>

        <!-- gaps between zones -->
        <g v-if="mode === 'gaps'">
          <polygon v-for="(s, i) in gaps.flatMap(g => g.shapes)" :key="i" :points="poly(pl().back(s), px)" fill="url(#dm-hatch)" stroke="#1f2328"
            stroke-width="1.2" stroke-linejoin="round" data-testid="gap-shape" />
        </g>

        <!-- corners of other zones: snap targets -->
        <g v-if="mode === 'draw' || (mode === 'edit' && ed.polygon.length)">
          <template v-for="r in rings" :key="r.id">
            <template v-if="r.id !== ed.id || mode === 'draw'">
              <rect v-for="(p, i) in r.zone.polygon" :key="i" :x="px(p).x - 3.5" :y="px(p).y - 3.5" width="7" height="7" fill="#fff" :stroke="colorOf(r.zone)"
                stroke-width="1.5" :transform="`rotate(45 ${px(p).x} ${px(p).y})`" />
            </template>
          </template>
        </g>

        <!-- the zone being edited -->
        <g v-if="(mode === 'edit' || mode === 'overlap') && ed.polygon.length >= 3" data-testid="editing">
          <polygon :points="poly(edShapes[0], px)" :fill="edColor" fill-opacity="0.3" :stroke="edColor" stroke-width="3" stroke-linejoin="round" data-testid="ed-shape" />
          <polygon v-for="(piece, k) in edShapes.slice(1)" :key="'x' + k" :points="poly(piece, px)" :fill="split === 'keep' ? edColor : '#b42318'" fill-opacity="0.12"
            :stroke="split === 'keep' ? edColor : '#b42318'" stroke-width="2" stroke-dasharray="6 5" stroke-linejoin="round" data-testid="split-piece" />
          <!-- the outline as drawn, where the cut took something away from it -->
          <polygon v-if="edShapes[0] !== ed.polygon" :points="poly(ed.polygon, px)" fill="none" :stroke="edColor" stroke-width="1.5" stroke-dasharray="3 5" opacity="0.7" />
          <polygon v-if="overlapZones.length && !isCut" :points="poly(ed.polygon, px)" fill="url(#dm-hatch)" fill-opacity="0.75" clip-path="url(#dm-ov)" data-testid="overlap-hatch" />
          <template v-for="(p, i) in ed.polygon" :key="'e' + i">
            <line v-if="badEdges.includes(i)" :x1="px(p).x" :y1="px(p).y" :x2="px(ed.polygon[(i + 1) % ed.polygon.length]).x"
              :y2="px(ed.polygon[(i + 1) % ed.polygon.length]).y" stroke="#b42318" stroke-width="4" />
          </template>
          <template v-if="mode === 'edit' && !narrow">
            <circle v-for="(p, i) in ed.polygon" :key="'m' + i" class="grab" :cx="px(mid(p, ed.polygon[(i + 1) % ed.polygon.length])).x"
              :cy="px(mid(p, ed.polygon[(i + 1) % ed.polygon.length])).y" r="4.5" fill="#fff" fill-opacity="0.75" :stroke="edColor" stroke-width="1.5"
              :data-testid="'mid-' + i" @pointerdown="addVertex(i, $event)" />
            <circle v-for="(p, i) in ed.polygon" :key="'v' + i" class="grab" :cx="px(p).x" :cy="px(p).y" r="6.5" fill="#fff" :stroke="edColor" stroke-width="2.5"
              :data-testid="'vertex-' + i" @pointerdown="dragVertex(i, $event)" @contextmenu.prevent="removeVertex(i)" />
          </template>
        </g>

        <!-- outline in progress -->
        <g v-if="mode === 'draw' && pts.length" data-testid="drawing">
          <polygon v-if="hover" :points="poly([...pts, hover], px)" :fill="COLORS[3]" fill-opacity="0.1" />
          <polyline :points="poly(pts, px)" fill="none" :stroke="COLORS[3]" stroke-width="3" stroke-linejoin="round" />
          <polyline v-if="hover" :points="poly([pts[pts.length - 1], hover, pts[0]], px)" fill="none" :stroke="COLORS[3]" stroke-width="2" stroke-dasharray="7 6" />
          <template v-for="(p, i) in pts" :key="i">
            <line v-if="badEdges.includes(i)" :x1="px(p).x" :y1="px(p).y" :x2="px(pts[(i + 1) % pts.length]).x" :y2="px(pts[(i + 1) % pts.length]).y" stroke="#b42318" stroke-width="4" />
            <circle :cx="px(p).x" :cy="px(p).y" :r="i ? 6 : 9" :fill="i ? '#fff' : COLORS[3]" :stroke="i ? COLORS[3] : '#fff'" stroke-width="2.5" />
            <text :x="px(p).x + (i ? 10 : -14)" :y="px(p).y - 10" font-size="11.5" font-weight="700" :fill="COLORS[3]" stroke="#fff" stroke-width="3"
              paint-order="stroke" :text-anchor="i ? 'start' : 'end'">{{ i + 1 }}</text>
          </template>
          <g v-if="pts.length >= 3" :transform="`translate(${px(pts[0]).x} ${px(pts[0]).y + 30})`">
            <rect :x="-textW(t('mz.close_here'), 12) / 2 - 8" y="-12" :width="textW(t('mz.close_here'), 12) + 16" height="24" rx="6" fill="#1f2328" />
            <text text-anchor="middle" y="4.5" font-size="12" fill="#fff">{{ t('mz.close_here') }}</text>
          </g>
        </g>
        <g v-if="snapMsg && (mode === 'draw' ? hover : true) && mode !== 'browse'">
          <circle v-if="mode === 'draw' && hover" :cx="px(hover).x" :cy="px(hover).y" r="16" fill="#ffd43b" fill-opacity="0.35" stroke="#e0a100" stroke-width="2" data-testid="snap-ring" />
        </g>

        <!-- names -->
        <template v-if="mode === 'browse' || mode === 'gaps'">
          <g v-for="z in drawn" :key="'l' + z.id" :opacity="z.active ? 1 : 0.6" :transform="`translate(${labelAt(z.polygon, px).x} ${labelAt(z.polygon, px).y})`">
            <rect :x="-Math.max(textW(zname(z), 12.5), textW(feeLine(z), 11.5)) / 2 - 10" y="-21" :width="Math.max(textW(zname(z), 12.5), textW(feeLine(z), 11.5)) + 20"
              height="40" rx="8" fill="#fff" :stroke="colorOf(z)" stroke-width="1.5" />
            <text text-anchor="middle" y="-4" font-size="12.5" font-weight="700" :fill="colorOf(z)">{{ zname(z) }}</text>
            <text text-anchor="middle" y="12" font-size="11.5" fill="#3f4650">{{ feeLine(z) }}</text>
          </g>
        </template>

        <!-- the store -->
        <g v-if="store && mode !== 'location'" :transform="`translate(${px(store).x} ${px(store).y})`" data-testid="store-pin">
          <path d="M0 0C-4 -12 -15 -17 -15 -29A15 15 0 1 1 15 -29C15 -17 4 -12 0 0Z" fill="#d9480f" stroke="#fff" stroke-width="2" />
          <path d="M-8 -31h16l-2 -6h-12zM-7 -30v8h14v-8M-2 -22v-5h4v5" fill="none" stroke="#fff" stroke-width="1.8" stroke-linejoin="round" />
          <text y="18" text-anchor="middle" font-size="12.5" font-weight="700" fill="#b93a08" stroke="#fff" stroke-width="3.5" paint-order="stroke">{{ pick(branch.name, branch.name_en || branch.name) }}</text>
        </g>
        <g v-if="mode === 'location'">
          <circle v-if="store" :cx="px(store).x" :cy="px(store).y" r="6" fill="#fff" stroke="#9aa0a6" stroke-width="2" stroke-dasharray="3 2" />
          <line v-if="store" :x1="px(store).x" :y1="px(store).y" :x2="px(pin).x" :y2="px(pin).y" stroke="#9aa0a6" stroke-width="1.5" stroke-dasharray="4 3" />
          <g class="grab" :transform="`translate(${px(pin).x} ${px(pin).y})`" data-testid="loc-pin" @pointerdown="map!.drag($event, setPin)">
            <ellipse cx="0" cy="2" rx="12" ry="4" fill="#000" opacity="0.22" />
            <path d="M0 0C-4 -12 -15 -17 -15 -29A15 15 0 1 1 15 -29C15 -17 4 -12 0 0Z" fill="#d9480f" stroke="#fff" stroke-width="2" />
            <path d="M-8 -31h16l-2 -6h-12zM-7 -30v8h14v-8M-2 -22v-5h4v5" fill="none" stroke="#fff" stroke-width="1.8" stroke-linejoin="round" />
          </g>
        </g>
      </template>

      <template #ui>
        <div class="mapc-float dmap-search">
          <form @submit.prevent="search"><span>🔍</span><input v-model="q" :placeholder="t('mz.search')" data-testid="map-search" /></form>
          <div v-if="hits" class="hits">
            <div v-for="(h, i) in hits" :key="i" @click="go(h)">{{ h.label }}</div>
            <div v-if="!hits.length" class="muted">{{ t('mz.no_hits') }}</div>
          </div>
        </div>
        <div v-if="!readOnly" class="mapc-float dmap-tools">
          <button :class="{ on: mode === 'browse' || mode === 'gaps' }" data-testid="tool-browse" @click="cancelEdit()">✋ {{ t('mz.browse') }}</button>
          <button class="wide" :class="{ on: ['draw', 'overlap', 'edit'].includes(mode) }" :disabled="!store" data-testid="tool-draw" @click="startDraw()">✏️ {{ t('mz.draw') }}</button>
          <button class="wide" :class="{ on: mode === 'circle' }" :disabled="!store" data-testid="tool-circle" @click="startCircle()">◯ {{ t('mz.circle') }}</button>
          <button :class="{ on: mode === 'location' }" data-testid="tool-location" @click="startLocation()">📍 {{ t('mz.store_loc') }}</button>
        </div>
        <div v-if="map?.adapter?.kind === 'google'" class="mapc-float dmap-type">
          <button :class="{ on: mapType === 'roadmap' }" @click="mapType = 'roadmap'; map!.setType('roadmap')">{{ t('mz.map') }}</button>
          <button :class="{ on: mapType === 'satellite' }" @click="mapType = 'satellite'; map!.setType('satellite')">{{ t('mz.satellite') }}</button>
        </div>
        <div v-if="mode === 'draw'" class="mapc-float dmap-hint">
          <template v-if="snapMsg">{{ snapMsg }}</template>
          <template v-else>{{ t('mz.hint_draw') }} <kbd>Backspace</kbd> {{ t('mz.hint_undo') }} <kbd>Esc</kbd> {{ t('mz.hint_cancel') }}</template>
        </div>
        <div v-else-if="snapMsg || hint" class="mapc-float dmap-hint">{{ snapMsg || hint }}</div>

        <template v-if="mode === 'overlap'">
          <div class="dmap-scrim"></div>
          <div class="mapc-float dmap-dialog" data-testid="overlap-dialog">
            <h2>{{ t(fresh && !ed.id ? 'mz.ov_title' : 'mz.ov_title_edit', { names: overlapNames }) }}</h2>
            <p class="muted">{{ t('mz.ov_sub', { area: t('mz.about', { v: geo.fmtArea(overlapM2) }) }) }}</p>
            <div class="dmap-choices">
              <button type="button" class="dmap-choice" :class="{ on: choice === 'new_over' }" :disabled="!canChoose.new_over" data-testid="choose-new" @click="choose('new_over')">
                <svg viewBox="0 0 190 70"><rect x="20" y="10" width="92" height="48" rx="6" fill="#2a78d6" fill-opacity=".35" /><rect x="78" y="24" width="92" height="38" rx="6" :fill="edColor" fill-opacity=".9" />
                  <text x="34" y="28" font-size="11" fill="#fff" font-weight="700">{{ t('mz.old') }}</text><text x="146" y="54" font-size="11" fill="#fff" font-weight="700">{{ t('mz.new_s') }}</text></svg>
                <b>{{ fresh ? t('mz.new_over') : t('mz.this_over', { names: overlapNames }) }}</b>
                <div class="small muted">{{ canChoose.new_over ? t('mz.new_over_d') : t('mz.gone_other', { names: overlapNames }) }}</div>
              </button>
              <button type="button" class="dmap-choice" :class="{ on: choice === 'old_over' }" :disabled="!canChoose.old_over" data-testid="choose-old" @click="choose('old_over')">
                <svg viewBox="0 0 190 70"><rect x="78" y="24" width="92" height="38" rx="6" :fill="edColor" fill-opacity=".35" /><rect x="20" y="10" width="92" height="48" rx="6" fill="#2a78d6" fill-opacity=".9" />
                  <text x="34" y="28" font-size="11" fill="#fff" font-weight="700">{{ t('mz.old') }}</text><text x="146" y="54" font-size="11" fill="#fff" font-weight="700">{{ t('mz.new_s') }}</text></svg>
                <b>{{ fresh ? t('mz.old_over') : t('mz.other_over', { names: overlapNames }) }}</b>
                <div class="small muted">{{ canChoose.old_over ? t('mz.old_over_d', { names: overlapNames }) : t('mz.gone_this') }}</div>
              </button>
            </div>
            <div class="row"><button type="button" class="btn" data-testid="overlap-back" @click="mode = 'edit'">{{ t('mz.back_adjust') }}</button><span class="grow"></span>
              <button type="button" class="btn primary" :disabled="!choice" data-testid="overlap-ok" @click="confirmChoice">{{ t('mz.ov_continue') }}</button></div>
          </div>
        </template>
      </template>
    </MapCanvas>

    <aside class="dmap-panel">
      <section v-if="readOnly"><div class="banner info">{{ t('mz.read_only') }}</div></section>
      <section v-if="error"><div class="banner bad" data-testid="map-error">{{ error }}</div></section>
      <section v-if="notice"><div class="banner ok row between" data-testid="map-notice"><span>{{ notice }}</span>
        <button v-if="canUndo && mode === 'gaps'" class="btn small" data-testid="undo-close" @click="undoClose">{{ t('mz.undo_close') }}</button>
        <button v-else-if="cutUndo && mode === 'browse'" class="btn small" data-testid="undo-cut" @click="undoCut">{{ t('mz.undo_close') }}</button>
        <button v-else-if="gaps.length && mode === 'browse'" class="btn small" @click="openGaps">{{ t('mz.view') }}</button></div></section>

      <!-- browse -->
      <template v-if="mode === 'browse'">
        <section v-if="narrow && !readOnly"><div class="banner info">{{ t('mz.narrow') }}</div></section>
        <section>
          <div class="row between"><h2>{{ t('mz.store') }}</h2><button v-if="!readOnly" class="btn small" @click="startLocation">{{ t('mz.change_loc') }}</button></div>
          <dl class="kv"><dt>{{ t('address') }}</dt><dd>{{ branch.address || '—' }}</dd>
            <template v-if="store"><dt>{{ t('mz.region') }}</dt>
              <dd v-if="branch.country_code" data-testid="store-region">{{ regionText }}</dd>
              <dd v-else class="muted" data-testid="store-region-unknown">{{ t(resolving ? 'mz.region_wait' : 'mz.region_unknown') }} <button v-if="!resolving && !readOnly" class="btn link" @click="fillRegion">{{ t('retry') }}</button></dd></template>
            <dt>{{ t('mz.latlng') }}</dt><dd data-testid="store-latlng">{{ store ? store[0].toFixed(5) + ', ' + store[1].toFixed(5) : t('mz.not_set') }}</dd></dl>
        </section>
        <section>
          <div class="row between" style="margin-bottom:8px"><h2 style="margin:0">{{ t('mz.zones') }} · {{ zones.length }}</h2>
            <button v-if="!readOnly && !narrow" class="btn small primary" :disabled="!store" data-testid="new-zone" @click="startDraw()">{{ t('mz.new') }}</button></div>
          <p v-if="!zones.length" class="muted small">{{ t('mz.no_zones') }}</p>
          <div v-for="(z, i) in zones" :key="z.id" class="dmap-zone" :class="{ sel: selected === z.id, over: overRow === i }" :data-testid="'zone-row-' + i" :data-zone-name="z.name"
            :draggable="!readOnly" @dragstart="dragRow = i" @dragover.prevent="overRow = i" @dragleave="overRow = -1" @drop.prevent="dropRow(i)"
            @click="selected = z.id; z.polygon.length >= 3 && map?.fit(z.polygon, 16)">
            <span class="ord"><button :disabled="readOnly || i === 0" :data-testid="'up-' + i" @click.stop="nudge(i, -1)">▲</button>
              <button :disabled="readOnly || i === zones.length - 1" :data-testid="'down-' + i" @click.stop="nudge(i, 1)">▼</button></span>
            <span class="dot" :style="{ background: colorOf(z) }"></span>
            <div><div class="bold">{{ zname(z) }} <span class="tag" :class="z.active ? 'ok' : ''">{{ z.active ? t('mz.on') : t('mz.off') }}</span>
                <span v-if="z.polygon.length < 3" class="tag warn" style="margin-left:4px">{{ t('mz.undrawn') }}</span></div>
              <div class="small muted">{{ t('mz.fee') }} {{ money(z.fee, cur) }} · {{ t('mz.min') }} {{ money(z.min_order, cur) }}<template v-if="z.eta_minutes"> · {{ t('mz.eta', { n: z.eta_minutes }) }}</template></div></div>
            <button v-if="!readOnly" class="btn small" :data-testid="'edit-' + i" @click.stop="editZone(z)">{{ z.polygon.length < 3 && !narrow ? t('mz.go_draw') : t('mz.edit') }}</button>
          </div>
          <p v-if="zones.length > 1" class="small muted">{{ t('mz.order_hint') }}</p>
        </section>
        <section>
          <h2>{{ t('mz.how') }}</h2>
          <div class="seg"><button :class="{ on: branch.zone_mode === 'map' }" :disabled="readOnly" data-testid="mode-map" @click="setMode('map')">{{ t('mz.mode_map') }}</button>
            <button :class="{ on: branch.zone_mode === 'list' }" :disabled="readOnly" data-testid="mode-list" @click="setMode('list')">{{ t('mz.mode_list') }}</button></div>
          <p class="small muted" style="margin-top:8px">{{ t(branch.zone_mode === 'map' ? 'mz.mode_map_hint' : 'mz.mode_list_hint') }}</p>
          <div class="row between" style="margin-top:10px"><span>{{ t('mz.tolerance') }}</span>
            <span class="row" style="gap:6px"><b>{{ t('mz.metres', { n: branch.zone_tolerance_m }) }}</b><button class="btn small" data-testid="open-gaps" @click="openGaps">{{ t('mz.view') }}</button></span></div>
          <p class="small muted" style="margin-top:4px">{{ branch.zone_tolerance_m ? t('mz.tolerance_hint', { n: branch.zone_tolerance_m }) : t('mz.tolerance_zero') }}</p>
        </section>
        <section v-if="zones.length">
          <div v-if="undrawnCount" class="banner warn">{{ t('mz.some_undrawn', { n: undrawnCount }) }}</div>
          <div v-else class="banner ok">{{ t('mz.all_drawn', { n: zones.length }) }}</div>
          <div v-if="overlapPairs.length" class="banner warn" style="margin-top:8px" data-testid="overlap-pairs">
            <div>{{ t('mz.pairs_found', { n: overlapPairs.length }) }}</div>
            <div v-for="(p, i) in overlapPairs" :key="i" class="row between" style="margin-top:6px"><span>{{ zname(p.a) }} ↔ {{ zname(p.b) }}</span>
              <button v-if="!readOnly && !narrow" class="btn small" :data-testid="'resolve-' + i" @click="resolveOverlap(p)">{{ t('mz.resolve') }}</button></div>
          </div>
          <div v-if="gaps.length" class="banner warn row between" style="margin-top:8px"><span data-testid="gap-count">{{ t('mz.gaps_found', { n: gaps.length }) }}</span>
            <button class="btn small" @click="openGaps">{{ t('mz.view') }}</button></div>
        </section>
      </template>

      <!-- drawing -->
      <template v-else-if="mode === 'draw'">
        <section><h2>{{ drawFor ? t('mz.drawing_for', { name: zname(drawFor) }) : t('mz.drawing') }}</h2>
          <dl class="kv"><dt>{{ t('mz.points') }}</dt><dd data-testid="point-count">{{ t('mz.n_points', { n: pts.length }) }}</dd>
            <template v-if="pts.length >= 3"><dt>{{ t('mz.area') }}</dt><dd>{{ t('mz.about', { v: geo.fmtArea(geo.area(pl().ring(pts))) }) }}</dd>
              <template v-if="store"><dt>{{ t('mz.reach') }}</dt><dd>{{ geo.fmtDist(geo.maxReach(pl().to(store), pl().ring(pts))) }}</dd></template></template></dl></section>
        <section><div class="row between"><span>{{ t('mz.snap') }}</span><button class="switch" :class="{ on: snapOn }" data-testid="snap-switch" @click="snapOn = !snapOn"></button></div>
          <p class="small muted" style="margin-top:6px">{{ t('mz.snap_hint') }}</p></section>
        <section><p class="muted small" style="margin-bottom:8px">{{ t('mz.how_draw') }}</p>
          <ol style="margin:0;padding-left:18px;display:grid;gap:6px"><li>{{ t('mz.step1') }}</li><li>{{ t('mz.step2') }}</li><li>{{ t('mz.step3') }}</li><li>{{ t('mz.step4') }}</li></ol></section>
        <div class="foot"><button class="btn" :disabled="!pts.length" data-testid="undo-point" @click="undoPoint">{{ t('mz.undo') }}</button>
          <button class="btn" data-testid="cancel-draw" @click="cancelDraw">{{ t('mz.cancel') }}</button>
          <button class="btn primary grow" :disabled="pts.length < 3" data-testid="finish-draw" @click="finishShape">{{ t('mz.finish') }}</button></div>
      </template>

      <!-- circle -->
      <template v-else-if="mode === 'circle'">
        <section><h2>{{ t('mz.circle') }}</h2><p class="small muted" style="margin-bottom:10px">{{ t('mz.circle_hint') }}</p>
          <label class="field"><span>{{ t('mz.radius') }}</span><input type="number" min="0.1" max="30" step="0.1" v-model.number="radiusKm" data-testid="radius" /></label></section>
        <div class="foot"><button class="btn" @click="cancelDraw">{{ t('mz.cancel') }}</button><button class="btn primary grow" data-testid="make-circle" @click="makeCircle">{{ t('mz.generate') }}</button></div>
      </template>

      <!-- overlap dialog is open -->
      <template v-else-if="mode === 'overlap'">
        <section><h2>{{ ed.name || t('mz.new_zone') }}</h2>
          <dl class="kv"><dt>{{ t('mz.vertices') }}</dt><dd>{{ edStats.n }}</dd><dt>{{ t('mz.area') }}</dt><dd>{{ t('mz.about', { v: geo.fmtArea(edStats.area) }) }}</dd>
            <dt>{{ t('mz.overlap') }}</dt><dd>{{ t('mz.overlap_n', { n: overlapZones.length, area: t('mz.about', { v: geo.fmtArea(overlapM2) }) }) }}</dd></dl></section>
        <section><div v-for="z in overlapZones" :key="z.id" class="row" style="margin-bottom:6px"><span class="dot" :style="{ background: colorOf(z) }"></span>
          <span class="grow">{{ zname(z) }}</span><span class="small muted">{{ feeLine(z) }}</span></div></section>
      </template>

      <!-- zone form -->
      <template v-else-if="mode === 'edit'">
        <section><div class="row between"><h2>{{ ed.id ? ed.name : t('mz.new_zone') }}</h2><span v-if="fresh" class="tag warn">{{ t('mz.unsaved') }}</span></div>
          <div class="two"><label class="field"><span>{{ t('mz.name') }}</span><input v-model="ed.name" data-testid="zone-name" /></label>
            <label class="field"><span>{{ t('mz.name_en') }}</span><input v-model="ed.name_en" data-testid="zone-name-en" /></label></div>
          <div class="two"><label class="field"><span>{{ t('mz.fee_f', { c: cur }) }}</span><input type="number" min="0" step="0.01" v-model.number="ed.fee" data-testid="zone-fee" /></label>
            <label class="field"><span>{{ t('mz.min_f', { c: cur }) }}</span><input type="number" min="0" step="0.01" v-model.number="ed.min_order" data-testid="zone-min" /></label></div>
          <div class="two"><label class="field"><span>{{ t('mz.eta_f') }}</span><input type="number" min="1" max="600" v-model="ed.eta" data-testid="zone-eta" /></label>
            <div class="field"><span class="small muted" style="display:block;margin-bottom:4px">{{ t('mz.color') }}</span><div class="swatches">
              <button v-for="c in COLORS" :key="c" type="button" :class="{ on: ed.color === c }" :style="{ background: c }" :aria-label="c" @click="ed.color = c"></button></div></div></div>
          <div class="row between"><span>{{ t('mz.enable') }}</span><button type="button" class="switch" :class="{ on: ed.active }" data-testid="zone-active" @click="ed.active = !ed.active"></button></div></section>
        <section v-if="ed.polygon.length">
          <dl class="kv"><dt>{{ t('mz.vertices') }}</dt><dd data-testid="vertex-count">{{ edStats.n }}</dd><dt>{{ t('mz.area') }}</dt><dd>{{ t('mz.about', { v: geo.fmtArea(edStats.area) }) }}</dd>
            <template v-if="store"><dt>{{ t('mz.reach') }}</dt><dd>{{ geo.fmtDist(edStats.reach) }}</dd></template></dl>
          <div v-if="edStats.area < 10000" class="banner warn" style="margin-top:8px">{{ t('mz.small_area') }}</div>
        </section>
        <section v-else><div class="banner warn">{{ t('mz.undrawn') }}</div></section>
        <section v-if="hasSplit" data-testid="split-choice">
          <div class="banner warn" style="margin-bottom:8px">{{ t('mz.split') }}</div>
          <label class="row" style="gap:8px;margin-bottom:6px"><input type="radio" value="keep" v-model="split" style="width:auto" data-testid="split-keep" /><span>{{ t('mz.split_keep') }}</span></label>
          <label class="row" style="gap:8px"><input type="radio" value="drop" v-model="split" style="width:auto" data-testid="split-drop" /><span>{{ t('mz.split_drop') }}</span></label>
        </section>
        <section v-if="overlapZones.length"><div class="banner warn" data-testid="overlap-banner" :data-cut="isCut ? 'yes' : 'no'">
          {{ t(!isCut ? 'mz.ov_banner_order' : place === 'new_over' ? 'mz.ov_banner_new' : 'mz.ov_banner_old', { names: overlapNames }) }}
          <button type="button" class="btn link" style="font-weight:700" data-testid="rechoose" @click="rechoose">{{ t('mz.rechoose') }}</button></div></section>
        <div class="foot"><button v-if="ed.id" class="btn danger" data-testid="delete-zone" @click="removeZone">{{ t('mz.delete') }}</button>
          <button v-if="!narrow" class="btn" data-testid="redraw" @click="redraw">{{ t('mz.redraw') }}</button>
          <button class="btn" @click="cancelEdit">{{ t('mz.cancel') }}</button>
          <button class="btn primary grow" data-testid="save-zone" @click="saveZone">{{ t('mz.save_zone') }}</button></div>
      </template>

      <!-- store location -->
      <template v-else-if="mode === 'location'">
        <section><h2>{{ t('mz.loc_title') }}</h2>
          <div v-if="!store" class="banner info" style="margin-bottom:10px">{{ t('mz.loc_first') }}</div>
          <p class="small muted" style="margin-bottom:10px">{{ t('mz.loc_hint') }}</p>
          <label class="field"><span>{{ t('mz.address') }}</span><input v-model="loc.address" data-testid="loc-address" /></label>
          <div class="two"><label class="field"><span>{{ t('mz.lat') }}</span><input type="number" step="0.000001" v-model.number="loc.lat" data-testid="loc-lat" /></label>
            <label class="field"><span>{{ t('mz.lng') }}</span><input type="number" step="0.000001" v-model.number="loc.lng" data-testid="loc-lng" /></label></div>
          <div v-if="store && movedM >= 1" class="banner info">{{ t('mz.moved', { d: geo.fmtDist(movedM) }) }}</div></section>
        <section v-if="drawn.length"><div class="banner warn">{{ t('mz.loc_note') }}</div></section>
        <div class="foot"><button v-if="store" class="btn" @click="cancelEdit">{{ t('mz.cancel') }}</button>
          <button class="btn primary grow" data-testid="save-location" @click="saveLocation">{{ t('mz.save_loc') }}</button></div>
      </template>

      <!-- gaps and tolerance -->
      <template v-else-if="mode === 'gaps'">
        <section><h2>{{ t('mz.gaps') }}</h2>
          <div v-if="!gaps.length" class="banner ok" data-testid="no-gaps">{{ t('mz.gaps_none') }}</div>
          <template v-else>
            <div class="banner warn" style="margin-bottom:10px" data-testid="gaps-banner">{{ t(gapsWide ? 'mz.gaps_out' : 'mz.gaps_in', { n: gaps.length, t: branch.zone_tolerance_m }) }}</div>
            <div v-for="(g, i) in gaps" :key="i" class="dmap-zone" style="grid-template-columns:minmax(0,1fr) auto;cursor:default" :data-testid="'gap-' + i">
              <div><div class="bold">{{ zname(zoneById(g.a)) }} ↔ {{ zname(zoneById(g.b)) }}</div>
                <div class="small muted">{{ t('mz.gap_size', { w: geo.fmtDist(g.width), l: geo.fmtDist(g.length) }) }}</div></div>
              <div class="row" style="gap:6px"><button class="btn small" @click="showGap(g)">{{ t('mz.locate') }}</button>
                <button v-if="!readOnly" class="btn small primary" :data-testid="'close-gap-' + i" @click="closeGap(g)">{{ t('mz.close_gap') }}</button></div>
            </div>
            <p class="small muted">{{ t('mz.close_hint') }}</p>
          </template></section>
        <section><h2>{{ t('mz.tolerance') }}</h2>
          <div class="two"><label class="field" style="margin:0"><span>{{ t('mz.tol_f') }}</span><input type="number" min="0" max="500" step="10" v-model.number="tol" :disabled="readOnly" data-testid="tolerance" /></label>
            <div class="small muted" style="align-self:end">{{ t('mz.tol_zero') }}</div></div>
          <p class="small muted" style="margin-top:8px">{{ t('mz.tol_note') }}</p></section>
        <div class="foot"><button class="btn grow" data-testid="gaps-back" @click="cancelEdit">{{ t('mz.back') }}</button>
          <button v-if="!readOnly" class="btn primary grow" data-testid="save-tolerance" @click="saveTolerance">{{ t('mz.save') }}</button></div>
      </template>
    </aside>
  </div>
</template>
