<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { get, post } from '../api'
import { money, t, toMajor, toMinor } from '../i18n'
import { currentRole, session } from '../session'
import type { Cash } from '../types'

const bid = session.branchId!
const cur = currentRole()?.currency ?? 'PHP'
const rows = ref<Cash[]>([])
const input = reactive<Record<number, string>>({})
const error = ref('')

async function load() {
  rows.value = (await get<{ collections: Cash[] }>(`/merchant/branches/${bid}/cash-collections`)).collections
  for (const r of rows.value) if (input[r.id] === undefined) input[r.id] = String(toMajor(r.amount_collected))
}
onMounted(load)

async function reconcile(r: Cash) {
  error.value = ''
  try { await post(`/merchant/cash-collections/${r.id}/reconcile`, { amount_returned: toMinor(input[r.id]) }) }
  catch (e) { error.value = (e as Error).message }
  await load()
}
</script>

<template>
  <div class="page stack">
    <h1>{{ t('cash') }}</h1>
    <div v-if="error" class="banner bad">{{ error }}</div>
    <div v-if="rows.length === 0" class="card muted">{{ t('no_cash') }}</div>
    <div v-else class="card" style="padding:0;overflow:auto">
      <table class="data">
        <thead><tr><th>{{ t('order_no') }}</th><th>{{ t('rider') }}</th><th>{{ t('due') }}</th><th>{{ t('collected') }}</th><th>{{ t('returned') }}</th><th></th></tr></thead>
        <tbody>
          <tr v-for="r in rows" :key="r.id">
            <td><b>{{ r.public_no }}</b><div class="muted small">{{ new Date(r.created_at).toLocaleString() }}</div></td>
            <td>{{ r.rider_name }}</td>
            <td class="price">{{ money(r.amount_due, cur) }}</td>
            <td class="price">{{ money(r.amount_collected, cur) }}
              <div v-if="r.amount_collected !== r.amount_due" class="small" style="color:var(--bad)">{{ t('diff') }} {{ money(r.amount_collected - r.amount_due, cur) }}</div></td>
            <td style="width:160px">
              <input v-if="r.reconcile_status === 'pending'" type="number" min="0" step="0.01" v-model="input[r.id]" />
              <span v-else class="price">{{ money(r.amount_returned ?? 0, cur) }}</span>
            </td>
            <td>
              <button v-if="r.reconcile_status === 'pending'" class="btn small primary" @click="reconcile(r)">{{ t('reconcile') }}</button>
              <span v-else class="tag" :class="r.reconcile_status === 'reconciled' ? 'ok' : 'bad'">{{ t('c.' + r.reconcile_status) }}</span>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>
