// Service worker of the staff workbench, also registered by the chat pages
// for their reminders. It keeps the workbench's page shell so that it opens
// without signal, and shows push reminders. API calls are never cached.
const SHELL = 'work-shell-v1'

self.addEventListener('install', () => self.skipWaiting())
self.addEventListener('activate', e => e.waitUntil((async () => {
  for (const k of await caches.keys()) if (k !== SHELL) await caches.delete(k)
  await self.clients.claim()
})()))

self.addEventListener('fetch', e => {
  const req = e.request
  const url = new URL(req.url)
  if (req.method !== 'GET' || url.origin !== location.origin || url.pathname.startsWith('/api/')) return
  if (req.mode === 'navigate') {
    // Always the newest page when online; the saved one when not.
    e.respondWith((async () => {
      const cache = await caches.open(SHELL)
      try {
        const res = await fetch(req)
        if (res.ok && url.pathname.startsWith('/work')) await cache.put('/work', res.clone())
        return res
      } catch (err) {
        const saved = url.pathname.startsWith('/work') && await cache.match('/work')
        if (saved) return saved
        throw err
      }
    })())
    return
  }
  if (url.pathname.startsWith('/assets/')) {
    // Built files carry a hash in their name, so a saved copy never goes stale.
    e.respondWith((async () => {
      const cache = await caches.open(SHELL)
      const hit = await cache.match(req)
      if (hit) return hit
      const res = await fetch(req)
      if (res.ok) await cache.put(req, res.clone())
      return res
    })())
  }
})

self.addEventListener('push', e => {
  let m = {}
  try { m = e.data.json() } catch { m = { title: e.data ? e.data.text() : '' } }
  e.waitUntil(self.registration.showNotification(m.title || '新消息 New message', {
    body: m.body || '', tag: m.tag, renotify: !!m.tag, icon: '/icons/work-192.png', badge: '/icons/work-192.png',
    data: { url: m.url || '/work' },
  }))
})

self.addEventListener('notificationclick', e => {
  e.notification.close()
  const target = e.notification.data && e.notification.data.url || '/work'
  e.waitUntil((async () => {
    const open = await self.clients.matchAll({ type: 'window', includeUncontrolled: true })
    // a page that already shows the place the reminder is about comes forward
    const path = new URL(target, location.origin).pathname
    for (const c of open) {
      if (!new URL(c.url).pathname.startsWith(path)) continue
      await c.focus()
      // a chat reminder names the conversation: go to it
      if (target.includes('?') && !c.url.endsWith(target)) await c.navigate(target).catch(() => {})
      return
    }
    return self.clients.openWindow(target)
  })())
})
