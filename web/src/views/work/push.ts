// The workbench as an installable page: a manifest, a service worker that
// keeps the page openable without signal, and web push for new deliveries.
import { reactive } from 'vue'
import { del, get, post } from '../../api'

export const push = reactive({ supported: false, on: false, denied: false, busy: false })
const KEY = 'work-push-device'
let pushKey = ''
let reg: ServiceWorkerRegistration | null = null

export async function setupWorkbench() {
  if (!document.querySelector('link[rel=manifest]')) {
    const link = document.createElement('link')
    link.rel = 'manifest'; link.href = '/manifest.webmanifest'
    document.head.appendChild(link)
  }
  if (!('serviceWorker' in navigator)) return
  try {
    reg = await navigator.serviceWorker.register('/sw.js')
    void saveShell()
    pushKey = (await get<{ push_key?: string }>('/config')).push_key || ''
    push.supported = !!pushKey && 'PushManager' in window && 'Notification' in window
    if (!push.supported) return
    push.denied = Notification.permission === 'denied'
    push.on = !!localStorage.getItem(KEY) && !!(await reg.pushManager.getSubscription())
  } catch { /* the page works without any of this */ }
}

// The first visit loads before the service worker is in charge, so hand it
// the page and the files this visit used. After that it keeps them itself.
async function saveShell() {
  try {
    await navigator.serviceWorker.ready
    const cache = await caches.open('work-shell-v1')
    const files = performance.getEntriesByType('resource').map(e => new URL(e.name))
      .filter(u => u.origin === location.origin && u.pathname.startsWith('/assets/')).map(u => u.pathname)
    await Promise.all([cache.add('/work'), ...[...new Set(files)].map(async f => (await cache.match(f)) || cache.add(f))])
  } catch { /* offline opening just will not work yet */ }
}

function keyBytes(k: string) {
  const raw = atob(k.replace(/-/g, '+').replace(/_/g, '/').padEnd(Math.ceil(k.length / 4) * 4, '='))
  return Uint8Array.from(raw, c => c.charCodeAt(0))
}

export async function enablePush() {
  if (!reg || !push.supported) return
  push.busy = true
  try {
    if (await Notification.requestPermission() !== 'granted') { push.denied = Notification.permission === 'denied'; return }
    const sub = await reg.pushManager.getSubscription() ??
      await reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: keyBytes(pushKey) })
    const d = await post<{ id: number }>('/rider/devices', { platform: 'web', token: JSON.stringify(sub), app_version: 'web' })
    localStorage.setItem(KEY, String(d.id))
    push.on = true
  } finally { push.busy = false }
}

export async function disablePush() {
  push.busy = true
  try {
    const id = localStorage.getItem(KEY)
    if (id) await del('/rider/devices/' + id).catch(() => {})
    localStorage.removeItem(KEY)
    await (await reg?.pushManager.getSubscription())?.unsubscribe()
    push.on = false
  } finally { push.busy = false }
}

// Signing out: the server forgets the device with the session; drop the
// browser's side too so the next person on this phone starts clean.
export async function forgetPush() {
  localStorage.removeItem(KEY)
  push.on = false
  try { await (await reg?.pushManager.getSubscription())?.unsubscribe() } catch { /* nothing to undo */ }
}
