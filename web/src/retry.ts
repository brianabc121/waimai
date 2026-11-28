// Two small helpers for requests whose answer may be late or lost
// (docs/CODE_REVIEW_FIX_PLAN.md H08, H09, M14).

function randomKey(): string {
  return globalThis.crypto?.randomUUID ? globalThis.crypto.randomUUID() : String(Date.now()) + Math.random().toString(16).slice(2)
}

interface Keep { getItem(k: string): string | null; setItem(k: string, v: string): void; removeItem(k: string): void }

// stableKey gives one Idempotency-Key per content: pressing the button again,
// or asking again after a lost answer, sends the same key for as long as
// what is being sent stays the same, so the server finds the first try. The
// key changes when the content does, and after done(). With keep (a
// sessionStorage) it also survives a reload of the page.
export function stableKey(keep?: Keep, name = 'idem-key') {
  let print = '', key = ''
  if (keep) {
    try {
      const saved = JSON.parse(keep.getItem(name) ?? 'null') as { print: string; key: string } | null
      if (saved && typeof saved.print === 'string' && typeof saved.key === 'string') ({ print, key } = saved)
    } catch { /* an unreadable value is no key */ }
  }
  return {
    for(content: unknown): string {
      const p = JSON.stringify(content)
      if (!key || p !== print) {
        print = p; key = randomKey()
        try { keep?.setItem(name, JSON.stringify({ print, key })) } catch { /* private mode: the key lives as long as the page */ }
      }
      return key
    },
    done() {
      print = ''; key = ''
      try { keep?.removeItem(name) } catch { /* nothing kept */ }
    },
  }
}

// latest tells a late answer from the current one: take a ticket before the
// request, and use the answer only when the ticket still holds.
//   const mine = turn.next(); const r = await get(...); if (!mine()) return
export function latest() {
  let seq = 0
  return { next(): () => boolean { const n = ++seq; return () => n === seq } }
}

// parts runs several reads on their own: one that fails does not hold back
// what the others brought. The caller shows what arrived and reports what
// did not (docs/CODE_REVIEW_FIX_PLAN.md M12).
export async function parts<T extends Record<string, () => Promise<unknown>>>(loaders: T):
  Promise<{ values: { [K in keyof T]?: Awaited<ReturnType<T[K]>> }; errors: { [K in keyof T]?: Error } }> {
  const names = Object.keys(loaders) as (keyof T)[]
  const done = await Promise.allSettled(names.map(n => loaders[n]()))
  const values: Record<string, unknown> = {}, errors: Record<string, Error> = {}
  done.forEach((r, i) => {
    const n = names[i] as string
    if (r.status === 'fulfilled') values[n] = r.value
    else errors[n] = r.reason instanceof Error ? r.reason : new Error(String(r.reason))
  })
  return { values, errors } as never
}

// screenState says what a live screen should tell its reader: 'never' when
// nothing was ever read (an empty screen is NOT "no orders"), 'stale' when
// the last good read is older than limit, 'failing' when the latest read
// failed but the data is still recent, 'ok' otherwise.
export function screenState(lastOK: number | null, failed: boolean, now: number, limit: number): 'ok' | 'failing' | 'stale' | 'never' {
  if (lastOK === null) return failed ? 'never' : 'ok' // the first read is still on its way
  if (now - lastOK > limit) return 'stale'
  return failed ? 'failing' : 'ok'
}

// signOut sends the sign-out and says whether the person is really out: yes
// when the server confirmed it, or said the session was already gone (401);
// no when the request was lost or the server failed, because then the
// session still works and the device must not pretend otherwise (M15).
export async function signOut(send: () => Promise<unknown>): Promise<boolean> {
  try { await send(); return true } catch (e) {
    return (e as { status?: number })?.status === 401
  }
}
