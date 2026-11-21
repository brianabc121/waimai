export interface Branch {
  id: number; slug: string; name: string; name_en: string; address: string; phone: string
  timezone: string; currency: string; tax_rate_bp: number; tax_inclusive: boolean; service_charge_bp: number
  packaging_fee: number; min_order_amount: number; accept_timeout_minutes: number
  delivery_enabled: boolean; pickup_enabled: boolean; paused: boolean
  open_time: string; close_time: string; open_now: boolean; notify_sms: boolean
}
export interface Station { id: number; name: string; name_en: string; active: boolean; sort: number; item_count: number; open_tasks: number }
export interface Zone { id: number; name: string; name_en: string; fee: number; min_order: number; active: boolean; sort: number }
export interface Option { id: number; name: string; name_en: string; price_delta: number; sold_out: boolean }
export interface OptionGroup { id: number; name: string; name_en: string; min_select: number; max_select: number; options: Option[] }
export interface Item {
  id: number; category_id: number; name: string; name_en: string; description: string; image_url: string
  price: number; published: boolean; sold_out: boolean; stock_qty: number | null; needs_prep: boolean
  delivery_available: boolean; pickup_available: boolean; sort: number; station_id: number | null; groups: OptionGroup[]
}
export interface Category { id: number; name: string; name_en: string; sort: number; items: Item[] }
export interface Storefront { branch: Branch; zones: Zone[]; categories: Category[] }
export interface Totals {
  subtotal: number; packaging_fee: number; delivery_fee: number; service_charge: number
  tax_amount: number; tax_inclusive: boolean; tax_rate_bp: number; discount: number; total: number
}
export interface LineOption { group: string; group_en: string; name: string; name_en: string; price_delta: number }
export interface OrderItem {
  id: number; item_id: number; name: string; name_en: string; unit_price: number; qty: number; line_total: number
  options: LineOption[]; note: string; needs_prep: boolean; prep_status: string
  station_id: number | null; station_name: string
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
  id: number; branch_id: number; public_no: string; pickup_code: string; service_type: 'delivery' | 'pickup'
  customer_name: string; customer_phone: string; zone_name: string; address_line: string; address_unit: string
  address_note: string; note: string; currency: string; totals: Totals; payment_method: string
  acceptance_status: string; packing_status: string; fulfillment_status: string; payment_status: string
  display_status: string; close_reason: string; bags: number | null; version: number; manifest_version: number
  accept_deadline: string; created_at: string; items: OrderItem[]; delivery: DeliveryTask | null; cash: Cash | null
}
export interface Quote { id: string; currency: string; service_type: string; zone_name: string; totals: Totals; expires_at: string
  lines: { item_id: number; name: string; name_en: string; unit_price: number; qty: number; line_total: number; options: LineOption[]; note: string }[] }
export interface BranchRole { branch_id: number; branch_name: string; slug: string; currency: string; role: string }
export interface User { id: number; username: string; display_name: string; is_platform_admin: boolean; roles: BranchRole[] }
