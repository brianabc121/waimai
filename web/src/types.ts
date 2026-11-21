export interface Branch {
  id: number; slug: string; name: string; name_en: string; address: string; phone: string
  timezone: string; currency: string; tax_rate_bp: number; tax_inclusive: boolean; service_charge_bp: number
  packaging_fee: number; min_order_amount: number; accept_timeout_minutes: number
  delivery_enabled: boolean; pickup_enabled: boolean; paused: boolean
  open_time: string; close_time: string; open_now: boolean; notify_sms: boolean
  scheduling_enabled: boolean; prep_minutes: number
  lat: number | null; lng: number | null; zone_mode: 'list' | 'map'; zone_tolerance_m: number
}
export interface Station { id: number; name: string; name_en: string; active: boolean; sort: number; item_count: number; open_tasks: number }
export type LatLngPair = [number, number]
export interface Zone { id: number; name: string; name_en: string; fee: number; min_order: number; active: boolean; sort: number
  polygon: LatLngPair[]; color: string; eta_minutes: number | null }
export interface Located { zone: Zone | null; distance_m: number; nearest: { name: string; name_en: string; distance_m: number } | null }
export interface Option { id: number; name: string; name_en: string; price_delta: number; sold_out: boolean }
export interface OptionGroup { id: number; name: string; name_en: string; min_select: number; max_select: number; options: Option[] }
export interface Item {
  id: number; category_id: number; name: string; name_en: string; description: string; image_url: string
  price: number; published: boolean; sold_out: boolean; stock_qty: number | null; needs_prep: boolean
  delivery_available: boolean; pickup_available: boolean; sort: number; station_id: number | null; groups: OptionGroup[]
}
export interface Category { id: number; name: string; name_en: string; sort: number; items: Item[] }
export interface Promotion {
  id: number; name: string; name_en: string; kind: 'amount_off' | 'percent_off' | 'free_delivery'
  threshold: number; value: number; max_discount: number | null; applies_delivery: boolean; applies_pickup: boolean
  starts_at: string | null; ends_at: string | null; daily_start: string; daily_end: string
  total_quota: number | null; per_phone_limit: number | null; used_count: number; status: string; live_now: boolean
}
export interface Template { layout: 'list' | 'featured' | 'grid'; theme_color: string; logo_url: string; cover_url: string }
export type Channel = 'delivery' | 'qr' | 'ipad'
export interface ChannelTemplate { channel: Channel; published: Template; draft: Template | null; published_at: string | null }
export interface Storefront { branch: Branch; zones: Zone[]; categories: Category[]; promotions: Promotion[]; slots: string[]; templates: Record<Channel, Template> }
export interface Totals {
  subtotal: number; packaging_fee: number; delivery_fee: number; service_charge: number
  tax_amount: number; tax_inclusive: boolean; tax_rate_bp: number; discount: number; total: number
  promotion_id: number; promotion_name: string; promotion_name_en: string
}
export interface LineOption { group: string; group_en: string; name: string; name_en: string; price_delta: number }
export interface OrderItem {
  id: number; item_id: number; name: string; name_en: string; unit_price: number; qty: number; line_total: number
  options: LineOption[]; note: string; needs_prep: boolean; prep_status: string
  station_id: number | null; station_name: string
  batch_seq: number; batch_source: string; served_at: string | null; void_reason?: string
}
export interface DeliveryTask {
  id: number; rider_id: number; rider_name: string; rider_phone: string; assignment_version: number
  status: string; fail_reason: string
}
export interface Cash {
  id: number; order_id: number; public_no: string; rider_name: string; amount_due: number; amount_collected: number
  reconcile_status: string; amount_returned: number | null; note: string; created_at: string
}
export interface Order {
  id: number; branch_id: number; public_no: string; pickup_code: string; service_type: 'delivery' | 'pickup' | 'dine_in'
  customer_name: string; customer_phone: string; zone_name: string; address_line: string; address_unit: string
  address_note: string; note: string; currency: string; totals: Totals; payment_method: string
  acceptance_status: string; packing_status: string; fulfillment_status: string; payment_status: string
  display_status: string; close_reason: string; bags: number | null; version: number; manifest_version: number
  dest_lat: number | null; dest_lng: number | null; zone_distance_m: number | null
  accept_deadline: string; scheduled_for: string | null; release_at: string | null; timezone: string; created_at: string; items: OrderItem[]; delivery: DeliveryTask | null; cash: Cash | null
  session_id: number | null; table_name: string; amount_received: number | null; payment_note: string
}
export interface TableDevice { id: number; name: string; paired: boolean; paired_at: string | null }
export interface TableSummary { id: number; order_id: number; public_no: string; guests: number; opened_at: string; total: number; item_count: number; unserved: number }
export interface DiningTable { id: number; name: string; qr_token: string; active: boolean; sort: number; session: TableSummary | null; devices: TableDevice[] }
export interface Bill { session_id: number; table_id: number; table_name: string; status: 'open' | 'closed'; guests: number; opened_at: string; closed_at: string | null; order: Order }
export interface TableAccess { slug: string; store_name: string; table_name: string; mode: 'qr' | 'ipad' }
export interface BatchLine { item_id: number; qty: number; option_ids: number[]; note: string }
export interface Quote { id: string; currency: string; service_type: string; zone_name: string; zone_distance_m: number | null; scheduled_for: string | null; totals: Totals; expires_at: string
  lines: { item_id: number; name: string; name_en: string; unit_price: number; qty: number; line_total: number; options: LineOption[]; note: string }[] }
export interface ImportRow {
  line: number; category: string; name: string; name_en: string; description: string; price: number; station: string
  action: string; old_price?: number; errors: string[]
}
export interface MenuImport {
  id: number; filename: string; status: 'pending' | 'parsed' | 'failed' | 'applied'; error: string
  total_rows: number; valid_rows: number; error_rows: number; created_count: number; updated_count: number
  created_at: string; rows: ImportRow[]
}
// A post is one job in a branch: a kitchen station, the packing counter, or rider.
export interface Post { kind: 'station' | 'packing' | 'rider'; station_id: number | null; station_name?: string; station_name_en?: string; station_active?: boolean }
// role is back-office access (owner, manager, cashier, staff); posts say what the person works on.
export interface BranchRole { branch_id: number; branch_name: string; slug: string; currency: string; role: string; posts: Post[] }
export interface StaffMember { id: number; username: string; display_name: string; phone: string; role: string; active: boolean; posts: Post[]; open_tasks: number; active_deliveries: number; on_duty: boolean }
export interface User { id: number; username: string; display_name: string; is_platform_admin: boolean; roles: BranchRole[] }
