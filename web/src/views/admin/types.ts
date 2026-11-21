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
