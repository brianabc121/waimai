// What the rider summary and the route page both need to know about a trip.
import { t } from '../../i18n'
import './strings'

export interface TripOrder { order_id: number; public_no: string; picked_up_at: string; outcome: string; done_at: string | null; drop_seq: number | null
  fail_reason?: string; customer_name?: string; address?: string; lat?: number; lng?: number }
export interface Trip { id: number; rider_id: number; day: string; status: string; end_reason: string; started_at: string; left_store_at: string | null
  last_drop_at: string | null; returned_at: string | null; ended_at: string | null; order_count: number; delivered_count: number; failed_count: number
  actual_m: number | null; track_points: number; gap_seconds: number; planned_m: number | null; planned_source: string; compare_m: number | null
  over_m: number | null; detour: boolean; orders: TripOrder[] }

/** metres as "8.1 km" */
export const km = (m: number) => `${(m / 1000).toFixed(1)} ${t('rr.km')}`
/** where a trip that has no return time stands */
export const tripState = (x: Trip) => x.status === 'loading' ? t('rr.s.loading') : x.status === 'out' ? t('rr.s.out') : t('rr.end.' + x.end_reason)
/** a difference in metres as "+1.3 km"; what rounds to nothing has no sign */
export const signedKm = (m: number) => { const v = (m / 1000).toFixed(1); return `${+v === 0 ? '0.0' : (m > 0 ? '+' : '') + v} ${t('rr.km')}` }
