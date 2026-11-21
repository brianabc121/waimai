// Service worker of the staff workbench. It is registered only from /work.
// It keeps the page shell so the workbench opens without signal, and shows
// push reminders. API calls are never cached.
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
  e.waitUntil(self.registration.showNotification(m.title || '员工工作台', {
    body: m.body || '', tag: m.tag, renotify: !!m.tag, icon: '/icons/work-192.png', badge: '/icons/work-192.png',
    data: { url: m.url || '/work' },
  }))
})

self.addEventListener('notificationclick', e => {
  e.notification.close()
  const target = e.notification.data && e.notification.data.url || '/work'
  e.waitUntil((async () => {
    const open = await self.clients.matchAll({ type: 'window', includeUncontrolled: true })
    for (const c of open) if (new URL(c.url).pathname.startsWith('/work')) return c.focus()
    return self.clients.openWindow(target)
  })())
})
