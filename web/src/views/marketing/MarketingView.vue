<script setup lang="ts">
// 营销 → 营销活动 (docs/STORED_VALUE_DESIGN.md §4): the store's top-up
// offers, what customers hold, this month's top-ups, and the recent ones.
import { computed, onMounted, ref } from 'vue'
import { money, t, whenText } from '../../i18n'
import { can, currentRole, session } from '../../session'
import '../../officeStrings'
import ApproveDialog from '../ApproveDialog.vue'
import CampaignEditor from './CampaignEditor.vue'
import {
  acceptStoredTerms, bonusRate, branchTopups, revokeTopup, setCampaignStatus, storedErr, storedOverview, tierCoupon, tierLimits,
  type StoredCampaign, type StoredOverview, type Topup,
} from '../../stored'

const bid = session.branchId!
const data = ref<StoredOverview | null>(null)
const topups = ref<Topup[]>([])
const error = ref(''), notice = ref('')
const editing = ref<StoredCampaign | null | undefined>(undefined)
const revoking = ref<Topup | null>(null)
const isOwner = computed(() => currentRole()?.role === 'owner')
const cur = computed(() => data.value?.currency ?? 'PHP')
const m = (v: number) => money(v, cur.value)

async function load() {
  error.value = ''
  try {
    data.value = await storedOverview(bid)
    topups.value = await branchTopups(bid)
  } catch (e) { error.value = storedErr(e) }
}
onMounted(load)

async function accept() {
  try { await acceptStoredTerms(bid); await load() } catch (e) { error.value = storedErr(e) }
}
async function toggle(c: StoredCampaign) {
  error.value = ''
  try { await setCampaignStatus(bid, c.id, c.status === 'active' ? 'paused' : 'active'); await load() } catch (e) { error.value = storedErr(e) }
}
async function revoke(x: Topup, headers?: Record<string, string>) {
  if (!headers && !confirm(t('sv.revoke_ask'))) return
  if (!headers && !can('stored.cashout')) { revoking.value = x; return }
  error.value = ''
  try { await revokeTopup(bid, x.id, '', headers); notice.value = t('sv.revoked'); await load() } catch (e) { error.value = storedErr(e) }
}
function approved(tok: string) { const x = revoking.value!; revoking.value = null; void revoke(x, { 'X-Approval': tok }) }
const period = (c: StoredCampaign) => !c.starts_at && !c.ends_at ? t('sv.m.always')
  : t('sv.m.period', { from: c.starts_at ? c.starts_at.slice(0, 10) : '…', to: c.ends_at ? c.ends_at.slice(0, 10) : '…' })
const stateCls: Record<string, string> = { running: 'ok', upcoming: 'gold', ended: 'off', paused: 'off' }
function saved() { editing.value = undefined; notice.value = t('sv.m.saved'); void load() }
</script>

<template>
  <div class="ox warm">
  <div class="page ox-page" data-testid="marketing-page">
    <div class="ox-head">
      <div><h1>{{ t('mk.campaigns') }}</h1><p class="ox-sub">{{ t('sv.m.hint') }}</p></div>
      <div class="ox-acts">
        <router-link v-if="can('promo')" class="btn" to="/merchant/promos">{{ t('promos') }} ›</router-link>
        <button v-if="data?.enabled && data.terms_at && can('promo')" class="btn primary" data-testid="mk-new" @click="editing = null">＋ {{ t('sv.m.new') }}</button>
      </div>
    </div>
    <div v-if="error" class="banner bad" data-testid="mk-error">{{ error }}</div>
    <div v-if="notice" class="banner ok" @click="notice = ''">{{ notice }}</div>

    <template v-if="data">
      <div v-if="!data.enabled" class="banner warn" data-testid="mk-off">{{ t('sv.m.off') }}</div>
      <div v-else-if="!data.terms_at" class="ox-tip terms" data-testid="mk-terms">
        <b>{{ t('sv.m.terms_t') }}</b>
        <p>{{ t('sv.m.terms', { cap: m(data.cap) }) }}</p>
        <button v-if="isOwner" class="btn primary" data-testid="mk-terms-ok" @click="accept">{{ t('sv.m.terms_ok') }}</button>
        <span v-else class="small">{{ t('sv.m.terms_owner') }}</span>
      </div>

      <div class="ox-stats">
        <div class="ox-stat"><span class="l">{{ t('sv.m.owed') }}</span><b class="v" data-testid="mk-owed">{{ m(data.principal + data.bonus) }}</b>
          <small class="s">{{ t('sv.m.parts', { p: m(data.principal), b: m(data.bonus) }) }} · {{ t('sv.m.holders', { n: data.holders }) }}</small></div>
        <div class="ox-stat"><span class="l">{{ t('sv.m.month_in') }}</span><b class="v">{{ m(data.month_paid) }}</b>
          <small class="s">{{ t('sv.m.month_n', { n: data.month_count, b: m(data.month_given) }) }}</small></div>
        <div class="ox-stat"><span class="l">{{ t('sv.m.month_spent') }}</span><b class="v">{{ m(data.month_spent_principal + data.month_spent_bonus) }}</b>
          <small class="s">{{ t('sv.m.month_spent_s', { b: m(data.month_spent_bonus) }) }}</small></div>
      </div>

      <section class="ox-panel">
        <div class="ox-panel-h"><h2 class="grow">{{ t('sv.m.campaigns') }}</h2>
          <span class="small muted">{{ t('sv.m.approve_over') }}: {{ m(data.approve_over) }}</span></div>
        <div v-if="!data.campaigns.length" class="ox-empty">{{ t('sv.m.none') }}</div>
        <div v-for="c in data.campaigns" :key="c.id" class="camp" data-testid="mk-campaign">
          <div class="camp-h">
            <b class="nm">{{ c.name }}</b>
            <span class="tag" :class="stateCls[c.state]" data-testid="mk-state">{{ t('sv.st.' + c.state) }}</span>
            <span class="small muted grow">{{ period(c) }}</span>
            <template v-if="can('promo')">
              <button class="btn small" data-testid="mk-edit" @click="editing = c">{{ t('sv.m.edit') }}</button>
              <button v-if="c.state !== 'ended'" class="btn small" data-testid="mk-toggle" @click="toggle(c)">{{ t(c.status === 'active' ? 'sv.m.pause' : 'sv.m.resume') }}</button>
            </template>
          </div>
          <div class="tiers">
            <div v-for="x in c.tiers" :key="x.id" class="tier" data-testid="mk-tier">
              <span v-if="x.label" class="hot">{{ x.label }}</span>
              <div class="pay">{{ t('sv.t.pay', { amt: '' }) }}<b>{{ m(x.pay) }}</b></div>
              <div class="give">{{ t('sv.t.give', { amt: m(x.bonus) }) }} <small v-if="bonusRate(x)">{{ t('sv.t.rate', { pct: bonusRate(x) }) }}</small></div>
              <div v-if="x.coupon_qty" class="cp">{{ tierCoupon(x, cur) }}</div>
              <div v-if="tierLimits(x).length" class="lim">{{ tierLimits(x).join(' · ') }}</div>
            </div>
            <div v-if="c.custom_ok" class="tier other"><div class="pay">{{ t('sv.t.custom', { min: m(c.custom_min) }) }}</div></div>
          </div>
          <div class="small muted">{{ t('sv.m.stats', { n: c.count, paid: m(c.paid), given: m(c.given) }) }}</div>
        </div>
        <p class="small muted" style="margin:10px 0 0">{{ t('sv.m.promos') }}</p>
      </section>

      <section class="ox-panel">
        <div class="ox-panel-h"><h2 class="grow">{{ t('sv.m.topups') }}</h2>
          <router-link v-if="can('stored.topup')" class="btn small" to="/merchant/order/topup">{{ t('sv.desk') }} ›</router-link></div>
        <div v-if="!topups.length" class="ox-empty">—</div>
        <div v-else class="ox-scroll">
          <table class="data">
            <thead><tr><th>{{ t('sv.c.time') }}</th><th>{{ t('sv.c.customer') }}</th><th class="num">{{ t('sv.c.pay') }}</th><th class="num">{{ t('sv.c.bonus') }}</th>
              <th>{{ t('sv.c.way') }}</th><th>{{ t('sv.c.by') }}</th><th>{{ t('sv.c.status') }}</th><th></th></tr></thead>
            <tbody>
              <tr v-for="x in topups" :key="x.id" data-testid="mk-topup">
                <td class="small">{{ whenText(x.paid_at ?? x.created_at) }}</td>
                <td>{{ x.phone }}<div v-if="x.brand_name" class="small muted">{{ x.brand_name }}</div></td>
                <td class="num"><span class="ox-money">{{ m(x.pay) }}</span></td>
                <td class="num">{{ m(x.bonus) }}<div v-if="x.coupon_qty" class="small muted">{{ tierCoupon(x, cur) }}</div></td>
                <td>{{ x.pay_type ? (x.pay_type === 'online' ? x.pay_method : t('ck.pt.' + x.pay_type)) : '' }}</td>
                <td>{{ x.received_by }}</td>
                <td><span class="tag" :class="x.status === 'paid' ? 'ok' : x.status === 'pending' ? 'gold' : 'off'">{{ t('sv.ts.' + x.status) }}</span></td>
                <td class="right"><button v-if="x.revocable" class="btn small" data-testid="mk-revoke" @click="revoke(x)">{{ !can('stored.cashout') ? '🔒 ' : '' }}{{ t('sv.revoke') }}</button></td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>
    </template>
  </div>
  <CampaignEditor v-if="editing !== undefined && data" :branch-id="bid" :currency="cur" :campaign="editing" :is-owner="isOwner"
    :approve-over="data.approve_over" @saved="saved" @close="editing = undefined" />
  <ApproveDialog v-if="revoking" :branch-id="bid" perm="stored.cashout" :what="t('sv.revoke_what')" @approved="approved" @close="revoking = null" />
  </div>
</template>

<style scoped>
.terms { display: grid; gap: 8px; justify-items: start; }
.terms p { margin: 0; line-height: 1.6; }
.camp { border: 1px solid var(--line); border-radius: 12px; padding: 12px 14px; display: grid; gap: 10px; background: #fff; margin-bottom: 12px; }
.camp-h { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; }
.camp-h .nm { font-size: 15px; }
.tag.ok { background: #e3f4ea; color: #18794e; } .tag.gold { background: #fbe7b3; color: #7a3e00; } .tag.off { background: #eee; color: #666; }
.tiers { display: grid; grid-template-columns: repeat(auto-fill, minmax(170px, 1fr)); gap: 10px; }
.tier { position: relative; border: 1px solid var(--line); border-radius: 10px; padding: 10px 12px; background: #fffaf2; }
.tier .pay { font-size: 13px; color: var(--muted); } .tier .pay b { font-size: 19px; color: #2f1810; margin-inline-start: 2px; }
.tier .give { color: var(--brand, #a02c20); font-weight: 700; margin-top: 2px; } .tier .give small { font-weight: 600; color: #b45309; }
.tier .cp { font-size: 12px; color: #7a3e00; margin-top: 4px; }
.tier .lim { font-size: 12px; color: var(--muted); margin-top: 4px; }
.tier .hot { position: absolute; top: -9px; inset-inline-end: 8px; background: #f4cf73; color: #5a3200; font-size: 11px; font-weight: 700; border-radius: 6px; padding: 0 6px; }
.tier.other { display: grid; place-items: center; border-style: dashed; }
</style>
