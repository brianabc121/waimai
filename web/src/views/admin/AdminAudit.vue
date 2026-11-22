<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { get } from '../../api'
import { t } from '../../i18n'
import Pager from './Pager.vue'
import type { AuditEntry } from './types'
import { fmtDateTime, fmtTime, qs } from './util'

const SIZE = 50
const action = ref('')
const from = ref('')
const to = ref('')
const page = ref(1)
const list = reactive<{ items: AuditEntry[]; total: number }>({ items: [], total: 0 })
const error = ref('')
const groups = ['merchant', 'branch', 'user', 'item', 'category', 'job', 'settings']

async function load() {
  // "to" is a whole day: everything before the start of the next one
  let until = ''
  if (to.value) { const d = new Date(to.value + 'T00:00:00'); d.setDate(d.getDate() + 1); until = d.toISOString() }
  try {
    Object.assign(list, await get('/admin/audit' + qs({ action: action.value, page: page.value, page_size: SIZE,
      from: from.value ? new Date(from.value + 'T00:00:00').toISOString() : '', to: until })))
    error.value = ''
  } catch (e) { error.value = (e as Error).message }
}
onMounted(load)
function reload() { page.value = 1; void load() }
function go(p: number) { page.value = p; void load() }

// detail is small JSON; show it as "key: value" pairs without the braces
function describe(d: Record<string, unknown>): string {
  return Object.entries(d).map(([k, v]) => `${k}: ${typeof v === 'object' && v !== null ? JSON.stringify(v) : String(v)}`).join(' · ')
}
</script>

<template>
  <div class="adm-page">
    <div class="row wrap">
      <select v-model="action" style="width:170px" data-testid="audit-action" @change="reload">
        <option value="">{{ t('ad.all_actions') }}</option>
        <option v-for="g in groups" :key="g" :value="g + '.'">{{ t('ad.act.' + g) }}</option>
      </select>
      <label class="row small muted">{{ t('ad.from') }} <input type="date" v-model="from" style="width:150px" @change="reload" /></label>
      <label class="row small muted">{{ t('ad.to') }} <input type="date" v-model="to" style="width:150px" @change="reload" /></label>
    </div>
    <div v-if="error" class="banner bad">{{ error }}</div>
    <div class="card flush">
      <table class="data">
        <thead><tr><th>{{ t('ad.time') }}</th><th>{{ t('ad.admin') }}</th><th>{{ t('ad.action') }}</th><th>{{ t('ad.target') }}</th><th>{{ t('ad.change') }}</th><th>{{ t('ad.ip') }}</th></tr></thead>
        <tbody>
          <tr v-for="e in list.items" :key="e.id" :data-action="e.action">
            <td class="num" :title="e.created_at">{{ fmtDateTime(e.created_at) }}<span class="muted small">{{ fmtTime(e.created_at).slice(5) }}</span></td>
            <td>{{ e.admin_name }}</td>
            <td><span class="tag">{{ e.action }}</span></td>
            <td class="num">{{ e.target_type }}<template v-if="e.target_id"> #{{ e.target_id }}</template></td>
            <td class="small muted" style="max-width:520px;word-break:break-word">{{ describe(e.detail) }}</td>
            <td class="mono">{{ e.ip }}</td>
          </tr>
          <tr v-if="list.items.length === 0"><td colspan="6" class="muted">{{ t('ad.empty') }}</td></tr>
        </tbody>
      </table>
    </div>
    <Pager :total="list.total" :page="page" :size="SIZE" @go="go" />
  </div>
</template>
