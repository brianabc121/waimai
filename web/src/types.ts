export interface Branch {
  id: number; slug: string; name: string; name_en: string; address: string; phone: string
  timezone: string; currency: string; tax_rate_bp: number; tax_inclusive: boolean; service_charge_bp: number
  packaging_fee: number; min_order_amount: number; accept_timeout_minutes: number; addon_confirm_minutes?: number
  delivery_enabled: boolean; pickup_enabled: boolean; paused: boolean
  open_time: string; close_time: string; open_now: boolean; notify_sms: boolean
  scheduling_enabled: boolean; prep_minutes: number
  // the language the menu is written in; languages whose customers get the English menu
  menu_lang?: string; menu_langs_off?: string[]
  // the name, notice and introduction in the customer's language (customer pages)
  name_tr?: string; notice_tr?: string; intro_tr?: string
  kitchen_slow_minutes: number; cook_target_minutes?: number; detour_percent: number; detour_meters: number
  lat: number | null; lng: number | null; zone_mode: 'list' | 'map'; zone_tolerance_m: number
  notice: string; intro: string; baskets_enabled: boolean
  // which of wechat, telegram, line, whatsapp the address form asks customers for
  contact_fields: string[]
  // where the store is, in words; empty until its location was saved on the map
  country_code: string; country: string; province: string; city: string; district: string; postal_code: string
}
export interface Station { id: number; name: string; name_en: string; active: boolean; sort: number; item_count: number; open_tasks: number }
export type LatLngPair = [number, number]
export interface Zone { id: number; name: string; name_en: string; fee: number; min_order: number; active: boolean; sort: number
  polygon: LatLngPair[]; color: string; eta_minutes: number | null }
// name_tr and the like: the text in the customer's language, when it differs (customer pages only)
export interface Option { id: number; name: string; name_en: string; price_delta: number; sold_out: boolean; name_tr?: string }
export interface OptionGroup { id: number; name: string; name_en: string; min_select: number; max_select: number; options: Option[]; name_tr?: string }
export interface Item {
  id: number; category_id: number; name: string; name_en: string; description: string; image_url: string
  price: number; published: boolean; sold_out: boolean; stock_qty: number | null; needs_prep: boolean
  delivery_available: boolean; pickup_available: boolean; dine_in_available?: boolean; sort: number; station_id: number | null; groups: OptionGroup[]
  // The dish page: more pictures after image_url, and a longer text.
  photos?: string[]; details?: string
  // How many were ordered so far; only the customer's store page gets it.
  sold?: number
  name_tr?: string; description_tr?: string; details_tr?: string
  // a short code staff type to find the dish, such as B1
  code?: string
  // Flavour note templates (docs/NOTE_TEMPLATE_DESIGN.md): the ones the dish
  // shows, and their groups with the tags on offer.
  note_template_ids?: number[]; notes?: NoteGroup[]
}
export interface NoteTag { id: number; name: string; name_en: string; active: boolean; name_tr?: string }
export interface NoteGroup { id: number; name: string; name_en: string; multi: boolean; tags: NoteTag[]; name_tr?: string }
export interface NoteTemplate { id: number; name: string; icon: string; sort: number; groups: NoteGroup[]; item_ids: number[] }
// A chosen tag as the order line keeps it.
export interface LineNoteTag { id: number; group: string; name: string; group_tr?: string; name_tr?: string }
export interface Category { id: number; name: string; name_en: string; sort: number; required?: boolean; items: Item[]; name_tr?: string }
export interface Promotion {
  id: number; name: string; name_en: string; kind: 'amount_off' | 'percent_off' | 'free_delivery'
  threshold: number; value: number; max_discount: number | null; applies_delivery: boolean; applies_pickup: boolean
  starts_at: string | null; ends_at: string | null; daily_start: string; daily_end: string
  total_quota: number | null; per_phone_limit: number | null; used_count: number; status: string; live_now: boolean
  name_tr?: string
  scope?: 'delivery' | 'dine_in'
}
export interface Template { layout: 'list' | 'featured' | 'grid'; theme_color: string; logo_url: string; cover_url: string }
export type Channel = 'delivery' | 'qr' | 'ipad'
export interface ChannelTemplate { channel: Channel; published: Template; draft: Template | null; published_at: string | null }
export interface Storefront { branch: Branch; zones: Zone[]; categories: Category[]; promotions: Promotion[]; dine_promotions?: Promotion[]; slots: string[]; templates: Record<Channel, Template>
  sales: number; rating: ReviewSummary; media?: PromoMedia[]
  // some of the menu is shown machine translated
  machine_translated?: boolean }
export interface Crop { x: number; y: number; w: number; h: number }
// A promotional picture, GIF or video at the top of the store page.
export interface PromoMedia { id: number; kind: 'image' | 'gif' | 'video'; url: string; poster_url: string; title: string
  crop: Crop; trim_start: number; trim_end: number; active: boolean; size: number; created_at: string }
export interface ReviewSummary { count: number; average: number; taste: number; packaging: number; delivery: number }
export interface Review { id: number; display_name: string; rating: number; taste: number; packaging: number; delivery: number | null
  comment: string; created_at: string; items: { name: string; name_en: string }[] }
export interface ReviewPage { summary: ReviewSummary; reviews: Review[]; more: boolean }
export interface OrderReview { can_review: boolean; review: Review | null }
export interface StoreReport { id: number; branch_id: number; branch_name: string; reason: string; detail: string; contact: string; created_at: string }
export interface Totals {
  subtotal: number; packaging_fee: number; delivery_fee: number; service_charge: number
  tax_amount: number; tax_inclusive: boolean; tax_rate_bp: number; discount: number; total: number
  promotion_id: number; promotion_name: string; promotion_name_en: string
  // the promotion's part (quotes only); the store's and the platform's parts of discount
  promotion_discount?: number; merchant_discount: number; platform_subsidy: number
  // the discount at checkout (docs/CHECKOUT_DISCOUNT_DESIGN.md §5): adj_percent_bp is the share
  // of the dishes still charged (9000 = 10% off), staff_discount what it took off, round_off
  // what the round-off or the typed amount took off what is paid
  adj_percent_bp?: number; staff_discount?: number; adj_mode?: '' | 'round' | 'set_total'; round_off?: number
}
export interface PayChannel { id: number; country: string; name: string; names: Record<string, string>; icon_url: string; sort: number; active: boolean; stores?: number; uses?: number }
export interface PayOptions { cash: boolean; card: boolean; channels: PayChannel[]; all: PayChannel[]; off: number[]; reasons: string[] | null; country: string }
export interface Adjust { percent_bp: number; mode: '' | 'round' | 'set_total'; amount: number; reason: string }
export interface PayChoice { type: 'cash' | 'card' | 'online'; channel_id: number }
export interface LineOption { option_id?: number; group: string; group_en: string; name: string; name_en: string; price_delta: number; group_tr?: string; name_tr?: string; qty?: number }
export interface OrderItem {
  id: number; item_id: number; name: string; name_en: string; name_tr?: string; unit_price: number; qty: number; line_total: number
  options: LineOption[]; note: string; needs_prep: boolean; prep_status: string
  note_tags?: LineNoteTag[]
  station_id: number | null; station_name: string
  batch_seq: number; batch_source: string; served_at: string | null; void_reason?: string
  // dine-in: when the round was sent, its notes, who sent it, and when the kitchen was asked to hurry
  batch_at?: string; round_tags?: string[]; batch_by?: string; rush_at?: string | null
  // cooking progress: when it reached the kitchen, who started it and who made it
  kitchen_at?: string | null; prep_started_at?: string | null; started_by?: string; ready_at?: string | null; ready_by?: string
  basket: number
  // the dish's picture; only on the customer's order page
  image_url?: string
}
export interface Aftersale { id: number; reason: string; detail: string; status: 'open' | 'resolved' | 'declined'; reply: string; created_at: string; handled_at: string | null }
export interface OrderStore { slug: string; name: string; name_en: string; name_tr?: string; phone: string; address: string; logo_url: string }
export interface OrderCard {
  token: string; public_no: string; store: OrderStore; service_type: 'delivery' | 'pickup'; display_status: string
  created_at: string; timezone: string; total: number; currency: string; item_count: number
  items: { name: string; name_en: string; name_tr?: string; qty: number; image_url: string }[]
  can_review: boolean; reviewed: boolean; aftersale: '' | 'open' | 'resolved' | 'declined'
}
export interface Reorder { slug: string; service_type: 'delivery' | 'pickup'
  lines: { item_id: number; qty: number; option_ids: number[]; note: string; note_tag_ids?: number[]; basket: number }[]; missing: { name: string; name_en: string }[] }
export interface DeliveryTask {
  id: number; rider_id: number; rider_name: string; rider_phone: string; assignment_version: number
  status: string; fail_reason: string; assigned_at: string; seen_at: string | null; proof_at: string | null
}
export interface Cash {
  id: number; order_id: number; public_no: string; rider_name: string; amount_due: number; amount_collected: number
  reconcile_status: string; amount_returned: number | null; note: string; created_at: string
}
export interface Order {
  id: number; branch_id: number; public_no: string; pickup_code: string; service_type: 'delivery' | 'pickup' | 'dine_in'
  customer_name: string; customer_phone: string; zone_name: string; address_line: string; address_unit: string
  // from the saved address: who to call (may differ from the account that ordered), other ways to reach them, a photo of the door
  contact_phone: string; contact_title: '' | 'mr' | 'ms'; contact_wechat: string; contact_telegram: string; contact_line: string; contact_whatsapp: string; address_photo: boolean
  address_note: string; note: string; currency: string; totals: Totals; payment_method: string
  acceptance_status: string; packing_status: string; fulfillment_status: string; payment_status: string
  display_status: string; close_reason: string; bags: number | null; version: number; manifest_version: number
  dest_lat: number | null; dest_lng: number | null; zone_distance_m: number | null
  accept_deadline: string; scheduled_for: string | null; release_at: string | null; timezone: string; created_at: string; items: OrderItem[]; delivery: DeliveryTask | null; cash: Cash | null
  session_id: number | null; table_name: string; amount_received: number | null; payment_note: string
  pay_method?: string; table_tags?: string[]
  // how it was paid and who gave the discount at checkout (staff only)
  pay_type?: '' | 'cash' | 'card' | 'online'; pay_channel_id?: number; adj_reason?: string; adj_by?: string; adj_approved_by?: string; adj_at?: string | null
  aftersale: Aftersale | null; store?: OrderStore; completed_at?: string | null; accepted_at?: string | null; closed_at?: string | null
  // the promotion and each coupon taken off; the coupon this order earned (customer page only)
  discounts?: OrderDiscount[]; gift?: CustomerCoupon | null
  // orders placed by staff for a customer: the store member, where the customer asked, who placed it, a note for staff
  member_no?: number | null; order_source?: string; placed_by?: string; staff_note?: string
  // the order, each add-on and the customer's add-ons waiting for the store (docs/ADD_ON_ORDER_DESIGN.md)
  batches?: OrderBatch[]; addon?: AddonState
  // dishes taken off and money given back by dish (docs/VOID_ITEM_DESIGN.md)
  voids?: OrderVoid[]; refunds?: OrderRefund[]; void_open?: boolean; refund_open?: boolean; refunded?: number
}
// One void; who did it and how far the dish had got are only on the store's pages.
export interface OrderVoid {
  id: number; item_id: number; batch_seq: number; name: string; qty: number; amount: number; reason: string; reason_kind: 'store' | 'customer'
  was_status: string; wasted: boolean; station_id: number | null; total_before: number; total_after: number
  by?: string; approved_by?: string; needs_ack: boolean; ack_at: string | null; ack_by?: string; at: string
}
export interface RefundLine { item_id: number; name: string; qty: number; amount: number }
export interface OrderRefund { id: number; lines: RefundLine[]; amount: number; method: string; reason: string; reason_kind: string; by?: string; approved_by?: string; at: string }
export interface VoidQuote {
  name: string; qty: number; max_qty: number; amount: number; was_status: string; started_at: string | null; wasted: boolean
  needs_approval: boolean; before: Totals; after: Totals; less: number; promo_before: string; promo_after: string; promo_down: boolean
  rider: string; packed: boolean; version: number
}
export interface RefundQuote { lines: RefundLine[]; dishes: number; suggested: number; left: number }
export interface QuoteLine { item_id: number; name: string; name_en: string; name_tr?: string; unit_price: number; qty: number; line_total: number
  options: LineOption[]; note: string; note_tags?: LineNoteTag[]; needs_prep?: boolean; basket?: number }
export interface OrderBatch {
  seq: number; source: string; status: 'pending' | 'accepted' | 'rejected' | 'expired' | 'cancelled'; at: string; by?: string
  round_tags: string[]; total_before: number | null; total_after: number | null; reject_reason?: string; expires_at: string | null
  lines?: QuoteLine[]
}
export interface AddonState { open: boolean; why?: '' | 'handed_off' | 'closed' | 'dine_in'; repack: boolean; customer_left: number; pending: boolean; confirm_minutes: number }
export interface AddonQuote { currency: string; lines: QuoteLine[]; subtotal: number; before: Totals; after: Totals; extra: number; promo_up: boolean; state: AddonState }
// a customer of one store, kept by the store (orders placed for a customer)
export interface StoreMember {
  id: number; no: number; status: 'temp' | 'active'; phone: string; name: string; source: string; contact: string
  address_line: string; address_unit: string; address_note: string; lat: number | null; lng: number | null; note: string
  expires_at: string | null; created_at: string
  stats?: { week: number; month: number; quarter: number; orders: number; spent: number; last_at: string | null; favorites: string[] }
}
export interface AddressHint { line: string; unit: string; note: string; lat: number | null; lng: number | null; mine: boolean; kind?: 'mine' | 'book' | 'store'; zone_name: string; fee: number | null }
export interface TableDevice { id: number; name: string; paired: boolean; paired_at: string | null }
export interface TableSummary { id: number; order_id: number; public_no: string; guests: number; opened_at: string; total: number; item_count: number; unserved: number
  unserved_qty: number; rounds: number; tags: string[] }
export interface DiningTable { id: number; name: string; qr_token: string; active: boolean; sort: number; area: string; seats: number; session: TableSummary | null; devices: TableDevice[] }
export interface Bill { session_id: number; table_id: number; table_name: string; status: 'open' | 'closed'; guests: number; opened_at: string; closed_at: string | null; order: Order
  tags: string[]; moved_from: string; area: string; seats: number }
// The dine-in desk's tap-to-choose lists (docs/DINE_IN_ORDER_DESIGN.md); null lists mean the built-in ones.
export interface DinePresets { table_tags: string[] | null; round_tags: string[] | null; void_reasons: string[] | null; pay_methods: string[] | null
  long_minutes: number; stay_after_send: boolean; show_photos: boolean | null }
export interface TableAccess { slug: string; store_name: string; table_name: string; mode: 'qr' | 'ipad' }
export interface BatchLine { item_id: number; qty: number; option_ids: number[]; note: string; note_tag_ids?: number[] }
export interface Quote { id: string; currency: string; service_type: string; zone_name: string; zone_distance_m: number | null; scheduled_for: string | null; totals: Totals; expires_at: string
  coupons: AppliedCoupon[]; coupon_choices: CouponChoice[]
  lines: { item_id: number; name: string; name_en: string; unit_price: number; qty: number; line_total: number; options: LineOption[]; note: string; note_tags?: LineNoteTag[]; basket?: number }[] }
export interface ImportSpecGroup { name: string; min_select: number; max_select: number; options: { name: string; price_delta: number }[] }
export interface ImportRow {
  line: number; category: string; name: string; name_en: string; description: string; price: number; station: string
  published: boolean | null; dine_in: boolean | null; delivery: boolean | null; pickup: boolean | null
  image_url: string; image_set: boolean; photos: string[]; photos_set: boolean; details: string; details_set: boolean
  specs: ImportSpecGroup[]; specs_set: boolean; new_category?: boolean
  note_templates?: string[]; note_templates_set?: boolean
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
export interface BranchRole { branch_id: number; branch_name: string; slug: string; currency: string; role: string; posts: Post[]; perms?: string[] }
export interface StaffMember { id: number; username: string; display_name: string; phone: string; role: string; active: boolean; posts: Post[]; open_tasks: number; active_deliveries: number; on_duty: boolean }
export interface RiderInfo extends StaffMember { lat: number | null; lng: number | null; accuracy: number | null; battery: number | null; located_at: string | null; contact: '' | 'ok' | 'none' | 'lost'; distance_m: number | null }
export interface Tracking { active: boolean; rider_name?: string; rider_phone?: string; lat: number | null; lng: number | null; located_at: string | null; dest_lat: number | null; dest_lng: number | null; distance_m: number | null }
export interface User { id: number; username: string; display_name: string; is_platform_admin: boolean; roles: BranchRole[]; entry?: Entry; has_approve_pin?: boolean }
// Where a sign-in leads (docs/STAFF_ACCESS_DESIGN.md §5).
export type Entry = 'office' | 'work' | 'admin'

// ---- coupons (docs/COUPON_DESIGN.md) ----
export type CouponKind = 'store_threshold' | 'store_free' | 'store_delivery' | 'platform_free'
export interface CustomerCoupon {
  id: number; kind: CouponKind; issuer: 'store' | 'platform'; branch_id: number | null
  store_slug: string; store_name: string; store_name_en: string; currency: string
  name: string; name_en: string; value: number; threshold: number; source: string
  issued_at: string; expires_at: string; status: 'available' | 'used' | 'expired'; used_at: string | null
  country: string; country_code: string; province: string
}
export interface CouponChoice extends CustomerCoupon {
  usable: boolean; reason: '' | 'below_threshold' | 'delivery_only' | 'no_saving'; short: number; saves: number; selected: boolean
}
export interface AppliedCoupon { id: number; kind: CouponKind; name: string; name_en: string; amount: number; funded_by: 'store' | 'platform' }
export interface OrderDiscount { kind: 'promotion' | CouponKind; funded_by: 'store' | 'platform'; name: string; name_en: string; amount: number; coupon_id: number | null }
export interface RepurchaseRule {
  enabled: boolean; kind: CouponKind; value: number; threshold: number; valid_days: number; min_order: number
  monthly_cap: number | null; currency: string
  stats: { issued: number; used: number; open: number; expired: number; cost: number; sales: number }
}
export interface CouponBatch {
  id: number; issuer: 'store' | 'platform'; branch_id: number | null; kind: CouponKind; source: 'repurchase' | 'claim' | 'new_customer' | 'manual'
  name: string; name_en: string; currency: string; value: number; threshold: number; valid_days: number
  total_quota: number | null; per_customer: number | null; budget: number | null
  country_code: string; country: string; province: string; issued_count: number; status: 'active' | 'paused' | 'ended'
  stats: { issued: number; used: number; open: number; spent: number }
}
export interface ClaimableCoupon { id: number; kind: CouponKind; name: string; name_en: string; value: number; threshold: number; valid_days: number; claimed: boolean; gone: boolean }
export interface CouponCountry { country_code: string; country: string; currency: string; stores: number; provinces: string[] }
export interface PlatformCouponSummary { currency: string; month_spent: number; month_orders: number; open_count: number; open_value: number }
export interface BillDiscount extends OrderDiscount { source: string }
export interface BillOrder {
  id: number; public_no: string; created_at: string; service_type: string; state: 'completed' | 'cancelled' | 'failed' | 'open'
  food: number; delivery_fee: number; promotion: number; store_free: number; store_threshold: number; store_delivery: number
  platform_free: number; paid: number; takings: number; discounts: BillDiscount[]
}
export interface BillList { timezone: string; from: string; to: string; currency: string; orders: BillOrder[]; sum: BillOrder; count: number; shown: number }
export interface Settlement {
  id: number; branch_id: number; store_name: string; store_slug: string; month: string; currency: string; orders: number; amount: number
  status: 'pending' | 'paid'; paid_on: string | null; transfer_ref: string; paid_at: string | null
}
export interface BillSummary {
  from: string; to: string; currency: string; orders: number; cancelled: number; food: number; merchant_discount: number
  platform_discount: number; paid: number; takings: number
  kinds: { kind: 'promotion' | CouponKind; funded_by: 'store' | 'platform'; orders: number; amount: number }[]
  repurchase: { issued: number; used: number; sales: number }
  days: { day: string; orders: number; food: number; discount: number; paid: number; takings: number }[]
  settlements: Settlement[]
}
