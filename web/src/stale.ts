// After an update the built files get new names and the old ones are gone.
// A page opened before the update still asks for the old names when the user
// moves to a screen it has not loaded yet, and that fails with a 404. Loading
// the page again fetches the new index.html (served with no-cache) and with
// it the new names. At most one reload per 30 s, so a real outage does not
// turn into a reload loop.
import type { Router } from 'vue-router'

const KEY = 'stale-reload-at'

export function isChunkError(err: unknown): boolean {
  const m = String((err as { message?: string })?.message ?? err)
  return /Failed to fetch dynamically imported module|error loading dynamically imported module|Importing a module script failed|Unable to preload CSS|Loading (CSS )?chunk .* failed/i.test(m)
}

export function reloadOnce(target?: string): boolean {
  let last = 0
  try { last = Number(sessionStorage.getItem(KEY)) || 0 } catch { /* storage blocked */ }
  if (Date.now() - last < 30_000) return false
  try { sessionStorage.setItem(KEY, String(Date.now())) } catch { /* storage blocked */ }
  if (target && target !== location.pathname + location.search + location.hash) location.assign(target)
  else location.reload()
  return true
}

export function watchStaleBuild(router: Router) {
  // the screen being opened, so that the reload lands on it
  let opening: string | undefined
  router.beforeEach(to => { opening = to.fullPath })
  router.afterEach(() => { opening = undefined })
  // Vite reports a failed load of a split file here; preventDefault keeps it
  // from also being thrown when the page is being reloaded anyway.
  window.addEventListener('vite:preloadError', e => {
    if (reloadOnce(opening)) e.preventDefault()
  })
  // A screen whose file is gone: open that screen again on the new build.
  router.onError((err, to) => {
    if (isChunkError(err)) reloadOnce(to?.fullPath)
  })
}
