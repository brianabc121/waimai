<script setup lang="ts">
// A manager approves one action that the signed-in person's role may not do
// (docs/STAFF_ACCESS_DESIGN.md §7): pick who approves, they enter their code.
import { onMounted, ref } from 'vue'
import { ApiError, get, post } from '../api'
import { t, whenText } from '../i18n'
import '../approve'

const props = defineProps<{ branchId: number; perm: string; what: string }>()
const emit = defineEmits<{ (e: 'approved', token: string, name: string): void; (e: 'close'): void }>()
interface Approver { id: number; display_name: string; role: string }
const list = ref<Approver[] | null>(null)
const who = ref(0)
const pin = ref('')
const error = ref('')
const busy = ref(false)

onMounted(async () => {
  try {
    list.value = (await get<{ approvers: Approver[] }>(`/merchant/branches/${props.branchId}/approvers?perm=${props.perm}`)).approvers
    who.value = list.value[0]?.id ?? 0
  } catch (e) { error.value = (e as Error).message; list.value = [] }
})

async function go() {
  busy.value = true; error.value = ''
  try {
    const r = await post<{ token: string; approver_name: string }>(`/merchant/branches/${props.branchId}/approvals`, { approver_id: who.value, perm: props.perm, pin: pin.value })
    emit('approved', r.token, r.approver_name)
  } catch (e) {
    pin.value = ''
    if (e instanceof ApiError && e.code === 'BAD_PIN') error.value = t('ap.bad', { n: e.details?.left ?? 0 })
    else if (e instanceof ApiError && e.code === 'APPROVAL_LOCKED') error.value = t('ap.locked', { at: whenText(e.details?.until) })
    else error.value = (e as Error).message
  } finally { busy.value = false }
}
</script>

<template>
  <div class="drawer-mask" @click="emit('close')"></div>
  <form class="approvebox card stack" data-testid="approve-dialog" @submit.prevent="go">
    <h2>{{ t('ap.title') }}</h2>
    <p class="muted small" style="margin:0">{{ t('ap.for', { what }) }}</p>
    <div v-if="list && !list.length" class="banner warn" data-testid="approve-none">{{ t('ap.none') }}</div>
    <template v-else-if="list">
      <div class="small muted">{{ t('ap.who') }}</div>
      <div class="approvers">
        <label v-for="a in list" :key="a.id" class="check" :data-approver="a.display_name">
          <input v-model="who" type="radio" :value="a.id" />{{ a.display_name }} <span class="rtag" :class="a.role">{{ t('role.' + a.role) }}</span></label>
      </div>
      <label class="field"><span>{{ t('ap.pin') }}</span>
        <input v-model="pin" class="pininput" inputmode="numeric" autocomplete="one-time-code" maxlength="6" pattern="[0-9]{6}" required data-testid="approve-pin" /></label>
    </template>
    <div v-if="error" class="banner bad" data-testid="approve-error">{{ error }}</div>
    <div class="row" style="justify-content:flex-end;gap:8px">
      <button type="button" class="btn" @click="emit('close')">{{ t('ap.cancel') }}</button>
      <button v-if="list?.length" class="btn primary" :disabled="busy || pin.length !== 6 || !who" data-testid="approve-go">{{ t('ap.go') }}</button>
    </div>
  </form>
</template>

<style scoped>
.drawer-mask { position: fixed; inset: 0; background: rgba(0,0,0,.35); z-index: 40; }
.approvebox { position: fixed; left: 50%; top: 16vh; transform: translateX(-50%); width: min(380px, calc(100vw - 32px)); z-index: 41; }
.approvebox h2 { margin: 0; font-size: 17px; }
.approvers { display: flex; flex-direction: column; gap: 6px; }
.approvers .check { display: flex; align-items: center; gap: 8px; }
.pininput { letter-spacing: 10px; font-size: 22px; text-align: center; font-variant-numeric: tabular-nums; }
</style>
