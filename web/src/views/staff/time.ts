// Dates and clock times in the store's time zone, whatever the browser's is.

const parts = (ms: number, timeZone: string) => {
  const f = new Intl.DateTimeFormat('en-CA', { timeZone, hourCycle: 'h23', year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })
  const o: Record<string, string> = {}
  for (const p of f.formatToParts(new Date(ms))) o[p.type] = p.value
  return o
}
/** "2026-11-21" of an instant in the zone */
export const dayOf = (iso: string | number, tz: string) => { const p = parts(typeof iso === 'number' ? iso : Date.parse(iso), tz); return `${p.year}-${p.month}-${p.day}` }
/** "09:58" of an instant in the zone */
export const clock = (iso: string | number, tz: string) => { const p = parts(typeof iso === 'number' ? iso : Date.parse(iso), tz); return `${p.hour}:${p.minute}` }
/** value for <input type="datetime-local"> */
export const toInput = (iso: string, tz: string) => `${dayOf(iso, tz)}T${clock(iso, tz)}`
/** the instant of a wall-clock time ("2026-11-21T09:58") in the zone */
export function fromInput(local: string, tz: string): string | null {
  const m = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})/.exec(local)
  if (!m) return null
  const want = Date.UTC(+m[1], +m[2] - 1, +m[3], +m[4], +m[5])
  let guess = want
  for (let i = 0; i < 2; i++) {
    const p = parts(guess, tz)
    guess += want - Date.UTC(+p.year, +p.month - 1, +p.day, +p.hour, +p.minute)
  }
  return new Date(guess).toISOString()
}
/** hours since midnight of `day` in the zone, may be below 0 or above 24 */
export const hoursInto = (iso: string | number, day: string, tz: string) =>
  ((typeof iso === 'number' ? iso : Date.parse(iso)) - Date.parse(fromInput(day + 'T00:00', tz)!)) / 3600000
export const addDays = (day: string, n: number) => new Date(Date.parse(day + 'T00:00:00Z') + n * 86400000).toISOString().slice(0, 10)
export const monthStart = (day: string) => day.slice(0, 8) + '01'
export const monthEnd = (day: string) => { const [y, m] = day.split('-').map(Number); return new Date(Date.UTC(y, m, 0)).toISOString().slice(0, 10) }
export const addMonths = (day: string, n: number) => { const [y, m] = day.split('-').map(Number); return new Date(Date.UTC(y, m - 1 + n, 1)).toISOString().slice(0, 10) }
export const weekday = (day: string) => new Date(day + 'T00:00:00Z').getUTCDay()
/** minutes as "4:12" */
export const hm = (min: number) => `${Math.floor(min / 60)}:${String(Math.round(min) % 60).padStart(2, '0')}`
export const metres = (m: number) => m >= 1000 ? `${(m / 1000).toFixed(1)} km` : `${m} m`
