// The workbench as an installable page: a manifest, a service worker that
// keeps the page openable without signal, and web push for new deliveries.
import { reactive } from 'vue'
import { del, get, post } from '../../api'
import { appPush, native, platform, plugin } from './native'

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
  if (native) { await setupNative(); return }
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

// ---- inside the app: the phone's own push service ----
let listening = false
async function setupNative() {
  try {
    // The server must hold the Firebase key too, or nothing would ever arrive.
    push.supported = appPush && !!(await get<{ app_push?: boolean }>('/config')).app_push
    if (!push.supported) return
    const pn = await plugin('PushNotifications')
    const perm = await pn.checkPermissions()
    push.denied = perm.receive === 'denied'
    push.on = !!localStorage.getItem(KEY) && perm.receive === 'granted'
    if (push.on) await registerNative() // the token can change between launches
  } catch { push.supported = false }
}
async function registerNative() {
  const pn = await plugin('PushNotifications')
  if (!listening) {
    listening = true
    await pn.addListener('registration', async (t: { value: string }) => {
      try {
        const d = await post<{ id: number }>('/rider/devices', { platform, token: t.value, app_version: 'app' })
        localStorage.setItem(KEY, String(d.id)); push.on = true
      } catch { /* tried again at the next launch */ }
    })
    await pn.addListener('pushNotificationActionPerformed', () => { if (!location.pathname.startsWith('/work')) location.assign('/work') })
  }
  if (platform === 'android') {
    await pn.createChannel({ id: 'deliveries', name: '新配送单 New deliveries', description: '新配送单、改派、取消、可以取餐',
      importance: 5, visibility: 1, vibration: true, lights: true }).catch(() => {})
  }
  await pn.register()
}
// askNotifications is called when a rider clocks on in the app: Android needs
// the permission to show the "sharing position" notice, and it is the moment
// a rider expects to be asked about reminders.
export async function askNotifications() {
  if (!native) return
  try {
    const pn = await plugin('PushNotifications')
    const perm = await pn.requestPermissions()
    push.denied = perm.receive === 'denied'
    if (perm.receive === 'granted' && push.supported && !push.on) await registerNative()
  } catch { /* the shift goes on without reminders */ }
}

export async function enablePush() {
  if (native) {
    push.busy = true
    try { await askNotifications() } finally { push.busy = false }
    return
  }
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
    if (native) { if (appPush) await (await plugin('PushNotifications')).unregister().catch(() => {}) }
    else await (await reg?.pushManager.getSubscription())?.unsubscribe()
    push.on = false
  } finally { push.busy = false }
}

// Signing out: the server forgets the device with the session; drop the
// browser's side too so the next person on this phone starts clean.
export async function forgetPush() {
  localStorage.removeItem(KEY)
  push.on = false
  try {
    if (native) { if (appPush && push.supported) await (await plugin('PushNotifications')).unregister() }
    else await (await reg?.pushManager.getSubscription())?.unsubscribe()
  } catch { /* nothing to undo */ }
}
