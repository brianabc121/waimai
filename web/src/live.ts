import { onBeforeUnmount, onMounted, ref } from 'vue'

// useLive keeps a screen in sync: an SSE stream delivers "refresh" hints and
// the screen re-reads its snapshot. Hints can be lost, so it also polls slowly,
// and quickly while the stream is down.
export function useLive(url: () => string | null, refresh: () => Promise<void> | void) {
  const online = ref(true)
  const lastSync = ref<Date | null>(null)
  let es: EventSource | null = null
  let timer: number | undefined
  let running = false
  let again = false
  let stopped = false

  async function run() {
    if (running) { again = true; return } // coalesce bursts; never run two reads at once
    running = true
    try {
      await refresh()
      lastSync.value = new Date()
    } catch { /* keep the last confirmed snapshot on screen */ }
    running = false
    if (again) { again = false; void run() }
  }

  function schedule() {
    window.clearTimeout(timer)
    if (stopped) return
    timer = window.setTimeout(() => { void run(); schedule() }, online.value ? 30000 : 5000)
  }

  function connect() {
    const u = url()
    if (!u || stopped) return
    es = new EventSource('/api/v1' + u, { withCredentials: true })
    es.addEventListener('ready', () => { online.value = true; void run(); schedule() })
    es.addEventListener('refresh', () => void run())
    es.onerror = () => {
      online.value = false
      schedule()
      // EventSource retries by itself unless the server closed it for good.
      if (es && es.readyState === EventSource.CLOSED) {
        es = null
        window.setTimeout(connect, 5000)
      }
    }
  }

  const onVisible = () => { if (document.visibilityState === 'visible') void run() }

  onMounted(() => {
    void run()
    connect()
    schedule()
    document.addEventListener('visibilitychange', onVisible)
  })
  onBeforeUnmount(() => {
    stopped = true
    es?.close()
    window.clearTimeout(timer)
    document.removeEventListener('visibilitychange', onVisible)
  })
  return { online, lastSync, refreshNow: run }
}
