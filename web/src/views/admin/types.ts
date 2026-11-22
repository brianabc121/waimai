export interface MerchantRow {
  id: number; name: string; status: 'active' | 'suspended' | 'deleted'; contact_name: string; contact_phone: string; note: string
  suspend_reason: string; suspended_at: string | null; deleted_at: string | null; created_at: string
  branch_count: number; slugs: string[]; orders_today: number; orders_7d: number
}
export interface AdminBranch {
  id: number; merchant_id: number; merchant_name: string; slug: string; name: string; name_en: string; timezone: string; currency: string
  address: string; phone: string; paused: boolean; open_now: boolean; orders_today: number; items_on_sale: number
}
export interface MerchantUser {
  id: number; username: string; display_name: string; phone: string; active: boolean; last_login_at: string | null
  roles: { branch_id: number; branch_name: string; role: string }[]
}
export interface MerchantDetail {
  merchant: MerchantRow
  stats: { orders_today: Record<string, number>; orders_7d: number; completed_7d: number; revenue_7d: { currency: string; amount: number }[] }
  branches: AdminBranch[]; users: MerchantUser[]
  reports: { id: number; branch_id: number; branch_name: string; reason: string; detail: string; contact: string; created_at: string }[]
}
export interface AdminItemRow {
  id: number; branch_id: number; branch_name: string; merchant_id: number; merchant_name: string; currency: string
  category_id: number; category_name: string; station_name: string; name: string; name_en: string; image_url: string
  price: number; published: boolean; sold_out: boolean; stock_qty: number | null; needs_prep: boolean; group_count: number; updated_at: string
}
export interface AuditEntry {
  id: number; admin_id: number; admin_name: string; action: string; target_type: string; target_id: number | null
  detail: Record<string, unknown>; ip: string; created_at: string
}
export interface WorkerInstance {
  instance: string; hostname: string; embedded: boolean; started_at: string; last_beat_at: string; last_loop_ms: number; online: boolean
}
export interface WorkerTask {
  task: string; interval_seconds: number; last_started_at: string | null; last_ok_at: string | null; last_duration_ms: number
  last_count: number; last_error: string; last_error_at: string | null; runs: number; failures: number; ok: boolean
}
export interface QueueKind { kind: string; pending: number; running: number; failed: number; done_24h: number }
export interface WorkerStatus {
  now: string; instances: WorkerInstance[]; tasks: WorkerTask[]
  queue: { pending: number; running: number; failed: number; done_24h: number; oldest_wait_seconds: number; by_kind: QueueKind[] }
}
export type JobStatus = 'pending' | 'running' | 'done' | 'failed' | 'discarded'
export interface JobRow {
  id: number; kind: string; status: JobStatus; attempts: number; max_attempts: number; subject: string
  run_at: string; created_at: string; finished_at: string | null; last_error: string; message?: string
}
export interface ApiPoint { t: string; ok: number; c4: number; c5: number; p50: number; p95: number }
export interface ApiTotals { requests: number; c4: number; c5: number; limited: number; p50: number; p95: number; max_ms: number }
export interface ApiRoute { method: string; route: string; count: number; c4: number; c5: number; p50: number; p95: number; max_ms: number }
export interface ApiStreams { instances: number; customer: number; table: number; store: number; rider: number }
export interface ApiMetrics { range: string; step_seconds: number; series: ApiPoint[]; totals: ApiTotals; routes: ApiRoute[]; streams: ApiStreams }
export interface ApiError {
  id: number; at: string; request_id: string; method: string; route: string; status: number; code: string
  duration_ms: number; user_id: number | null; message: string
}
export type Health = 'ok' | 'warn' | 'bad'
export interface Overview {
  now: string
  api: { state: Health; requests: number; error_rate: number; p95: number; instances: number }
  worker: { state: Health; online: number; failing_tasks: string[] }
  db: { state: Health; ping_ms: number; in_use: number; max: number }
  queue: { state: Health; pending: number; failed: number; oldest_wait_seconds: number }
  day: ApiTotals; orders_today: number; stores_open: number; stores_total: number
  hour: ApiPoint[]; errors: ApiError[]
}
