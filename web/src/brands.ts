// One store, several brands (docs/MULTI_BRAND_DESIGN.md): the brands the
// signed-in person works on in the current store, the one picked on a page,
// and the texts. A store that never added a brand has only its main brand,
// and pages show no brand picker or tags then.
import { computed, ref, watch } from 'vue'
import { extend } from './i18n'
import { currentRole } from './session'
import type { BrandRef } from './types'

extend({
  'br.nav': ['品牌', 'Brands'],
  'br.title': ['品牌', 'Brands'],
  'br.sub': ['一个门店 = 一个厨房；下面挂一个堂食品牌和多个外卖品牌', 'One store is one kitchen, with one dine-in brand and several delivery brands'],
  'br.store_note': ['厨房、员工、桌台、骑手、钱箱、配送范围由各品牌共用', 'The kitchen, staff, tables, riders, cash drawer and delivery area are shared by the brands'],
  'br.add': ['新建外卖品牌', 'New delivery brand'],
  'br.edit': ['编辑品牌', 'Edit brand'],
  'br.main': ['主品牌', 'Main brand'],
  'br.code': ['品牌代码', 'Brand code'],
  'br.code_hint': ['2–8 位字母或数字，全平台唯一；代客单号以它开头', '2–8 letters or digits, unique on the platform; staff order numbers start with it'],
  'br.preview': ['代客单号预览：{no}，同日第二单 {no2}', 'Staff order numbers: {no}, the second that day {no2}'],
  'br.name': ['品牌名', 'Brand name'],
  'br.name_en': ['英文名', 'English name'],
  'br.color': ['标签颜色', 'Tag colour'],
  'br.color_hint': ['后台订单、后厨、地图上用这个颜色', 'Used on orders, the kitchen screen and maps'],
  'br.kind': ['类型', 'Takes'],
  'br.dine': ['堂食', 'Dine-in'],
  'br.delivery': ['外卖 / 自取', 'Delivery / pickup'],
  'br.dine_taken': ['本店已有堂食品牌', 'The store already has a dine-in brand'],
  'br.hours': ['营业时间', 'Hours'],
  'br.hours_store': ['跟门店', 'Same as the store'],
  'br.hours_own': ['单独设', 'Own hours'],
  'br.mall': ['在河马商城显示为一家独立的店', 'Show it in the mall as a shop of its own'],
  'br.paused': ['暂停接单', 'Paused'],
  'br.open': ['营业中', 'Open'],
  'br.disabled': ['已停用', 'Switched off'],
  'br.disable': ['停用品牌', 'Switch off'],
  'br.enable': ['重新启用', 'Switch on'],
  'br.disable_ask': ['停用后顾客看不到这个品牌、不能下单；订单、会员、账单都保留。确定停用？', 'Customers will no longer see or order from this brand; its orders, members and bills are kept. Switch it off?'],
  'br.today': ['今日 {n} 单 · {s}', 'Today {n} orders · {s}'],
  'br.site': ['专属网址', 'Own page'],
  'br.rules': ['一个门店最多 1 个堂食品牌；外卖品牌最多 {n} 个。品牌代码全平台唯一，改代码只影响以后的单。有订单的品牌只能停用不能删，主品牌不能停用。', 'A store has at most one dine-in brand and {n} delivery brands. Brand codes are unique on the platform and a new code only applies to new orders. Brands are switched off, not deleted; the main brand stays on.'],
  'br.save': ['保存品牌', 'Save brand'],
  'br.pick': ['品牌', 'Brand'],
  'br.all': ['全部品牌（合并）', 'All brands (together)'],
  'br.staff': ['可管品牌', 'Brands'],
  'br.staff_all': ['全部品牌', 'All brands'],
  'br.staff_hint': ['限定后：下单、会员、券、活动、账单只出现这些品牌；后厨和骑手不受限。', 'Limited: ordering, members, coupons, promotions and bills show only these brands; the kitchen and riders are not limited.'],
  'br.members_note': ['会员按品牌独立：同一个电话在不同品牌是两个会员。', 'Members are kept per brand: one phone number is a separate member in each brand.'],
  'br.err.code_taken': ['这个品牌代码已被使用', 'This brand code is taken'],
  'br.err.code_invalid': ['品牌代码是 2–8 位字母或数字', 'A brand code is 2 to 8 letters or digits'],
  'br.err.dine_taken': ['本店已有堂食品牌', 'The store already has a dine-in brand'],
  'br.err.limit': ['外卖品牌已到上限（{n} 个）', 'The store has the most delivery brands it may have ({n})'],
  'br.err.main': ['主品牌不能停用', 'The main brand cannot be switched off'],
})

// brandsHere are the brands the person works on in the current store, main first.
export const brandsHere = (): BrandRef[] => currentRole()?.brands ?? []
// multiBrand: the store has more than one brand, so pages show brand pickers and tags.
export const multiBrand = () => brandsHere().length > 1
export const deliveryBrands = () => brandsHere().filter(b => b.active && b.delivery)
export const brandById = (id: number | null | undefined) => brandsHere().find(b => b.id === id) ?? null

// useBrandPick is the brand a page works on, remembered per page on this
// device; 0 is "every brand together" where allowAll is set.
export function useBrandPick(key: string, list: () => BrandRef[] = brandsHere, allowAll = false) {
  const saved = Number(localStorage.getItem('brand-' + key) || 0)
  const pick = ref<number>(0)
  const fix = () => {
    const ids = list().map(b => b.id)
    if (allowAll && pick.value === 0) return
    if (!ids.includes(pick.value)) pick.value = ids.includes(saved) ? saved : allowAll ? 0 : ids[0] ?? 0
  }
  pick.value = saved
  fix()
  watch(() => list().map(b => b.id).join(','), fix)
  watch(pick, v => { try { localStorage.setItem('brand-' + key, String(v)) } catch { /* private window */ } })
  const brand = computed(() => list().find(b => b.id === pick.value) ?? null)
  return { pick, brand }
}
