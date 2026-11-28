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
