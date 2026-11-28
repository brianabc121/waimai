// The rider's offline steps, kept apart per account (CODE_REVIEW_FIX_PLAN
// M16): what one rider left unsent is neither shown to nor sent by the next
// person who signs in on the same phone. No Vue and no network in here, so
// it can be tested on its own; offline.ts wires it to the page.

export interface Op { id: string; task: number; order: number; no: string; action: string; body: Record<string, unknown>; at: string }
export interface Rejected { no: string; code: string; message: string }
interface Keep { getItem(k: string): string | null; setItem(k: string, v: string): void; removeItem(k: string): void }
interface SendError { code?: string; status?: number; message?: string }

const OLD_KEY = 'work-ops' // before the queue was per account
const keyOf = (user: number) => `work-ops:${user}`

export function opQueue(keep: Keep, sendOne: (op: Op) => Promise<unknown>, state: { ops: Op[] } = { ops: [] }) {
  let owner = 0
  // Every change of owner starts a new turn; a send loop from an earlier
  // turn stops at its next step and leaves the queue alone.
  let turn = 0
  let sending = false

  const load = (user: number): Op[] => {
    try { const v = JSON.parse(keep.getItem(keyOf(user)) || '[]'); return Array.isArray(v) ? v : [] } catch { return [] }
  }
  const save = () => {
    if (!owner) return
    try { state.ops.length ? keep.setItem(keyOf(owner), JSON.stringify(state.ops)) : keep.removeItem(keyOf(owner)) } catch { /* storage full or off */ }
  }

  return {
    state,
    // own makes the queue this account's: its own unsent steps come back.
    own(user: number) {
      try { keep.removeItem(OLD_KEY) } catch { /* nothing to drop */ }
      if (user === owner) return
      owner = user; turn++; sending = false
      state.ops = user ? load(user) : []
    },
    // drop is signing out: nothing stays in memory and nothing more is sent.
    // The steps stay on the phone under their owner, for when they return.
    drop() { owner = 0; turn++; sending = false; state.ops = [] },
    enqueue(op: Op) {
      if (!owner) return
      state.ops.push(op); save()
    },
    // send posts the queue in order. It stops at the first network failure
    // and returns the steps the server refused; a refused step takes the
    // later steps of the same delivery with it.
    async send(): Promise<Rejected[]> {
      const rejected: Rejected[] = []
      if (sending || !owner) return rejected
      sending = true
      const mine = turn
      try {
        while (state.ops.length) {
          const op = state.ops[0]
          try {
            await sendOne(op)
            if (turn !== mine) return rejected
            state.ops.shift()
          } catch (e) {
            if (turn !== mine) return rejected
            const err = e as SendError
            if (err.code === 'NETWORK' || (err.status ?? 0) >= 500 || err.status === 429) break
            rejected.push({ no: op.no, code: err.code ?? '', message: err.message ?? '' })
            state.ops = state.ops.filter(x => x.task !== op.task)
          }
          save()
        }
      } finally {
        if (turn === mine) { sending = false; save() }
      }
      return rejected
    },
  }
}
