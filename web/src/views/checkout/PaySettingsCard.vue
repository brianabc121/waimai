<script setup lang="ts">
// The store's payment methods and discount reasons (docs/CHECKOUT_DISCOUNT_DESIGN.md §6.1).
import { onMounted, reactive, ref } from 'vue'
import { get, put } from '../../api'
import { t } from '../../i18n'
import { channelName } from '../../checkout'
import type { PayOptions } from '../../types'

const props = defineProps<{ branchId: number }>()
const opts = ref<PayOptions | null>(null)
const f = reactive({ cash: true, card: true, off: [] as number[], reasons: '' })
const error = ref('')
const saved = ref(false)
function load(o: PayOptions) {
  opts.value = o
  Object.assign(f, { cash: o.cash, card: o.card, off: [...o.off], reasons: (o.reasons ?? []).join('\n') })
}
onMounted(async () => { try { load(await get<PayOptions>(`/merchant/branches/${props.branchId}/pay-options`)) } catch (e) { error.value = (e as Error).message } })
function toggle(id: number) { f.off = f.off.includes(id) ? f.off.filter(x => x !== id) : [...f.off, id] }
async function save() {
  error.value = ''; saved.value = false
  const reasons = f.reasons.split('\n').map(x => x.trim()).filter(Boolean).slice(0, 12)
  try {
    load(await put<PayOptions>(`/merchant/branches/${props.branchId}/pay-settings`, { cash: f.cash, card: f.card, off: f.off, reasons }))
    saved.value = true
  } catch (e) { error.value = (e as Error).message }
}
</script>

<template>
  <form class="card stack" data-testid="pay-settings" @submit.prevent="save">
    <h2>{{ t('ck.set_title') }}</h2>
    <p class="muted small" style="margin:0">{{ t('ck.pay_set_hint') }}</p>
    <template v-if="opts">
      <div class="row wrap" style="gap:16px">
        <label class="check"><input v-model="f.cash" type="checkbox" data-testid="pay-set-cash" />{{ t('ck.set_cash') }}</label>
        <label class="check"><input v-model="f.card" type="checkbox" data-testid="pay-set-card" />{{ t('ck.set_card') }}</label>
      </div>
      <div class="muted small">{{ t('ck.set_online', { country: opts.country }) }}</div>
      <div class="row wrap" style="gap:8px 16px">
        <label v-for="c in opts.all" :key="c.id" class="check" data-testid="pay-set-channel">
          <input type="checkbox" :checked="!f.off.includes(c.id)" @change="toggle(c.id)" />{{ channelName(c) }}</label>
      </div>
      <label class="field"><span>{{ t('ck.set_reasons') }}</span>
        <textarea v-model="f.reasons" rows="4" :placeholder="t('ck.set_reasons_hint')" data-testid="pay-set-reasons"></textarea></label>
    </template>
    <div v-if="error" class="banner bad small">{{ error }}</div>
    <div class="row" style="gap:10px;align-items:center">
      <button class="btn primary" :disabled="!opts" data-testid="pay-set-save">{{ t('ck.save') }}</button>
      <span v-if="saved" class="tag ok" data-testid="pay-set-saved">{{ t('ck.saved') }}</span>
    </div>
  </form>
</template>
