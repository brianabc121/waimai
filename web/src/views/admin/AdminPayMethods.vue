<script setup lang="ts">
// The online payment methods of each country (docs/CHECKOUT_DISCOUNT_DESIGN.md §6.3).
// They only record how a bill was paid: nothing is scanned or charged.
import { computed, onMounted, reactive, ref } from 'vue'
import { api, get, post } from '../../api'
import { extend, pick, t } from '../../i18n'
import { LANGS } from '../../langs'
import type { PayChannel } from '../../types'

extend({
  'ad.pay.hint': ['在线支付只是付款类型：店员结账时点一下记录顾客用什么付的，不扫码、不对接支付接口。停用后老订单照常显示，以后结账不再出现；用过的方式不能删除。',
    'Online payment is only a payment type: staff tap how the customer paid. Nothing is scanned or charged. A disabled method still shows on old orders but no longer at checkout; methods that were used cannot be deleted.'],
  'ad.pay.countries': ['国家', 'Countries'],
  'ad.pay.add_country': ['加国家（两位代码，例如 PH）', 'Add a country (2-letter code, e.g. PH)'],
  'ad.pay.n': ['{n} 种 · {s} 家店', '{n} methods · {s} stores'],
  'ad.pay.name': ['名字', 'Name'],
  'ad.pay.names': ['各语言名字（不填就显示原名）', 'Names by language (empty shows the name)'],
  'ad.pay.icon': ['图标链接（正方形图片）', 'Icon link (square picture)'],
  'ad.pay.active': ['启用', 'On'],
  'ad.pay.stores': ['开通的店', 'Stores'],
  'ad.pay.uses': ['30 天用了', 'Used (30 days)'],
  'ad.pay.new': ['新增方式', 'Add a method'],
  'ad.pay.edit': ['编辑', 'Edit'],
  'ad.pay.save': ['保存', 'Save'],
  'ad.pay.cancel': ['取消', 'Cancel'],
  'ad.pay.up': ['上移', 'Up'],
  'ad.pay.down': ['下移', 'Down'],
  'ad.pay.none': ['这个国家还没有在线方式', 'No online methods for this country yet'],
  'ad.pay.dup': ['这个国家已经有同名的方式', 'This country already has a method with that name'],
})

interface Country { country: string; channels: number; stores: number }
const countries = ref<Country[]>([])
const cc = ref('')
const list = ref<PayChannel[]>([])
const error = ref('')
const newCC = ref('')

async function loadCountries() {
  countries.value = (await get<{ countries: Country[] }>('/admin/pay-countries')).countries
  if (!cc.value && countries.value.length) cc.value = countries.value[0].country
}
async function load() {
  if (!cc.value) { list.value = []; return }
  list.value = (await get<{ channels: PayChannel[] }>(`/admin/pay-channels?country=${cc.value}`)).channels
}
async function run(f: () => Promise<unknown>) {
  error.value = ''
  try { await f() } catch (e) { error.value = (e as Error & { code?: string }).code === 'DUPLICATE' ? t('ad.pay.dup') : (e as Error).message }
}
onMounted(() => run(async () => { await loadCountries(); await load() }))
function choose(c: string) { cc.value = c; editing.value = null; void run(load) }
function addCountry() {
  const c = newCC.value.trim().toUpperCase()
  if (!/^[A-Z]{2}$/.test(c)) return
  if (!countries.value.some(x => x.country === c)) countries.value.push({ country: c, channels: 0, stores: 0 })
  newCC.value = ''; choose(c)
}

const OTHER = LANGS.map(l => l.code)
const editing = ref<PayChannel | null>(null)
const f = reactive<{ name: string; names: Record<string, string>; icon_url: string; active: boolean }>({ name: '', names: {}, icon_url: '', active: true })
function edit(c: PayChannel | null) {
  editing.value = c ?? { id: 0, country: cc.value, name: '', names: {}, icon_url: '', sort: 0, active: true }
  Object.assign(f, { name: editing.value.name, names: { ...editing.value.names }, icon_url: editing.value.icon_url, active: editing.value.active })
}
const save = () => run(async () => {
  const c = editing.value!
  const body = { country: cc.value, name: f.name, names: f.names, icon_url: f.icon_url.trim(), active: f.active }
  if (c.id) await api('PUT', `/admin/pay-channels/${c.id}`, body)
  else await post('/admin/pay-channels', body)
  editing.value = null
  await loadCountries(); await load()
})
const toggle = (c: PayChannel) => run(async () => {
  await api('PUT', `/admin/pay-channels/${c.id}`, { country: c.country, name: c.name, names: c.names, icon_url: c.icon_url, active: !c.active })
  await load()
})
const move = (i: number, d: number) => run(async () => {
  const ids = list.value.map(c => c.id)
  const j = i + d
  if (j < 0 || j >= ids.length) return
  ;[ids[i], ids[j]] = [ids[j], ids[i]]
  await post('/admin/pay-channels/sort', { country: cc.value, ids })
  await load()
})
const shownName = (c: PayChannel) => pick(c.names.zh || c.name, c.names.en || c.name)
const cur = computed(() => countries.value.find(x => x.country === cc.value))
</script>

<template>
  <div class="adm-page">
    <p class="muted small" style="margin-top:0">{{ t('ad.pay.hint') }}</p>
    <div v-if="error" class="banner bad">{{ error }}</div>
    <div class="pm">
      <div class="card flush pm-side" data-testid="pay-countries">
        <div class="pm-h">{{ t('ad.pay.countries') }}</div>
        <button v-for="c in countries" :key="c.country" class="pm-cc" :class="{ on: c.country === cc }" :data-cc="c.country" @click="choose(c.country)">
          <b>{{ c.country }}</b><span class="muted small">{{ t('ad.pay.n', { n: c.channels, s: c.stores }) }}</span></button>
        <form class="row" style="padding:8px;gap:6px" @submit.prevent="addCountry">
          <input v-model="newCC" maxlength="2" style="width:70px;text-transform:uppercase" :placeholder="'PH'" :title="t('ad.pay.add_country')" data-testid="pay-add-cc" />
          <button class="btn small">+</button>
        </form>
      </div>
      <div class="stack" style="flex:1;min-width:0">
        <div class="row between"><h2 style="margin:0">{{ cc }} <span v-if="cur" class="muted small">{{ t('ad.pay.n', { n: cur.channels, s: cur.stores }) }}</span></h2>
          <button class="btn primary small" :disabled="!cc" data-testid="pay-new" @click="edit(null)">{{ t('ad.pay.new') }}</button></div>
        <div class="card flush">
          <table class="data">
            <thead><tr><th></th><th>{{ t('ad.pay.name') }}</th><th>{{ t('ad.pay.stores') }}</th><th>{{ t('ad.pay.uses') }}</th><th>{{ t('ad.pay.active') }}</th><th></th></tr></thead>
            <tbody>
              <tr v-for="(c, i) in list" :key="c.id" :data-channel="c.name" :class="{ off: !c.active }">
                <td style="width:40px"><img v-if="c.icon_url" :src="c.icon_url" alt="" class="pm-icon" /><span v-else class="pm-icon pm-ph">{{ c.name.slice(0, 1) }}</span></td>
                <td><b>{{ c.name }}</b><div v-if="shownName(c) !== c.name" class="muted small">{{ shownName(c) }}</div></td>
                <td class="num">{{ c.stores ?? 0 }}</td>
                <td class="num">{{ c.uses ?? 0 }}</td>
                <td><label class="check"><input type="checkbox" :checked="c.active" data-testid="pay-active" @change="toggle(c)" /></label></td>
                <td class="row" style="gap:4px;justify-content:flex-end">
                  <button class="btn small" :disabled="i === 0" :title="t('ad.pay.up')" @click="move(i, -1)">↑</button>
                  <button class="btn small" :disabled="i === list.length - 1" :title="t('ad.pay.down')" @click="move(i, 1)">↓</button>
                  <button class="btn small" data-testid="pay-edit" @click="edit(c)">{{ t('ad.pay.edit') }}</button>
                </td>
              </tr>
              <tr v-if="!list.length"><td colspan="6" class="muted">{{ t('ad.pay.none') }}</td></tr>
            </tbody>
          </table>
        </div>
        <form v-if="editing" class="card stack" data-testid="pay-form" @submit.prevent="save">
          <label class="field"><span>{{ t('ad.pay.name') }}</span><input v-model="f.name" maxlength="40" required data-testid="pay-name" /></label>
          <div class="muted small">{{ t('ad.pay.names') }}</div>
          <div class="pm-names">
            <label v-for="l in OTHER" :key="l" class="row" style="gap:6px"><span class="muted small" style="width:56px">{{ l }}</span>
              <input v-model="f.names[l]" maxlength="40" :placeholder="f.name" /></label>
          </div>
          <label class="field"><span>{{ t('ad.pay.icon') }}</span><input v-model="f.icon_url" maxlength="500" placeholder="https://…" /></label>
          <label class="check"><input v-model="f.active" type="checkbox" />{{ t('ad.pay.active') }}</label>
          <div class="row" style="justify-content:flex-end;gap:8px">
            <button type="button" class="btn" @click="editing = null">{{ t('ad.pay.cancel') }}</button>
            <button class="btn primary" :disabled="!f.name.trim()" data-testid="pay-save">{{ t('ad.pay.save') }}</button>
          </div>
        </form>
      </div>
    </div>
  </div>
</template>

<style scoped>
.pm { display: flex; gap: 14px; align-items: flex-start; }
.pm-side { width: 200px; flex: none; }
.pm-h { padding: 10px 12px; font-weight: 600; border-bottom: 1px solid var(--line); }
.pm-cc { display: flex; flex-direction: column; align-items: flex-start; width: 100%; padding: 8px 12px; border: 0; border-bottom: 1px solid var(--line); background: none; cursor: pointer; text-align: start; font: inherit; }
.pm-cc.on { background: var(--brand-soft, #fdecea); }
.pm-icon { width: 28px; height: 28px; border-radius: 6px; object-fit: cover; display: inline-flex; align-items: center; justify-content: center; }
.pm-ph { background: #f3e9dc; color: #8a5a00; font-weight: 700; }
tr.off td { opacity: .5; }
.pm-names { display: grid; grid-template-columns: repeat(auto-fill, minmax(200px, 1fr)); gap: 6px 12px; }
@media (max-width: 700px) { .pm { flex-direction: column; } .pm-side { width: 100%; } }
</style>
