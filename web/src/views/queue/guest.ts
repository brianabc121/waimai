// The guest's browser remembers one queue number per store, and its own id
// (one number at a time per browser, docs/QUEUE_DISPLAY_DESIGN.md §8).
import { newKey } from '../../api'

function read(k: string): string | null { try { return localStorage.getItem(k) } catch { return null } }
function write(k: string, v: string) { try { localStorage.setItem(k, v) } catch { /* private mode */ } }

export function guestClient(): string {
  let id = read('q-client')
  if (!id) { id = newKey(); write('q-client', id) }
  return id
}
export const savedToken = (slug: string) => read('q-token:' + slug)
export const saveToken = (slug: string, token: string) => write('q-token:' + slug, token)
