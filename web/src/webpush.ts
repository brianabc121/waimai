// Web push for the chat pages: a customer who wants to hear about a store's
// answer, and a store's desk that wants to hear about a customer's message.
// The staff workbench has its own, fuller version (views/work/push.ts); both
// use the same service worker and so the same subscription.
import { reactive } from 'vue'
import { del, get, post } from './api'

export const chatPush = reactive({ supported: false, on: false, denied: false, busy: false })
const KEYS = { customer: 'chat-push-device', staff: 'work-push-device' }
let pushKey = ''
let reg: ServiceWorkerRegistration | null = null

function keyBytes(k: string) {
  const raw = atob(k.replace(/-/g, '+').replace(/_/g, '/').padEnd(Math.ceil(k.length / 4) * 4, '='))
  return Uint8Array.from(raw, c => c.charCodeAt(0))
}

// setupChatPush finds out whether this browser can be reminded and whether it
// already is. It never asks for permission by itself.
export async function setupChatPush(who: 'customer' | 'staff') {
  chatPush.supported = chatPush.on = false
  if (!('serviceWorker' in navigator) || !('PushManager' in window) || !('Notification' in window)) return
  try {
    pushKey = (await get<{ push_key?: string }>('/config')).push_key || ''
    if (!pushKey) return
    reg = await navigator.serviceWorker.register('/sw.js')
    chatPush.supported = true
    chatPush.denied = Notification.permission === 'denied'
    chatPush.on = !!localStorage.getItem(KEYS[who]) && !!(await reg.pushManager.getSubscription())
  } catch { /* the page works without reminders */ }
}

export async function enableChatPush(who: 'customer' | 'staff') {
  if (!reg || !chatPush.supported) return
  chatPush.busy = true
  try {
    if (await Notification.requestPermission() !== 'granted') { chatPush.denied = Notification.permission === 'denied'; return }
    const sub = await reg.pushManager.getSubscription() ??
      await reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: keyBytes(pushKey) })
    const d = who === 'customer' ? await post<{ id: number }>('/customer/devices', { token: JSON.stringify(sub) })
      : await post<{ id: number }>('/rider/devices', { platform: 'web', token: JSON.stringify(sub), app_version: 'web' })
    localStorage.setItem(KEYS[who], String(d.id))
    chatPush.on = true
  } catch { /* stays off; the row stays where it is to try again */ } finally { chatPush.busy = false }
}

export async function disableChatPush(who: 'customer' | 'staff') {
  chatPush.busy = true
  try {
    const id = localStorage.getItem(KEYS[who])
    if (id) await del((who === 'customer' ? '/customer/devices/' : '/rider/devices/') + id).catch(() => {})
    localStorage.removeItem(KEYS[who])
    chatPush.on = false
  } finally { chatPush.busy = false }
}
