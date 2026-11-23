<script setup lang="ts">
// Platform coupons (docs/COUPON_DESIGN.md §7): no-minimum coupons the platform
// pays for, kept to one country (or one province of it). New-customer batches
// go to everyone who signs up with a phone number of that country; manual
// batches are given by phone number, or to everyone of that country.
import { computed, onMounted, reactive, ref } from 'vue'
import { get, post, put } from '../../api'
import { countryName, couponTitle, regionName } from '../../coupons'
import { extend, money, t, toMajor, toMinor } from '../../i18n'
import type { CouponBatch, CouponCountry, PlatformCouponSummary } from '../../types'

extend({
  'apc.intro': ['平台无门槛券由平台出钱：顾客少付的钱，月底由平台补给商家。每张券只能在所选国家（或省份）的店铺使用。', 'The platform pays for these: what the customer does not pay, the platform pays the store at month end. A coupon works only at stores in its country (or province).'],
  'apc.new': ['新建平台券', 'New platform coupon'], 'apc.edit': ['修改平台券', 'Edit platform coupon'],
  'apc.name': ['名称（顾客看到）', 'Name (customers see it)'], 'apc.name_en': ['英文名称', 'English name'],
  'apc.country': ['国家/地区', 'Country'], 'apc.province': ['省份', 'Province'], 'apc.whole': ['全国', 'Whole country'],
  'apc.no_country': ['还没有店铺填了所在国家，先让商家在配送地图里定位店铺。', 'No store has a country yet: stores get one when they set their location on the delivery map.'],
  'apc.currency': ['金额单位跟随该国店铺：{c}', 'Amounts are in the money of its stores: {c}'],
  'apc.value': ['减多少', 'Amount off'], 'apc.days': ['有效期（天）', 'Valid for (days)'],
  'apc.source': ['怎么发', 'How it is given'], 'apc.src.new_customer': ['新人券：新注册顾客自动领取', 'New customers: given on sign-up'],
  'apc.src.manual': ['手动发放', 'By hand'],
  'apc.budget': ['预算上限（总金额）', 'Budget (total)'], 'apc.budget_h': ['发出的券面额合计不超过它；空着表示不限', 'Coupons given add up to no more than this; empty means no limit'],
  'apc.region': ['使用范围', 'Where'], 'apc.given': ['已发', 'Given'], 'apc.used': ['已用', 'Used'], 'apc.spent': ['平台已出', 'Platform paid'],
  'apc.st.active': ['发放中', 'Running'], 'apc.st.paused': ['已暂停', 'Paused'], 'apc.st.ended': ['已结束', 'Ended'],
  'apc.pause': ['暂停', 'Pause'], 'apc.resume': ['继续', 'Resume'], 'apc.end': ['结束', 'End'],
  'apc.end_confirm': ['结束后不能再发，已发出的券到期前仍可用。确定结束“{name}”？', 'Once ended it cannot be given again; coupons already given stay usable. End "{name}"?'],
  'apc.issue': ['发放', 'Give out'], 'apc.issue_title': ['发放“{name}”', 'Give out "{name}"'],
  'apc.phones': ['手机号，每行一个（带国家区号，如 +639171234567）', 'Phone numbers, one a line (with the country code, e.g. +639171234567)'],
  'apc.all': ['发给该国家所有顾客（按手机号区号）', 'Everyone with a phone number of that country'],
  'apc.issued': ['发出 {given} 张，跳过 {skipped} 位（已领过或预算用完）', 'Gave {given}; skipped {skipped} (already had one, or the budget ran out)'],
  'apc.not_found': ['没找到这些号码：{list}', 'No customer with: {list}'],
  'apc.month': ['本月平台已出（已完成订单）', 'Platform paid this month (completed orders)'], 'apc.orders': ['{n} 单', '{n} orders'],
  'apc.open': ['未用的券 {n} 张，面额合计 {amt}', '{n} unused coupons worth {amt}'],
  'apc.none': ['还没有平台券。', 'No platform coupons yet.'],
})

const view = ref<{ batches: CouponBatch[]; summary: PlatformCouponSummary[]; countries: CouponCountry[] } | null>(null)
const error = ref(''), note = ref('')
async function load() {
  try { view.value = await get('/admin/coupons'); error.value = '' } catch (e) { error.value = (e as Error).message }
}
onMounted(load)
async function run(fn: () => Promise<unknown>) {
  error.value = ''; note.value = ''
  try { await fn(); await load(); return true } catch (e) { error.value = (e as Error).message; return false }
}

// ---- the form ----
const blank = () => ({ id: 0, name: '', name_en: '', country_code: '', province: '', value: 10 as number | string, valid_days: 30 as number | string,
  source: 'new_customer' as 'new_customer' | 'manual', budget: '' as number | string })
const f = reactive(blank())
const editing = ref(false)
function edit(b: CouponBatch | null) {
  Object.assign(f, b ? { id: b.id, name: b.name, name_en: b.name_en, country_code: b.country_code, province: b.province, value: toMajor(b.value),
    valid_days: b.valid_days, source: b.source === 'manual' ? 'manual' : 'new_customer', budget: b.budget == null ? '' : toMajor(b.budget) }
    : { ...blank(), country_code: view.value?.countries[0]?.country_code ?? '' })
  editing.value = true
}
const country = computed(() => view.value?.countries.find(c => c.country_code === f.country_code))
async function save() {
  const body = { name: f.name, name_en: f.name_en, country_code: f.country_code, province: f.province, value: toMinor(f.value),
    valid_days: Number(f.valid_days), source: f.source, budget: f.budget === '' ? null : toMinor(f.budget) }
  if (await run(() => f.id ? put(`/admin/coupons/${f.id}`, body) : post('/admin/coupons', body))) editing.value = false
}
const status = (b: CouponBatch, to: string) => {
  if (to === 'ended' && !confirm(t('apc.end_confirm', { name: b.name }))) return
  void run(() => post(`/admin/coupons/${b.id}/status`, { status: to }))
}
const region = (b: { country_code: string; country: string; province: string }) => b.province ? regionName(b) : `${regionName(b)} · ${t('apc.whole')}`

// ---- giving out ----
const giving = ref<CouponBatch | null>(null)
const g = reactive({ phones: '', all: false })
async function give() {
  const b = giving.value!
  let r: { given: number; skipped: number; not_found: string[] } | null = null
  const ok = await run(async () => {
    r = await post(`/admin/coupons/${b.id}/issue`, { all: g.all, phones: g.all ? [] : g.phones.split(/[\s,;]+/).filter(Boolean) })
  })
  const res = r as { given: number; skipped: number; not_found: string[] } | null
  if (ok && res) {
    note.value = t('apc.issued', { given: res.given, skipped: res.skipped }) + (res.not_found.length ? ' ' + t('apc.not_found', { list: res.not_found.join(', ') }) : '')
    giving.value = null; g.phones = ''; g.all = false
  }
}
</script>

<template>
  <div class="adm-page" data-testid="admin-coupons">
    <div class="banner info">{{ t('apc.intro') }}</div>
    <div v-if="error" class="banner bad" data-testid="apc-error">{{ error }}</div>
    <div v-if="note" class="banner ok" data-testid="apc-note">{{ note }}</div>
    <template v-if="view">
      <div v-if="view.summary.length" class="sums">
        <div v-for="s in view.summary" :key="s.currency" class="card sum" data-testid="apc-summary">
          <span class="small muted">{{ t('apc.month') }}</span>
          <b>{{ money(s.month_spent, s.currency) }}</b><span class="small muted">{{ t('apc.orders', { n: s.month_orders }) }}</span>
          <span class="small">{{ t('apc.open', { n: s.open_count, amt: money(s.open_value, s.currency) }) }}</span>
        </div>
      </div>

      <div class="card stack">
        <div class="row between"><h2>{{ t('ad.coupons') }}</h2>
          <button class="btn primary" data-testid="apc-new" :disabled="!view.countries.length" @click="edit(null)">{{ t('apc.new') }}</button></div>
        <div v-if="!view.countries.length" class="banner warn">{{ t('apc.no_country') }}</div>

        <form v-if="editing" class="bform" data-testid="apc-form" @submit.prevent="save">
          <h3 class="full">{{ t(f.id ? 'apc.edit' : 'apc.new') }}</h3>
          <label class="field"><span>{{ t('apc.country') }}</span>
            <select v-model="f.country_code" data-testid="apc-country" @change="f.province = ''">
              <option v-for="c in view.countries" :key="c.country_code" :value="c.country_code">{{ countryName(c.country_code, c.country) }}</option></select></label>
          <label class="field"><span>{{ t('apc.province') }}</span>
            <select v-model="f.province" data-testid="apc-province"><option value="">{{ t('apc.whole') }}</option>
              <option v-for="p in country?.provinces ?? []" :key="p" :value="p">{{ p }}</option></select></label>
          <label class="field"><span>{{ t('apc.name') }}</span><input v-model="f.name" maxlength="60" required data-testid="apc-name" /></label>
          <label class="field"><span>{{ t('apc.name_en') }}</span><input v-model="f.name_en" maxlength="60" /></label>
          <label class="field"><span>{{ t('apc.value') }}</span><input v-model="f.value" type="number" min="0.01" step="0.01" required data-testid="apc-value" /></label>
          <label class="field"><span>{{ t('apc.days') }}</span><input v-model="f.valid_days" type="number" min="1" max="365" required data-testid="apc-days" /></label>
          <label class="field"><span>{{ t('apc.source') }}</span>
            <select v-model="f.source" data-testid="apc-source"><option value="new_customer">{{ t('apc.src.new_customer') }}</option><option value="manual">{{ t('apc.src.manual') }}</option></select></label>
          <label class="field"><span>{{ t('apc.budget') }}</span><input v-model="f.budget" type="number" min="0" step="0.01" placeholder="—" /><small class="muted">{{ t('apc.budget_h') }}</small></label>
          <div v-if="country" class="small muted full">{{ t('apc.currency', { c: country.currency }) }} ·
            {{ couponTitle({ kind: 'platform_free', value: toMinor(f.value), threshold: 0 }, country.currency) }}</div>
          <div class="row full" style="gap:8px"><button class="btn primary" data-testid="apc-save">{{ t('save') }}</button>
            <button type="button" class="btn" @click="editing = false">{{ t('cancel') }}</button></div>
        </form>

        <div class="table-wrap"><table v-if="view.batches.length" class="data">
          <thead><tr><th>{{ t('apc.name') }}</th><th>{{ t('apc.region') }}</th><th>{{ t('apc.source') }}</th><th class="right">{{ t('apc.given') }}</th>
            <th class="right">{{ t('apc.used') }}</th><th class="right">{{ t('apc.spent') }}</th><th>{{ t('ad.status') }}</th><th></th></tr></thead>
          <tbody><tr v-for="b in view.batches" :key="b.id" data-testid="apc-row">
            <td><b>{{ b.name }}</b><div class="small muted">{{ couponTitle(b, b.currency) }} · {{ t('cr.days_valid', { n: b.valid_days }) }}</div></td>
            <td data-testid="apc-region">{{ region(b) }}</td>
            <td class="small">{{ t('apc.src.' + b.source) }}<div v-if="b.budget != null" class="muted">{{ t('apc.budget') }} {{ money(b.budget, b.currency) }}</div></td>
            <td class="right">{{ b.stats.issued }}</td><td class="right">{{ b.stats.used }}</td><td class="right">{{ money(b.stats.spent, b.currency) }}</td>
            <td><span class="tag" :class="{ ok: b.status === 'active' }">{{ t('apc.st.' + b.status) }}</span></td>
            <td class="right nowrap">
              <template v-if="b.status !== 'ended'">
                <button v-if="b.source === 'manual' && b.status === 'active'" class="btn small primary" data-testid="apc-give" @click="giving = b">{{ t('apc.issue') }}</button>
                <button class="btn small" @click="edit(b)">{{ t('edit') }}</button>
                <button v-if="b.status === 'active'" class="btn small" @click="status(b, 'paused')">{{ t('apc.pause') }}</button>
                <button v-else class="btn small" @click="status(b, 'active')">{{ t('apc.resume') }}</button>
                <button class="btn small danger" @click="status(b, 'ended')">{{ t('apc.end') }}</button>
              </template>
            </td>
          </tr></tbody>
        </table><div v-else class="muted">{{ t('apc.none') }}</div></div>

        <form v-if="giving" class="stack give" data-testid="apc-give-form" @submit.prevent="give">
          <h3>{{ t('apc.issue_title', { name: giving.name }) }} · {{ region(giving) }}</h3>
          <label class="check"><input v-model="g.all" type="checkbox" data-testid="apc-all" />{{ t('apc.all') }}</label>
          <label v-if="!g.all" class="field"><span>{{ t('apc.phones') }}</span><textarea v-model="g.phones" rows="4" data-testid="apc-phones"></textarea></label>
          <div class="row" style="gap:8px"><button class="btn primary" data-testid="apc-give-go">{{ t('apc.issue') }}</button>
            <button type="button" class="btn" @click="giving = null">{{ t('cancel') }}</button></div>
        </form>
      </div>
    </template>
  </div>
</template>

<style scoped>
.sums { display: grid; grid-template-columns: repeat(auto-fit, minmax(240px, 1fr)); gap: 12px; margin-bottom: 12px; }
.sum { display: flex; flex-direction: column; gap: 2px; } .sum b { font-size: 24px; font-variant-numeric: tabular-nums; }
.bform { display: grid; grid-template-columns: repeat(auto-fit, minmax(220px, 1fr)); gap: 0 14px; border-top: 1px solid var(--line); padding-top: 14px; }
.bform .full { grid-column: 1 / -1; margin: 0 0 8px; }
.give { border-top: 1px solid var(--line); padding-top: 14px; } .give h3 { margin: 0; }
.give textarea { width: 100%; max-width: 520px; }
.table-wrap { overflow-x: auto; }
.nowrap { white-space: nowrap; } .nowrap .btn { margin-left: 6px; }
</style>
