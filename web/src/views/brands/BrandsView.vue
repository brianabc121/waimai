<script setup lang="ts">
// 店铺设置 · 品牌 (docs/MULTI_BRAND_DESIGN.md §14): the store's dine-in brand
// and delivery brands, each with its code, page, today's orders and state;
// adding and editing a brand.
import { computed, onMounted, reactive, ref } from 'vue'
import { ApiError, get, post, put } from '../../api'
import { money, t } from '../../i18n'
import { can, currentRole, reloadUser, session } from '../../session'
import { storeURL } from '../../site'
import type { Brand } from '../../types'
import '../../brands'
import '../../officeStrings'

const base = `/merchant/branches/${session.branchId}`
const brands = ref<Brand[]>([])
const error = ref('')
const cur = computed(() => currentRole()?.currency ?? 'PHP')
const mayEdit = computed(() => can('brand.manage'))
const LIMIT = 10
const COLORS = ['#a02c20', '#e07a10', '#2463a6', '#2f7d4f', '#7c3aed', '#be185d', '#0f766e', '#57534e']

async function load() {
  try { brands.value = (await get<{ brands: Brand[] }>(`${base}/brands`)).brands; error.value = '' } catch (e) { error.value = (e as Error).message }
}
onMounted(load)
const dineTaken = (id: number) => brands.value.some(b => b.dine_in && b.status === 'active' && b.id !== id)

// ---- the form ----
const empty = () => ({ id: 0, is_main: false, name: '', name_en: '', code: '', dine_in: false, delivery: true, color: COLORS[1],
  own_hours: false, open_time: '11:00', close_time: '22:00', paused: false, mall_visible: true, status: 'active' as 'active' | 'disabled' })
const f = reactive({ open: false, saving: false, error: '', ...empty() })
function add() {
  Object.assign(f, empty(), { open: true, error: '', color: COLORS[brands.value.length % COLORS.length] })
}
function edit(b: Brand) {
  Object.assign(f, { open: true, error: '', saving: false, id: b.id, is_main: b.is_main, name: b.name, name_en: b.name_en, code: b.code,
    dine_in: b.dine_in, delivery: b.delivery, color: b.color, own_hours: !!b.open_time, open_time: b.open_time || '11:00',
    close_time: b.close_time || '22:00', paused: b.paused, mall_visible: b.mall_visible, status: b.status })
}
let codeTimer = 0
function suggest() {
  if (f.id || f.code) return
  clearTimeout(codeTimer)
  codeTimer = window.setTimeout(async () => {
    if (!f.name_en.trim() || f.code) return
    try { f.code = (await get<{ code: string }>(`${base}/brand-code?name_en=${encodeURIComponent(f.name_en)}`)).code } catch { /* type one */ }
  }, 400)
}
const preview = computed(() => {
  const c = (f.code || 'HCD').toUpperCase()
  return t('br.preview', { no: c + '000001', no2: c + '000001-2' })
})
function errText(e: unknown): string {
  if (e instanceof ApiError) {
    const k: Record<string, string> = { BRAND_CODE_TAKEN: 'br.err.code_taken', BRAND_CODE_INVALID: 'br.err.code_invalid',
      DINE_BRAND_TAKEN: 'br.err.dine_taken', BRAND_LIMIT: 'br.err.limit', MAIN_BRAND: 'br.err.main' }
    if (k[e.code]) return t(k[e.code], { n: (e.details as { limit?: number } | undefined)?.limit ?? LIMIT })
  }
  return (e as Error).message
}
async function save(status?: 'active' | 'disabled') {
  f.saving = true; f.error = ''
  const body = { name: f.name, name_en: f.name_en, code: f.code, dine_in: f.dine_in, delivery: f.delivery, color: f.color,
    open_time: f.own_hours ? f.open_time : '', close_time: f.own_hours ? f.close_time : '', paused: f.paused,
    mall_visible: f.mall_visible, status: status ?? f.status }
  try {
    if (f.id) await put(`${base}/brands/${f.id}`, body)
    else await post(`${base}/brands`, body)
    f.open = false
    await Promise.all([load(), reloadUser()])
  } catch (e) { f.error = errText(e) } finally { f.saving = false }
}
function toggle() {
  if (f.status === 'active' && !confirm(t('br.disable_ask'))) return
  void save(f.status === 'active' ? 'disabled' : 'active')
}
const hours = (b: Brand) => b.open_time ? `${b.open_time}–${b.close_time}` : t('br.hours_store')
</script>

<template>
  <div class="page ox-page stack" data-testid="brands-page">
    <div class="ox-head">
      <div><h1>{{ t('br.title') }}</h1><p class="ox-sub">{{ t('br.sub') }}</p></div>
      <button v-if="mayEdit" class="btn primary" data-testid="brand-add" @click="add">＋ {{ t('br.add') }}</button>
    </div>
    <div v-if="error" class="banner bad">{{ error }}</div>
    <p class="muted small" style="margin:0">{{ currentRole()?.branch_name }} · {{ t('br.store_note') }}</p>
    <div class="bgrid">
      <div v-for="b in brands" :key="b.id" class="bcard" :class="{ off: b.status !== 'active' }" :style="{ '--c': b.color }" data-testid="brand-card">
        <div class="bh">
          <div class="logo">{{ [...b.name][0] }}</div>
          <div class="grow">
            <b class="bn" data-testid="brand-name">{{ b.name }}</b> <span v-if="b.is_main" class="tag gold">{{ t('br.main') }}</span>
            <div class="muted small">{{ t('br.code') }} <span class="ox-code" data-testid="brand-code">{{ b.code || '—' }}</span>
              <template v-if="b.code"> · {{ b.code }}000001</template></div>
          </div>
          <button v-if="mayEdit" class="btn small" :data-testid="'brand-edit-' + b.id" @click="edit(b)">{{ t('br.edit') }}</button>
        </div>
        <div class="row wrap" style="gap:6px">
          <span v-if="b.dine_in" class="tag">{{ t('br.dine') }}</span>
          <span v-if="b.delivery" class="tag">{{ t('br.delivery') }}</span>
          <span class="tag">{{ t('br.hours') }} {{ hours(b) }}</span>
        </div>
        <div v-if="b.is_main" class="muted small">{{ t('br.site') }} {{ storeURL({ store_no: b.store_no, slug: b.slug }) }}</div>
        <div class="row between">
          <span v-if="b.today" class="small">{{ t('br.today', { n: b.today.orders, s: money(b.today.total, cur) }) }}</span>
          <b v-if="b.status !== 'active'" class="bad small">{{ t('br.disabled') }}</b>
          <b v-else-if="b.paused" class="bad small">{{ t('br.paused') }}</b>
          <b v-else class="ok small">● {{ t('br.open') }}</b>
        </div>
      </div>
    </div>
    <div class="banner warn small">{{ t('br.rules', { n: LIMIT }) }}</div>

    <template v-if="f.open">
      <div class="sheet-mask" @click="f.open = false"></div>
      <form class="sheet bsheet" data-testid="brand-form" @submit.prevent="save()">
        <div class="sheet-head"><h2>{{ f.id ? t('br.edit') : t('br.add') }}</h2></div>
        <div class="stack">
          <label class="field">{{ t('br.name') }}<input v-model="f.name" maxlength="40" required data-testid="brand-f-name" /></label>
          <label class="field">{{ t('br.name_en') }}<input v-model="f.name_en" maxlength="60" data-testid="brand-f-name-en" @input="suggest" /></label>
          <label class="field">{{ t('br.code') }}<input v-model="f.code" maxlength="8" required class="mono" style="text-transform:uppercase" data-testid="brand-f-code" />
            <span class="muted small">{{ t('br.code_hint') }}</span><span class="small" data-testid="brand-f-preview">{{ preview }}</span></label>
          <div class="field">{{ t('br.color') }}
            <div class="row wrap" style="gap:6px">
              <button v-for="c in COLORS" :key="c" type="button" class="sw" :class="{ on: f.color === c }" :style="{ background: c }" :aria-label="c" @click="f.color = c"></button>
            </div><span class="muted small">{{ t('br.color_hint') }}</span></div>
          <div class="field">{{ t('br.kind') }}
            <label class="check"><input v-model="f.dine_in" type="checkbox" :disabled="!f.dine_in && dineTaken(f.id)" data-testid="brand-f-dine" />{{ t('br.dine') }}
              <span v-if="!f.dine_in && dineTaken(f.id)" class="muted small">（{{ t('br.dine_taken') }}）</span></label>
            <label class="check"><input v-model="f.delivery" type="checkbox" data-testid="brand-f-delivery" />{{ t('br.delivery') }}</label>
          </div>
          <div class="field">{{ t('br.hours') }}
            <label class="check"><input v-model="f.own_hours" type="radio" :value="false" />{{ t('br.hours_store') }}</label>
            <label class="check"><input v-model="f.own_hours" type="radio" :value="true" />{{ t('br.hours_own') }}
              <template v-if="f.own_hours"><input v-model="f.open_time" type="time" /> – <input v-model="f.close_time" type="time" /></template></label>
          </div>
          <label class="check"><input v-model="f.mall_visible" type="checkbox" />{{ t('br.mall') }}</label>
          <label class="check"><input v-model="f.paused" type="checkbox" data-testid="brand-f-paused" />{{ t('br.paused') }}</label>
          <div v-if="f.error" class="banner bad" data-testid="brand-f-error">{{ f.error }}</div>
        </div>
        <div class="row between" style="margin-top:14px">
          <button v-if="f.id && !f.is_main" type="button" class="btn" :class="{ danger: f.status === 'active' }" data-testid="brand-f-toggle" @click="toggle">
            {{ f.status === 'active' ? t('br.disable') : t('br.enable') }}</button>
          <span v-else></span>
          <span class="row" style="gap:8px">
            <button type="button" class="btn" @click="f.open = false">{{ t('cancel') }}</button>
            <button class="btn primary" :disabled="f.saving" data-testid="brand-f-save">{{ t('br.save') }}</button>
          </span>
        </div>
      </form>
    </template>
  </div>
</template>

<style scoped>
.bgrid { display: grid; grid-template-columns: repeat(auto-fill, minmax(280px, 1fr)); gap: 14px; }
.bcard { background: #fff; border: 1px solid var(--line); border-top: 4px solid var(--c); border-radius: 12px; padding: 14px; display: grid; gap: 8px; min-width: 0; }
.bcard.off { opacity: .6; }
.bh { display: flex; gap: 10px; align-items: center; min-width: 0; }
.bh .grow { min-width: 0; }
.bn { font-size: 16px; overflow-wrap: anywhere; }
.logo { width: 44px; height: 44px; flex: none; border-radius: 12px; display: grid; place-items: center; color: #fff; font-weight: 800; font-size: 18px; background: var(--c); }
.tag.gold { background: #f8e3b2; color: #7a3e00; }
.sw { width: 26px; height: 26px; border-radius: 50%; border: 2px solid #fff; box-shadow: 0 0 0 1px var(--line); cursor: pointer; padding: 0; }
.sw.on { box-shadow: 0 0 0 2px var(--ink, #3b1f14); }
.bsheet { max-width: 520px; }
.field { display: grid; gap: 4px; font-weight: 600; font-size: 13px; }
.field .check { font-weight: 400; }
</style>
