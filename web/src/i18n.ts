import { ref } from 'vue'

export type Locale = 'zh' | 'en'
export const locale = ref<Locale>((localStorage.getItem('locale') as Locale) || 'zh')

export function setLocale(l: Locale) {
  locale.value = l
  localStorage.setItem('locale', l)
  document.documentElement.lang = l === 'zh' ? 'zh-CN' : 'en'
}

// [zh, en]
const dict: Record<string, [string, string]> = {
  delivery: ['外卖', 'Delivery'], pickup: ['自取', 'Pickup'],
  menu: ['点菜', 'Menu'], store_info: ['门店', 'Store'],
  closed: ['休息中', 'Closed'], paused: ['暂停接单', 'Not taking orders'], open: ['营业中', 'Open'],
  hours: ['营业时间', 'Hours'], all_day: ['全天', 'All day'], address: ['地址', 'Address'], phone: ['电话', 'Phone'],
  sold_out: ['已售罄', 'Sold out'], add: ['加入', 'Add'], choose: ['选规格', 'Options'],
  required: ['必选', 'Required'], optional: ['可选', 'Optional'], pick_n: ['选 {n} 项', 'Choose {n}'], up_to_n: ['最多 {n} 项', 'Up to {n}'],
  note: ['备注', 'Note'], note_ph: ['口味、忌口等', 'Allergies, preferences…'],
  add_to_cart: ['加入购物车', 'Add to cart'], cart: ['购物车', 'Cart'], cart_empty: ['购物车是空的', 'Your cart is empty'],
  clear: ['清空', 'Clear'], clear_confirm: ['确定清空购物车？', 'Clear the cart?'], checkout: ['去结算', 'Checkout'],
  unavailable: ['已下架或售罄', 'No longer available'], remove: ['移除', 'Remove'],
  subtotal: ['菜品小计', 'Subtotal'], packaging_fee: ['包装费', 'Packaging'], delivery_fee: ['配送费', 'Delivery fee'],
  service_charge: ['服务费', 'Service charge'], tax: ['税', 'Tax'], tax_included: ['含税', 'Tax included'],
  total: ['应付', 'Total'], deliver_to: ['配送区域', 'Delivery area'], choose_zone: ['请选择配送区域', 'Choose a delivery area'],
  min_order: ['起送', 'Min. order'], below_min: ['还差 {amt} 起送', '{amt} more to reach the minimum'],
  your_name: ['联系人', 'Name'], street: ['详细地址', 'Street address'], unit: ['门牌 / 楼层', 'Unit / floor'],
  address_note: ['配送说明', 'Delivery instructions'], order_note: ['订单备注', 'Order note'],
  pay_method: ['付款方式', 'Payment'], cod: ['货到付款（现金）', 'Cash on delivery'], pay_at_pickup: ['到店付款', 'Pay at pickup'],
  verify_phone: ['验证手机号', 'Verify your phone'], verify_hint: ['下单前需要验证手机号，方便骑手联系你。', 'We verify your number so the rider can reach you.'],
  phone_number: ['手机号', 'Phone number'], send_code: ['发送验证码', 'Send code'], code: ['验证码', 'Code'], verify: ['验证', 'Verify'],
  verified_as: ['已验证', 'Verified'], dev_code: ['演示模式验证码', 'Demo code'],
  review_amount: ['核对金额', 'Review amount'], place_order: ['提交订单', 'Place order'], placing: ['提交中…', 'Placing…'],
  amount_changed: ['价格或费用有变化，请核对新的金额后再提交。', 'Prices or fees changed. Please review the new amount.'],
  back_to_menu: ['返回菜单', 'Back to menu'], back: ['返回', 'Back'],
  order_no: ['订单号', 'Order'], pickup_code: ['取餐号', 'Pickup code'], order_summary: ['订单明细', 'Order summary'],
  cancel_order: ['取消订单', 'Cancel order'], cancel_confirm: ['确定取消这张订单？', 'Cancel this order?'],
  contact_store: ['联系门店', 'Call the store'], rider: ['骑手', 'Rider'], unpaid: ['未付款', 'Unpaid'], paid: ['已付款', 'Paid'],
  offline: ['连接已断开，显示的内容可能不是最新', 'Connection lost. This may be out of date.'],
  synced: ['同步于', 'Synced'], loading: ['加载中…', 'Loading…'], not_found: ['没有找到', 'Not found'], retry: ['重试', 'Retry'],
  // display status
  's.pending_acceptance': ['等待商家接单', 'Waiting for the store to accept'], 's.preparing': ['备餐中', 'Preparing'],
  's.awaiting_rider': ['已打包，等待骑手取餐', 'Packed, waiting for the rider'], 's.out_for_delivery': ['配送中', 'Out for delivery'],
  's.rider_arrived': ['骑手已到达', 'Rider has arrived'], 's.delivered': ['已送达', 'Delivered'],
  's.ready_for_pickup': ['可以取餐了', 'Ready for pickup'], 's.picked_up': ['已取餐', 'Picked up'],
  's.delivery_failed': ['配送异常，门店会联系你', 'Delivery problem. The store will contact you.'],
  's.rejected': ['商家未接单', 'The store declined the order'], 's.cancelled': ['已取消', 'Cancelled'],
  'r.customer_cancelled': ['顾客取消', 'Cancelled by customer'], 'r.accept_timeout': ['商家超时未接单', 'Not accepted in time'],
  // staff
  sign_in: ['登录', 'Sign in'], username: ['用户名', 'Username'], password: ['密码', 'Password'], sign_out: ['退出', 'Sign out'],
  bad_login: ['用户名或密码不对', 'Wrong username or password'],
  orders: ['订单', 'Orders'], menu_admin: ['菜单', 'Menu'], settings: ['设置', 'Settings'], cash: ['核款', 'Cash'], kitchen: ['后厨', 'Kitchen'],
  active: ['进行中', 'Active'], history: ['历史', 'History'], no_orders: ['暂无订单', 'No orders'],
  accept: ['接单', 'Accept'], reject: ['拒单', 'Reject'], cancel: ['取消', 'Cancel'], reason: ['原因', 'Reason'],
  reason_required: ['请填写原因', 'Please give a reason'], assign_rider: ['指派骑手', 'Assign rider'], reassign: ['改派', 'Reassign'],
  handoff_rider: ['确认交给骑手', 'Hand to rider'], handoff_customer: ['确认顾客取餐并收款', 'Customer picked up & paid'],
  print: ['打印小票', 'Print ticket'], waiting: ['已等', 'Waiting'], min: ['分钟', 'min'], customer: ['顾客', 'Customer'],
  'a.pending': ['待接单', 'Pending'], 'a.accepted': ['已接单', 'Accepted'], 'a.rejected': ['已拒单', 'Rejected'], 'a.cancelled': ['已取消', 'Cancelled'],
  'p.none': ['—', '—'], 'p.waiting_items': ['待齐菜', 'Waiting for items'], 'p.ready_to_pack': ['待打包', 'Ready to pack'], 'p.packed': ['已打包', 'Packed'],
  'f.none': ['未出餐', 'In store'], 'f.awaiting_handoff': ['待交骑手', 'Awaiting rider'], 'f.out_for_delivery': ['配送中', 'Out for delivery'],
  'f.arrived': ['骑手已到达', 'Rider arrived'], 'f.delivered': ['已送达', 'Delivered'], 'f.delivery_failed': ['配送异常', 'Delivery failed'],
  'f.awaiting_pickup': ['待顾客取餐', 'Awaiting pickup'], 'f.picked_up': ['已取餐', 'Picked up'],
  'i.pending': ['待制作', 'To do'], 'i.preparing': ['制作中', 'Cooking'], 'i.ready': ['已备好', 'Ready'], 'i.cancelled': ['已取消', 'Cancelled'],
  start: ['开始', 'Start'], ready: ['备好', 'Ready'], undo: ['撤回', 'Undo'], no_prep: ['免制作', 'No prep'],
  bags: ['袋数', 'Bags'], confirm_pack: ['确认打包完成', 'Confirm packed'], check_all: ['请逐项核对后再确认', 'Tick every line first'],
  missing: ['缺', 'Missing'], cancelled_notice: ['此单已取消，请停止制作', 'Order cancelled. Stop preparing.'], kitchen_empty: ['没有待处理的订单', 'Nothing to prepare'],
  to_cook: ['制作', 'Cooking'], to_pack: ['打包', 'Packing'], new_order_sound: ['新单提示音', 'New order sound'],
  my_tasks: ['我的配送', 'My deliveries'], no_tasks: ['暂无配送任务', 'No deliveries'], not_packed: ['门店还在备餐', 'Still being prepared'],
  confirm_pickup: ['确认取餐', 'Confirm pickup'], arrived: ['已到达', 'Arrived'], navigate: ['导航', 'Navigate'], call: ['拨打', 'Call'],
  collect: ['应收现金', 'Cash to collect'], collected: ['实收', 'Collected'], confirm_delivered: ['确认送达', 'Confirm delivered'],
  report_problem: ['配送异常', 'Report a problem'], finished: ['已完成', 'Finished'],
  save: ['保存', 'Save'], saved: ['已保存', 'Saved'], edit: ['编辑', 'Edit'], new_item: ['新增菜品', 'New item'], new_category: ['新增分类', 'New category'],
  name_zh: ['中文名', 'Name (Chinese)'], name_en: ['英文名', 'Name (English)'], price: ['价格', 'Price'], category: ['分类', 'Category'],
  description: ['描述', 'Description'], image_url: ['图片链接', 'Image URL'], published: ['已发布', 'Published'], draft: ['草稿', 'Draft'],
  stock: ['限量库存', 'Limited stock'], unlimited: ['不限', 'Unlimited'], needs_prep: ['需要后厨制作', 'Needs kitchen prep'],
  option_groups: ['规格组', 'Option groups'], add_group: ['添加规格组', 'Add group'], add_option: ['添加选项', 'Add option'],
  min_sel: ['最少选', 'Min'], max_sel: ['最多选', 'Max'], extra: ['加价', 'Extra'], delete: ['删除', 'Delete'], archive: ['下架删除', 'Remove'],
  mark_sold_out: ['标记售罄', 'Mark sold out'], restore: ['恢复供应', 'Back in stock'], publish: ['发布', 'Publish'], unpublish: ['转为草稿', 'Unpublish'],
  zones: ['配送区', 'Delivery areas'], fee: ['配送费', 'Fee'], enabled: ['启用', 'Enabled'], new_zone: ['新增配送区', 'New area'],
  store_settings: ['门店设置', 'Store settings'], tax_rate: ['税率 %', 'Tax rate %'], tax_inclusive: ['菜单价已含税', 'Menu prices include tax'],
  service_rate: ['服务费 %', 'Service charge %'], accept_timeout: ['接单超时（分钟）', 'Accept timeout (min)'],
  open_time: ['开始营业', 'Opens'], close_time: ['结束营业', 'Closes'], pause_orders: ['暂停接单', 'Pause ordering'],
  staff: ['员工', 'Staff'], role: ['角色', 'Role'], add_staff: ['添加员工', 'Add staff'], display_name: ['姓名', 'Name'],
  disable: ['停用', 'Disable'], enable: ['启用', 'Enable'], disabled: ['已停用', 'Disabled'],
  'role.owner': ['店主', 'Owner'], 'role.manager': ['店长', 'Manager'], 'role.cashier': ['收银 / 接单', 'Cashier'], 'role.kitchen': ['后厨 / 打包', 'Kitchen'], 'role.rider': ['骑手', 'Rider'],
  returned: ['交回金额', 'Returned'], reconcile: ['确认核款', 'Reconcile'], due: ['应收', 'Due'], diff: ['差额', 'Difference'],
  'c.pending': ['待核款', 'Pending'], 'c.reconciled': ['已核款', 'Reconciled'], 'c.discrepancy': ['有差异', 'Discrepancy'], no_cash: ['暂无代收记录', 'No collections yet'],
  share_link: ['顾客点餐链接', 'Customer link'], merchants: ['商户', 'Merchants'], new_merchant: ['创建商户', 'New merchant'],
  merchant_name: ['商户名', 'Merchant name'], branch_name: ['门店名', 'Store name'], slug: ['链接标识', 'Link slug'], owner_account: ['店主账号', 'Owner account'], create: ['创建', 'Create'],
  stations: ['后厨工作台', 'Kitchen stations'], station: ['工作台', 'Station'], new_station: ['新增工作台', 'New station'],
  stations_hint: ['接单时菜品按这里的归属分到各工作台。停用的工作台不再接新任务，已有任务仍可处理。', 'Items are routed to stations when an order is accepted. A disabled station gets no new work but can finish what it has.'],
  items_n: ['{n} 个菜品', '{n} items'], tasks_n: ['{n} 个在制', '{n} in progress'],
  unassigned: ['未分台', 'Unassigned'], all_stations: ['全部', 'All'],
  unassigned_warn: ['有 {n} 个待制作的菜没有分到工作台，请在这里处理或改派。', '{n} items to prepare have no station. Handle them here or move them.'],
  move_to: ['改派到', 'Move to'],
  notify_sms: ['订单状态短信通知顾客', 'Text customers about order status'],
  redeliver: ['重新配送', 'Send out again'], close_failed: ['结束订单（不再配送）', 'Close order (no redelivery)'],
  failed_hint: ['配送异常：请联系顾客后选择重新配送或结束订单。', 'Delivery failed. Contact the customer, then send it out again or close the order.'],
  choose_rider: ['选择骑手', 'Choose rider'],
  promos: ['活动', 'Promotions'], new_promo: ['新建活动', 'New promotion'], no_promos: ['还没有活动', 'No promotions yet'],
  promos_hint: ['一张订单只用一个活动，系统自动选对顾客最划算的那个。已下的订单保留当时的优惠，不受之后修改影响。', 'One promotion per order: the system picks the one that saves the customer the most. Existing orders keep the discount they got.'],
  promo_name: ['活动', 'Promotion'], promo_rule: ['规则', 'Rule'], promo_when: ['时间', 'When'], promo_usage: ['已用', 'Used'],
  promo_name_ph: ['例如：满300减30', 'e.g. Spend 300, save 30'], promo_kind: ['类型', 'Type'],
  'kind.amount_off': ['满减', 'Amount off'], 'kind.percent_off': ['折扣', 'Percent off'], 'kind.free_delivery': ['免配送费', 'Free delivery'],
  promo_threshold: ['菜品满多少可用', 'Minimum spend'], promo_amount: ['减多少', 'Amount off'], promo_percent: ['优惠百分比（10 表示九折）', 'Percent off (10 = 10% off)'],
  promo_cap: ['最多减', 'Maximum discount'], promo_starts: ['开始日期时间', 'Starts'], promo_ends: ['结束日期时间', 'Ends'],
  promo_daily_start: ['每天开始', 'Daily from'], promo_daily_end: ['每天结束', 'Daily until'],
  promo_quota: ['总名额（单）', 'Total uses'], promo_per_phone: ['每个手机号限用次数', 'Uses per phone number'],
  promo_time_hint: ['每天的开始和结束相同表示全天。结束早于开始表示跨午夜（例如 22:00 到 02:00），按门店当地时间计算。', 'Equal daily times mean all day. An end before the start crosses midnight (e.g. 22:00 to 02:00), in the store\'s local time.'],
  promo_end_confirm: ['结束后不能再启用，确定结束这个活动？', 'An ended promotion cannot be reactivated. End it?'],
  'promo.draft': ['草稿', 'Draft'], 'promo.active': ['进行中', 'Live'], 'promo.paused': ['已暂停', 'Paused'], 'promo.ended': ['已结束', 'Ended'],
  'promo.not_live': ['已发布，当前不在时段或名额已满', 'Published, not live right now'],
  pause: ['暂停', 'Pause'], resume: ['恢复', 'Resume'], end: ['结束', 'End'], daily: ['每天', 'Daily'], always: ['长期', 'Always'],
  per_customer_n: ['每人限 {n} 次', '{n} per customer'],
  trial: ['试算', 'Trial calculation'], trial_run: ['试算', 'Calculate'], trial_wins: ['采用', 'Applied'],
  trial_hint: ['输入一张单的金额，看现在会用哪个活动。草稿和暂停的活动也会参与试算，方便发布前检查。', 'Enter an order amount to see which promotion would apply now. Drafts and paused promotions are included so you can check before publishing.'],
  'why.outside_time': ['不在活动时间', 'Outside its time'], 'why.service_type': ['不适用这种取餐方式', 'Not for this service type'],
  'why.below_threshold': ['未达到门槛', 'Below the minimum spend'], 'why.quota_used_up': ['名额已用完', 'No uses left'],
  'why.customer_limit': ['顾客已达上限', 'Customer limit reached'], 'why.not_active': ['未启用', 'Not active'],
  discount: ['优惠', 'Discount'], more_for_promo: ['再买 {amt} 可享「{name}」', 'Add {amt} more for "{name}"'],
  when: ['时间', 'When'], asap: ['尽快', 'As soon as possible'], today: ['今天', 'Today'], tomorrow: ['明天', 'Tomorrow'],
  deliver_at: ['预约送达', 'Deliver at'], pickup_at: ['预约取餐', 'Pick up at'], scheduled: ['预约', 'Scheduled'],
  schedule_invalid: ['这个时间已经不能预约，请重新选择', 'That time is no longer available. Please choose another.'],
  closed_can_schedule: ['现在休息中，可以预约稍后的时间', 'Closed right now. You can order ahead for later.'],
  pick_time: ['请选择时间', 'Choose a time'],
  's.scheduled': ['已接单，会按预约时间准备', 'Accepted. It will be prepared for your chosen time.'],
  scheduling_enabled: ['接受预约单', 'Take scheduled orders'], prep_minutes: ['备餐提前量（分钟）', 'Preparation lead time (min)'],
  kitchen_from: ['后厨开始', 'Kitchen starts'],
  import_excel: ['导入 Excel', 'Import Excel'], download_template: ['下载模板', 'Download template'], upload: ['上传', 'Upload'],
  import_hint: ['按模板填写后上传。新菜品会先存为草稿，同分类同名的菜品会更新价格等信息。规格在导入后到菜品里编辑。', 'Fill in the template and upload it. New dishes are saved as drafts; a dish with the same category and name is updated. Options are edited on the dish afterwards.'],
  import_parsing: ['正在解析…', 'Parsing…'], import_failed: ['文件无法导入', 'The file could not be imported'],
  import_summary: ['共 {total} 行，{valid} 行可导入，{bad} 行有错误', '{total} rows: {valid} can be imported, {bad} have errors'],
  import_apply: ['导入 {n} 行', 'Import {n} rows'], import_done: ['已导入：新增 {c} 个草稿，更新 {u} 个', 'Imported: {c} new drafts, {u} updated'],
  import_line: ['行', 'Row'], import_result: ['结果', 'Result'], 'act.create': ['新增（草稿）', 'New (draft)'], 'act.update': ['更新', 'Update'],
  'ie.category_required': ['缺少分类', 'Category missing'], 'ie.name_required': ['缺少菜名', 'Name missing'],
  'ie.price_required': ['缺少价格', 'Price missing'], 'ie.price_invalid': ['价格格式不对', 'Invalid price'],
  'ie.station_unknown': ['工作台不存在或已停用', 'Unknown or disabled station'], 'ie.yes_no_invalid': ['是/否 填写不对', 'Use yes or no'],
  'ie.stock_invalid': ['库存格式不对', 'Invalid stock'], 'ie.text_too_long': ['文字太长', 'Text too long'],
  'ie.duplicate_of_line': ['和第 {n} 行重复', 'Duplicate of row {n}'],
  conflict: ['订单已被其他人更新，已刷新', 'Someone else updated this order. Refreshed.'],
}

export function t(key: string, vars: Record<string, string | number> = {}): string {
  const e = dict[key]
  let s = e ? e[locale.value === 'zh' ? 0 : 1] : key
  for (const k in vars) s = s.replace(`{${k}}`, String(vars[k]))
  return s
}

// pick shows the name in the current language, falling back to the other one.
export function pick(zh: string, en: string): string {
  return locale.value === 'en' ? en || zh : zh || en
}

// both shows "中文 / English" for staff screens where the team is mixed.
export function both(zh: string, en: string): string {
  return en && en !== zh ? `${zh} / ${en}` : zh
}

const symbols: Record<string, string> = { PHP: '₱', USD: '$', CNY: '¥', SGD: 'S$', MYR: 'RM', THB: '฿' }

export function money(minor: number, currency = 'PHP'): string {
  const v = minor / 100
  const s = Number.isInteger(v) ? String(v) : v.toFixed(2)
  return (symbols[currency] ?? currency + ' ') + s.replace(/\B(?=(\d{3})+(?!\d))/g, ',')
}

// promoLabel describes a promotion's rule in the current language.
export function promoLabel(p: { kind: string; threshold: number; value: number; max_discount: number | null }, currency: string): string {
  const zh = locale.value === 'zh'
  const th = p.threshold > 0 ? money(p.threshold, currency) : ''
  let what = ''
  if (p.kind === 'amount_off') what = zh ? `减${money(p.value, currency)}` : `${money(p.value, currency)} off`
  else if (p.kind === 'percent_off') {
    const pct = p.value / 100
    what = zh ? `${(100 - pct) / 10}折` : `${pct}% off`
    if (p.max_discount) what += zh ? `（最多减${money(p.max_discount, currency)}）` : ` (up to ${money(p.max_discount, currency)})`
  } else what = zh ? '免配送费' : 'Free delivery'
  if (!th) return what
  return zh ? `满${th}${what}` : `${what} over ${th}`
}

// whenText shows a scheduled time as "今天 18:30" / "Tomorrow 11:00", in the
// given time zone (the store's for customers; the device's for staff).
export function whenText(iso: string, timeZone?: string): string {
  const d = new Date(iso)
  const day = (x: Date) => x.toLocaleDateString('en-CA', { timeZone })
  const time = d.toLocaleTimeString('en-GB', { timeZone, hour: '2-digit', minute: '2-digit' })
  const today = day(new Date()), tomorrow = day(new Date(Date.now() + 86400000))
  const label = day(d) === today ? t('today') : day(d) === tomorrow ? t('tomorrow')
    : d.toLocaleDateString(locale.value === 'zh' ? 'zh-CN' : 'en-US', { timeZone, month: 'short', day: 'numeric' })
  return `${label} ${time}`
}

// importError turns a row error code from the server into text.
export function importError(code: string): string {
  const [k, n] = code.split(':')
  return dict['ie.' + k] ? t('ie.' + k, { n: n ?? '' }) : code
}

// Staff type amounts in major units; the API wants minor units.
export const toMinor = (v: number | string) => Math.round(Number(v || 0) * 100)
export const toMajor = (v: number) => v / 100

export function reasonText(r: string): string {
  const k = 'r.' + r
  return dict[k] ? t(k) : r
}

export function minutesSince(iso: string): number {
  return Math.max(0, Math.floor((Date.now() - new Date(iso).getTime()) / 60000))
}
