// A rider's shift and position. Positions are only read while the rider is
// on duty and the rider identity is in front; they are sent in small batches
// and kept on the phone while there is no signal.
import { reactive } from 'vue'
import { ApiError, get, post } from '../../api'
import { native, nativePost, plugin } from './native'

export interface Point { lat: number; lng: number; accuracy?: number; battery?: number; at: string }
type Geo = 'idle' | 'ok' | 'denied' | 'unsupported' | 'searching'

export const duty = reactive({ loaded: false, on: false, since: null as string | null, geo: 'idle' as Geo,
  sentAt: null as string | null, queued: 0 })

const KEY = 'work-points'
const MAX_AGE = 10 * 60 * 1000
const read = (): Point[] => { try { return JSON.parse(localStorage.getItem(KEY) || '[]') } catch { return [] } }
function write(points: Point[]) {
  const fresh = points.filter(p => Date.now() - Date.parse(p.at) < MAX_AGE).slice(-100)
  duty.queued = fresh.length
  if (fresh.length) localStorage.setItem(KEY, JSON.stringify(fresh)); else localStorage.removeItem(KEY)
}

function apply(s: { on_duty: boolean; duty_since: string | null; located_at?: string | null }) {
  duty.on = s.on_duty; duty.since = s.duty_since; duty.loaded = true
  if (!s.on_duty) { duty.sentAt = null; write([]) }
}
export async function loadDuty() { try { apply(await get('/rider/duty')) } catch { /* keep what we know */ } }
export async function setDuty(on: boolean) { apply(await post('/rider/duty', { on_duty: on })) }

let sending = false
export async function flush() {
  const points = read()
  if (sending || !points.length || !duty.on) return
  sending = true
  try {
    if (!(await nativePost('/rider/locations', { points }))) await post('/rider/locations', { points })
    duty.sentAt = points[points.length - 1].at
    // Fixes taken while the request was in flight stay queued.
    write(read().filter(p => !points.some(q => q.at === p.at)))
  } catch (e) {
    const code = e instanceof ApiError ? e.code : ''
    if (code === 'OFF_DUTY') { duty.on = false; duty.since = null; write([]) }
    else if (code !== 'NETWORK' && (e as ApiError).status < 500) write([]) // the server will never take these
  }
  sending = false
}

// metres between two fixes, good enough over a few hundred metres
function apart(a: Point, b: Point) {
  const k = Math.PI / 180, x = (b.lng - a.lng) * k * Math.cos((a.lat + b.lat) * k / 2), y = (b.lat - a.lat) * k
  return Math.sqrt(x * x + y * y) * 6371000
}

let watch: number | null = null
let last: Point | null = null
let battery: number | undefined
let busy: () => boolean = () => false

function onFix(c: { latitude: number; longitude: number; accuracy: number }, time: number) {
  duty.geo = 'ok'
  const p: Point = { lat: c.latitude, lng: c.longitude, accuracy: Math.round(c.accuracy),
    battery, at: new Date(Math.min(time || Date.now(), Date.now())).toISOString() }
  // Every 15 s with a delivery in hand, every minute otherwise; a rider who
  // stands still is reported once in five minutes.
  const gap = last ? Date.parse(p.at) - Date.parse(last.at) : Infinity
  const still = last ? apart(last, p) < 20 : false
  if (gap < (busy() ? 15000 : 60000) || (still && gap < 5 * 60000)) return
  last = p
  write([...read(), p])
  void flush()
}

// In the app the phone itself keeps reading the position, with a notice in
// the status bar, also while the screen is off.
let nativeWatch: Promise<string> | null = null
async function trackNative(on: boolean) {
  const geo = await plugin('BackgroundGeolocation')
  if (!on) {
    const id = nativeWatch; nativeWatch = null
    if (id) await geo.removeWatcher({ id: await id }).catch(() => {})
    return
  }
  if (nativeWatch) return
  nativeWatch = geo.addWatcher({ backgroundTitle: '员工工作台 · 上班中', backgroundMessage: '正在上报位置，下班后停止 Sharing your position while on duty',
    requestPermissions: true, stale: false, distanceFilter: 15 }, (loc: any, err: any) => {
    if (err) { duty.geo = err.code === 'NOT_AUTHORIZED' ? 'denied' : 'searching'; return }
    if (loc) onFix(loc, loc.time)
  })
}
// Opens the phone's settings page of the app (location permission, battery).
export async function openAppSettings() { if (native) await (await plugin('BackgroundGeolocation')).openSettings().catch(() => {}) }

// track starts or stops reading the position. hasDeliveries tells how often.
export function track(on: boolean, hasDeliveries: () => boolean = () => false) {
  busy = hasDeliveries
  if (native) {
    if (on && !nativeWatch) duty.geo = 'searching'
    if (!on) { last = null; if (duty.geo === 'ok' || duty.geo === 'searching') duty.geo = 'idle' }
    void trackNative(on).catch(() => { duty.geo = 'unsupported' })
    return
  }
  if (!on) {
    if (watch !== null) navigator.geolocation.clearWatch(watch)
    watch = null; last = null
    if (duty.geo === 'ok' || duty.geo === 'searching') duty.geo = 'idle'
    return
  }
  if (watch !== null) return
  if (!('geolocation' in navigator)) { duty.geo = 'unsupported'; return }
  duty.geo = 'searching'
  ;(navigator as any).getBattery?.().then((b: any) => {
    const set = () => (battery = Math.round(b.level * 100)); set(); b.addEventListener('levelchange', set)
  }).catch(() => {})
  watch = navigator.geolocation.watchPosition(pos => onFix(pos.coords, pos.timestamp),
    err => { duty.geo = err.code === err.PERMISSION_DENIED ? 'denied' : 'searching' },
    { enableHighAccuracy: true, maximumAge: 10000, timeout: 30000 })
}

duty.queued = read().length
window.addEventListener('online', () => void flush())
