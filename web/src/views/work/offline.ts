// Rider steps taken without signal. Each step is kept on the phone and sent
// in order once the network is back. The server ignores a step it already
// has, so sending one twice is harmless.
import { reactive } from 'vue'
import { ApiError, newKey, post } from '../../api'
import type { Order } from '../../types'

export interface Op { id: string; task: number; order: number; no: string; action: string; body: Record<string, unknown>; at: string }

const KEY = 'work-ops'
const load = (): Op[] => { try { return JSON.parse(localStorage.getItem(KEY) || '[]') } catch { return [] } }
export const queue = reactive<{ ops: Op[] }>({ ops: load() })
const save = () => queue.ops.length ? localStorage.setItem(KEY, JSON.stringify(queue.ops)) : localStorage.removeItem(KEY)

export function enqueue(o: Order, action: string, body: Record<string, unknown>) {
  queue.ops.push({ id: newKey(), task: o.delivery!.id, order: o.id, no: o.public_no, action, body, at: new Date().toISOString() })
  save()
}

// What a queued step will do to a task, so the screen moves on while offline.
const after: Record<string, [string, string]> = {
  pickup: ['picked_up', 'out_for_delivery'], arrive: ['arrived', 'arrived'],
  deliver: ['delivered', 'delivered'], fail: ['failed', 'delivery_failed'],
}
const rank = ['assigned', 'picked_up', 'arrived', 'delivered', 'failed']
export function overlay(tasks: Order[]): Order[] {
  if (!queue.ops.length) return tasks
  return tasks.map(o => {
    let out = o
    for (const op of queue.ops) {
      const next = after[op.action]
      if (op.order !== o.id || !next || !out.delivery || rank.indexOf(next[0]) <= rank.indexOf(out.delivery.status)) continue
      out = { ...out, fulfillment_status: next[1], delivery: { ...out.delivery, status: next[0] } }
    }
    return out
  })
}

export interface Rejected { no: string; code: string; message: string }
let sending = false
// send posts the queue in order. It stops at the first network failure and
// returns the steps the server refused; a refused step takes the later steps
// of the same delivery with it.
export async function send(): Promise<Rejected[]> {
  const rejected: Rejected[] = []
  if (sending) return rejected
  sending = true
  try {
    while (queue.ops.length) {
      const op = queue.ops[0]
      try {
        await post(`/rider/tasks/${op.task}/${op.action}`, { ...op.body, op_id: op.id, at: op.at })
        queue.ops.shift()
      } catch (e) {
        const err = e as ApiError
        if (err.code === 'NETWORK' || err.status >= 500 || err.status === 429) break
        rejected.push({ no: op.no, code: err.code, message: err.message })
        queue.ops = queue.ops.filter(x => x.task !== op.task)
      }
      save()
    }
  } finally { sending = false; save() }
  return rejected
}
