<script setup lang="ts">
// Stored value on the platform (docs/STORED_VALUE_DESIGN.md §9): the switch
// and cap per country, and what customers hold at each store.
import { onMounted, reactive, ref } from 'vue'
import { get, put } from '../../api'
import { extend, money, t, toMajor, toMinor } from '../../i18n'
import type { AdminStoredBranch, StoredCountry } from '../../stored'

extend({
  'ad.sv.hint': ['按国家开关“充值送”和每位顾客在一家店最多能存多少。关掉后已有储值照常能用，只是不能再充。储值是门店欠顾客的钱，请留意存得多的门店。',
    'Turn top-ups on per country and set the most one customer may hold at a store. Turning a country off stops new top-ups; what is held can still be spent. Stored value is owed by the stores, so keep an eye on those holding a lot.'],
  'ad.sv.countries': ['国家', 'Countries'],
  'ad.sv.country': ['国家代码', 'Country'], 'ad.sv.on': ['开通', 'On'], 'ad.sv.cap': ['每人每店上限', 'Cap per customer per store'],
  'ad.sv.note': ['备注', 'Note'], 'ad.sv.stores': ['门店', 'Stores'], 'ad.sv.held': ['顾客储值', 'Held'],
  'ad.sv.save': ['保存', 'Save'], 'ad.sv.saved': ['已保存', 'Saved'], 'ad.sv.add': ['加国家', 'Add a country'],
  'ad.sv.branches': ['门店储值', 'Stores'], 'ad.sv.store': ['门店', 'Store'], 'ad.sv.principal': ['本金', 'Principal'], 'ad.sv.bonus': ['赠送', 'Bonus'],
  'ad.sv.month_topup': ['本月充值', 'Topped up this month'], 'ad.sv.month_out': ['本月退储值', 'Paid back this month'],
  'ad.sv.watch': ['存得多，请留意', 'Holds a lot'], 'ad.sv.closed': ['已停业', 'Closed'], 'ad.sv.frozen': ['有冻结', 'Frozen'],
  'ad.sv.none': ['还没有门店有储值', 'No store holds stored value yet'],
})

const countries = ref<(StoredCountry & { capText: string })[]>([])
const branches = ref<AdminStoredBranch[]>([])
const error = ref(''), notice = ref('')
const add = reactive({ cc: '' })
async function load() {
  try {
    countries.value = (await get<{ items: StoredCountry[] }>('/admin/stored/countries')).items.map(c => ({ ...c, capText: String(toMajor(c.cap)) }))
    branches.value = (await get<{ items: AdminStoredBranch[] }>('/admin/stored/branches')).items
  } catch (e) { error.value = (e as Error).message }
}
onMounted(load)
async function save(c: StoredCountry & { capText: string }) {
  error.value = ''
  try { await put('/admin/stored/countries', { country_code: c.country_code, enabled: c.enabled, cap: toMinor(c.capText), note: c.note }); notice.value = t('ad.sv.saved'); await load() }
  catch (e) { error.value = (e as Error).message }
}
function addCountry() {
  const cc = add.cc.trim().toUpperCase()
  if (!/^[A-Z]{2}$/.test(cc) || countries.value.some(c => c.country_code === cc)) return
  countries.value.push({ country_code: cc, enabled: false, cap: 0, capText: '', note: '', currency: '', stores: 0, principal: 0, bonus: 0 })
  add.cc = ''
}
</script>

<template>
  <div class="page stack" data-testid="admin-stored">
    <p class="muted small">{{ t('ad.sv.hint') }}</p>
    <div v-if="error" class="banner bad">{{ error }}</div>
    <div v-if="notice" class="banner ok" @click="notice = ''">{{ notice }}</div>
    <div class="card">
      <h2>{{ t('ad.sv.countries') }}</h2>
      <table class="data">
        <thead><tr><th>{{ t('ad.sv.country') }}</th><th>{{ t('ad.sv.on') }}</th><th>{{ t('ad.sv.cap') }}</th><th>{{ t('ad.sv.note') }}</th>
          <th class="num">{{ t('ad.sv.stores') }}</th><th class="num">{{ t('ad.sv.held') }}</th><th></th></tr></thead>
        <tbody>
          <tr v-for="c in countries" :key="c.country_code" :data-country="c.country_code">
            <td><b>{{ c.country_code }}</b> <span class="muted small">{{ c.currency }}</span></td>
            <td><input v-model="c.enabled" type="checkbox" data-testid="sv-on" /></td>
            <td><input v-model="c.capText" inputmode="decimal" style="width:120px" /></td>
            <td><input v-model="c.note" maxlength="200" /></td>
            <td class="num">{{ c.stores }}</td>
            <td class="num">{{ c.currency ? money(c.principal + c.bonus, c.currency) : '' }}</td>
            <td><button class="btn small primary" data-testid="sv-save" @click="save(c)">{{ t('ad.sv.save') }}</button></td>
          </tr>
        </tbody>
      </table>
      <div class="row" style="margin-top:10px"><input v-model="add.cc" maxlength="2" placeholder="SG" style="width:80px" />
        <button class="btn small" @click="addCountry">{{ t('ad.sv.add') }}</button></div>
    </div>
    <div class="card">
      <h2>{{ t('ad.sv.branches') }}</h2>
      <div v-if="!branches.length" class="muted">{{ t('ad.sv.none') }}</div>
      <table v-else class="data">
        <thead><tr><th>{{ t('ad.sv.store') }}</th><th class="num">{{ t('ad.sv.principal') }}</th><th class="num">{{ t('ad.sv.bonus') }}</th>
          <th class="num">{{ t('ad.sv.month_topup') }}</th><th class="num">{{ t('ad.sv.month_out') }}</th><th></th></tr></thead>
        <tbody>
          <tr v-for="b in branches" :key="b.branch_id" data-testid="sv-branch">
            <td>{{ b.name }} <span class="muted small">{{ b.country_code }}</span></td>
            <td class="num">{{ money(b.principal, b.currency) }}</td><td class="num">{{ money(b.bonus, b.currency) }}</td>
            <td class="num">{{ money(b.month_topup, b.currency) }}</td><td class="num">{{ money(b.month_paid_out, b.currency) }}</td>
            <td><span v-if="b.watch" class="tag warn">{{ t('ad.sv.watch') }}</span> <span v-if="b.closed" class="tag">{{ t('ad.sv.closed') }}</span>
              <span v-if="b.frozen" class="tag">{{ t('ad.sv.frozen') }}</span></td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>
