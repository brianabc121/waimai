// The customer's address book and what a store says about delivering to a spot.
import { del, get, post, put, upload } from './api'
import { prettyPhone } from './dial'
import { extend, money, t } from './i18n'

// What a customer is told about delivery to one spot. Which of the store's
// areas it falls in is not part of it.
export interface DeliveryTerms { deliverable: boolean; fee: number; min_order: number; eta_minutes: number | null }

export interface Address {
  id: number; contact_name: string; title: '' | 'mr' | 'ms'; phone: string
  place_name: string; place_address: string; lat: number | null; lng: number | null
  unit: string; note: string; wechat: string; telegram: string; line: string; whatsapp: string
  label: '' | 'home' | 'work' | 'school'; is_default: boolean; last_used_at: string | null; photo_at: string | null
  delivery?: DeliveryTerms | null
}
export type AddressInput = Omit<Address, 'id' | 'last_used_at' | 'delivery' | 'photo_at'>

export const listAddresses = (slug?: string) =>
  get<{ addresses: Address[]; limit: number }>('/customer/addresses' + (slug ? `?store=${encodeURIComponent(slug)}` : ''))
export const saveAddress = (id: number, a: AddressInput) => id ? put<Address>(`/customer/addresses/${id}`, a) : post<Address>('/customer/addresses', a)
export const deleteAddress = (id: number) => del(`/customer/addresses/${id}`)
export const photoUrl = (a: Address) => `/api/v1/customer/addresses/${a.id}/photo?v=${encodeURIComponent(a.photo_at ?? '')}`
export const savePhoto = (id: number, photo: Blob) => upload<Address>(`/customer/addresses/${id}/photo`, new File([photo], 'door.jpg', { type: 'image/jpeg' }))
export const deletePhoto = (id: number) => del(`/customer/addresses/${id}/photo`)
export const termsAt = (slug: string, lat: number, lng: number) => post<DeliveryTerms>(`/stores/${slug}/locate`, { lat, lng })

// An address this store cannot deliver to cannot be chosen for an order.
export const usable = (a: Address) => !a.delivery || a.delivery.deliverable
// Those the store delivers to come first; the server already put the default
// and the recently used ones ahead.
export const sorted = (list: Address[]) => [...list].sort((a, b) => Number(usable(b)) - Number(usable(a)))

// Who an order is for, as staff read it: the name with Mr / Ms, and the number to call.
interface Contact { customer_name: string; customer_phone: string; contact_phone?: string; contact_title?: string }
// "陈先生" who also ticked Mr is not called 陈先生先生.
const titled = (name: string, title?: string) => { const x = title ? t('ad.title_' + title) : ''; return name.endsWith(x.trim()) ? name : name + x }
export const contactName = (o: Contact) => titled(o.customer_name, o.contact_title)
export const contactPhone = (o: Contact) => o.contact_phone || o.customer_phone
export const whoLine = (a: Address) => `${titled(a.contact_name, a.title)} ${prettyPhone(a.phone)}`

// The other ways to reach a customer that a store may ask for on the address
// form (it chooses which in its settings), in the order they are shown.
export const CONTACT_FIELDS = ['wechat', 'telegram', 'line', 'whatsapp'] as const
export type ContactField = typeof CONTACT_FIELDS[number]
// Those an order came with, as staff read them: [name of the app, the customer's name there].
export const contactApps = (o: Partial<Record<`contact_${ContactField}`, string>>) =>
  CONTACT_FIELDS.filter(f => o[`contact_${f}`]).map(f => ({ field: f, label: t('oc.' + f), value: o[`contact_${f}`]! }))
export const termsText = (d: DeliveryTerms, currency?: string) =>
  d.deliverable ? t('ad.can', { fee: money(d.fee, currency) }) : t('ad.cannot')

extend({
  'ad.choose': ['选择收货地址', 'Choose a delivery address'], 'ad.mine': ['我的地址', 'My addresses'],
  'ad.new': ['新增收货地址', 'Add an address'], 'ad.edit': ['修改收货地址', 'Edit address'], 'ad.edit_short': ['编辑', 'Edit'],
  'ad.empty': ['还没有保存的地址', 'No saved addresses yet'], 'ad.unit_is': ['门牌号：{unit}', 'Unit: {unit}'],
  'ad.can': ['可配送 · 配送费 {fee}', 'We deliver here · fee {fee}'], 'ad.cannot': ['不在本店配送范围', 'This store does not deliver here'],
  'ad.need_pin': ['请在地图上补一个位置', 'Place this address on the map first'],
  'ad.full': ['最多保存 {n} 个地址，请先删除不用的', 'You can keep {n} addresses. Delete one first.'],
  'ad.title_mr': ['先生', ' (Mr)'], 'ad.title_ms': ['女士', ' (Ms)'], 'ad.mr': ['先生', 'Mr'], 'ad.ms': ['女士', 'Ms'],
  'ad.f.name': ['收货人', 'Name'], 'ad.f.name_ph': ['请输入姓名', 'Name'],
  'ad.f.phone': ['联系电话', 'Phone'], 'ad.f.phone_ph': ['请输入手机号码', 'Mobile number'], 'ad.f.dial': ['国家或地区区号', 'Country code'],
  'ad.f.phone_hint': ['默认使用登录手机号，可修改', 'Your login number by default; you can change it'],
  'ad.f.place': ['收货地址', 'Address'], 'ad.f.place_ph': ['在地图上选择收货地址', 'Pick the address on the map'],
  'ad.f.street_ph': ['街道和大楼名称', 'Street and building'],
  'ad.f.unit': ['门牌号', 'Unit'], 'ad.f.unit_ph': ['楼层、房号（详细地址）', 'Floor and unit number'],
  'ad.f.note': ['骑手备注', 'Rider note'], 'ad.f.note_ph': ['如门禁或交接方式', 'Gate code, where to hand over…'],
  'ad.f.wechat': ['微信号', 'WeChat'], 'ad.f.telegram': ['Telegram', 'Telegram'], 'ad.f.line': ['LINE', 'LINE'], 'ad.f.whatsapp': ['WhatsApp', 'WhatsApp'],
  'ad.f.wechat_ph': ['电话打不通时联系', 'If the phone is not answered'], 'ad.f.account_ph': ['请输入账号', 'Your name there'], 'ad.f.whatsapp_ph': ['请输入 WhatsApp 号码', 'Your WhatsApp number'],
  'ad.s.place': ['地址信息', 'Address'], 'ad.s.contact': ['联系人', 'Contact'], 'ad.s.extra': ['配送补充', 'For the rider'], 'ad.s.optional': ['选填', 'optional'],
  'ad.f.label': ['标签', 'Label'], 'ad.label_work': ['公司', 'Work'], 'ad.label_home': ['家', 'Home'], 'ad.label_school': ['学校', 'School'],
  'ad.f.default': ['设为默认地址', 'Make this my default address'],
  'ad.save': ['保存地址', 'Save address'], 'ad.saving': ['保存中…', 'Saving…'], 'ad.delete': ['删除这个地址', 'Delete this address'],
  'ad.delete_confirm': ['确定删除这个地址？', 'Delete this address?'],
  'ad.e.name': ['请填写收货人', 'Enter the name'], 'ad.e.phone': ['请输入正确的 +63 手机号，例如 917 555 0123', 'Enter a valid +63 mobile number, e.g. 917 555 0123'], 'ad.e.phone_any': ['请输入正确的手机号码', 'Enter a valid mobile number'],
  'ad.e.place': ['请选择收货地址', 'Choose the address'], 'ad.e.unit': ['请填写门牌号', 'Enter the unit'],
  'ad.f.photo': ['门口照片', 'Door photo'], 'ad.f.photo_hint': ['方便骑手找到门口', 'Helps the rider find your door'],
  'ad.f.photo_add': ['拍照或选图', 'Take or choose a photo'], 'ad.f.photo_change': ['换一张', 'Replace'], 'ad.f.photo_remove': ['删除照片', 'Remove'],
  'ad.e.photo': ['地址已保存，但照片没有传上去，请稍后在“修改地址”里重试', 'The address is saved, but the photo did not upload. Try again from "Edit address".'],
  'oc.wechat': ['微信', 'WeChat'], 'oc.telegram': ['Telegram', 'Telegram'], 'oc.line': ['LINE', 'LINE'], 'oc.whatsapp': ['WhatsApp', 'WhatsApp'], 'oc.door_photo': ['门口照片', 'Door photo'], 'oc.view': ['查看', 'View'],
  'oc.ordered_by': ['下单账号', 'Ordered by'],
  'co.add_address': ['添加收货地址', 'Add a delivery address'], 'co.title': ['结算', 'Checkout'],
  'co.now_delivery': ['立即配送', 'Deliver now'], 'co.later_delivery': ['预约配送', 'Deliver later'],
  'co.now_pickup': ['尽快自取', 'Pick up as soon as possible'], 'co.later_pickup': ['预约自取', 'Pick up later'],
  'co.eta': ['约 {n} 分钟送达', 'About {n} min'], 'co.need_address': ['请选择地址', 'Choose an address'], 'co.pick_time': ['选择时间', 'Choose a time'],
  'co.closed_now': ['现在打烊，只能预约', 'Closed now: order for later'], 'co.by_store': ['门店配送', 'Delivered by the store'],
  'co.basket': ['购物篮 {n}', 'Basket {n}'], 'co.basket_subtotal': ['购物篮小计', 'Basket subtotal'],
  'co.fee_later': ['选择地址后计算', 'After you choose an address'], 'co.no_fee_yet': ['未含配送费', 'Delivery fee not included'],
  'co.saved': ['已优惠 {amt}', 'You save {amt}'], 'co.short': ['还差 {amt} 起送', '{amt} more to order'],
  'co.svc_delivery': ['配送', 'Delivery'], 'co.svc_pickup': ['到店自取', 'Pickup'],
  'co.pickup_name': ['取餐人', 'Name for pickup'], 'co.account': ['登录手机号', 'Logged in as'], 'co.note_ph': ['口味、餐具等要求（选填）', 'Requests (optional)'],
  'co.deliver_to': ['配送至', 'Deliver to'], 'co.pickup_at': ['自取门店', 'Pick up at'],
  'co.cod': ['货到付款', 'Cash on delivery'], 'co.pay_cash': ['现金支付', 'Pay in cash'],
  'co.address_gone': ['这个地址已经不在了，请重新选择', 'That address is gone. Please choose again.'],
  'pp.title': ['选择位置', 'Choose the location'], 'pp.search': ['搜索大楼、街道或地标', 'Search a building, street or landmark'],
  'pp.results': ['搜索结果', 'Results'], 'pp.nearby': ['图钉附近的地址', 'Near the pin'], 'pp.done': ['完成', 'Done'],
  'pp.drag': ['拖动地图，把图钉对准门口', 'Move the map until the pin is on your door'],
  'pp.none': ['没有找到，换个写法试试，或者直接拖动地图', 'Nothing found. Try other words, or just move the map.'],
  'pp.looking': ['正在查找…', 'Looking…'], 'pp.metres': ['距图钉 {n} 米', '{n} m from the pin'], 'pp.pin_only': ['就用图钉的位置', 'Use the pin itself'],
  'pp.can': ['这个位置可以配送', 'We deliver to this spot'], 'pp.terms': ['配送费 {fee} · 起送 {min}', 'Fee {fee} · min. {min}'],
  'pp.eta': [' · 约 {n} 分钟', ' · ~{n} min'],
  'pp.cannot': ['这个位置不在本店配送范围', 'This store does not deliver to this spot'],
  'pp.cannot_hint': ['可以换一个收货位置，或改为到店自取。', 'Choose another spot, or switch to pickup.'],
  'pp.cannot_keep': ['仍然可以保存这个地址，但这家店的外卖不能选它。', 'You can still save this address, but not order delivery to it from this store.'],
  'pp.checking': ['正在判断能不能送…', 'Checking delivery…'], 'pp.failed': ['暂时无法判断，请稍后再试', 'Could not check right now, please try again'],
  'pp.me': ['用我现在的位置', 'Use my current location'], 'pp.me_denied': ['没有拿到定位，请在地图上手动选位置', 'Location is unavailable; please move the map by hand'],
  'pp.to_pickup': ['改为到店自取', 'Switch to pickup'],
})
