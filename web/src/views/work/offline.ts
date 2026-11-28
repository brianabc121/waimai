// Rider steps taken without signal. Each step is kept on the phone and sent
// in order once the network is back. The server ignores a step it already
// has, so sending one twice is harmless.
import { reactive } from 'vue'
import { newKey, post } from '../../api'
import type { Order } from '../../types'

import { opQueue, type Op, type Rejected } from './opqueue'
export type { Op, Rejected }

// The queue belongs to the signed-in account (opqueue.ts): useQueue(user)
// when the workbench opens, dropQueue() on signing out.
export const queue = reactive<{ ops: Op[] }>({ ops: [] })
const q = opQueue(localStorage, op => post(`/rider/tasks/${op.task}/${op.action}`, { ...op.body, op_id: op.id, at: op.at }), queue)
export const useQueue = (user: number) => q.own(user)
export const dropQueue = () => q.drop()

export function enqueue(o: Order, action: string, body: Record<string, unknown>) {
  q.enqueue({ id: newKey(), task: o.delivery!.id, order: o.id, no: o.public_no, action, body, at: new Date().toISOString() })
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

export const send = () => q.send()
