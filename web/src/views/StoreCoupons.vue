<script setup lang="ts">
// The coupons a store lets customers claim, as a row of small tickets on its
// page (docs/COUPON_DESIGN.md §3.5). Claiming needs an account.
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { get, post } from '../api'
import { couponName, couponStub } from '../coupons'
import { t } from '../i18n'
import { loadCustomer, loginRoute } from '../customer'
import type { ClaimableCoupon } from '../types'

const props = defineProps<{ slug: string; currency: string }>()
const router = useRouter()
const list = ref<ClaimableCoupon[]>([])
const note = ref(''), busy = ref(0)
async function load() {
  try { list.value = (await get<{ coupons: ClaimableCoupon[] }>(`/stores/${props.slug}/coupons`)).coupons } catch { list.value = [] }
}
onMounted(load)
async function claim(c: ClaimableCoupon) {
  if (!(await loadCustomer())) { router.push(loginRoute(`/s/${props.slug}`, props.slug)); return }
  busy.value = c.id; note.value = ''
  try { await post(`/stores/${props.slug}/coupons/${c.id}/claim`); note.value = t('cb.got') } catch (e) { note.value = (e as Error).message }
  finally { busy.value = 0; await load() }
}
</script>

<template>
  <div v-if="list.length" class="scp" data-testid="store-coupons">
    <div class="scp-list">
      <div v-for="c in list" :key="c.id" class="scp-c" :class="{ done: c.claimed || c.gone }" data-testid="store-coupon">
        <span class="amt"><b>{{ couponStub(c, currency)[0] }}</b><small>{{ couponStub(c, currency)[1] }}</small></span>
        <span class="nm">{{ couponName(c) }}</span>
        <button type="button" :disabled="c.claimed || c.gone || busy === c.id" data-testid="claim-btn" @click="claim(c)">
          {{ c.claimed ? t('cb.claimed') : c.gone ? t('cb.gone') : t('cb.claim') }}</button>
      </div>
    </div>
    <div v-if="note" class="small scp-note" data-testid="claim-note">{{ note }}</div>
  </div>
</template>

<style scoped>
.scp { margin-top: 8px; }
.scp-list { display: flex; gap: 8px; overflow-x: auto; padding-bottom: 2px; scrollbar-width: none; }
.scp-c { flex: none; display: flex; align-items: center; gap: 8px; border: 1px solid #f0c9a8; background: #fff6ee; border-radius: 8px; padding: 5px 6px 5px 10px; max-width: 260px; }
.scp-c .amt { display: flex; flex-direction: column; line-height: 1.1; color: #b8392a; }
.scp-c .amt b { font-size: 16px; } .scp-c .amt small { font-size: 11px; }
.scp-c .nm { font-size: 12px; color: #7b6657; max-width: 110px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.scp-c button { border: 0; border-radius: 6px; background: #b8392a; color: #fff; font: inherit; font-size: 12.5px; font-weight: 600; padding: 4px 10px; cursor: pointer; white-space: nowrap; }
.scp-c.done { opacity: .65; } .scp-c button:disabled { background: #c9b8a8; cursor: default; }
.scp-note { color: #18794e; margin-top: 4px; }
</style>
