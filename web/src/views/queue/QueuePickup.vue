<script setup lang="ts">
// 取餐叫号 (docs/QUEUE_DISPLAY_DESIGN.md §5.3): the ready numbers with "call
// again" and "picked up"; preparing and in line below, to look at only.
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { post } from '../../api'
import { t } from '../../i18n'
import { minsSince, qErr, type PickupNumber, type QueueSettings, type TeaBoard } from '../../queue'

const props = defineProps<{ board: TeaBoard | null; base: string; settings: QueueSettings }>()
const emit = defineEmits<{ reload: [] }>()
const error = ref('')
const busy = ref(0)
const now = ref(Date.now())
let tick = 0
onMounted(() => { tick = window.setInterval(() => { now.value = Date.now() }, 20000) })
onBeforeUnmount(() => window.clearInterval(tick))

async function act(n: PickupNumber, what: 'recall' | 'done') {
  busy.value = n.order_id
  error.value = ''
  try { await post(`${props.base}/pickup/${n.order_id}/${what}`, {}) } catch (e) { error.value = qErr(e) }
  busy.value = 0
  emit('reload')
}
const leaves = (n: PickupNumber) => n.leaves_at ? Math.max(0, Math.ceil((new Date(n.leaves_at).getTime() - now.value) / 60000)) : 0
</script>

<template>
  <div class="qp">
    <div v-if="error" class="banner bad">{{ error }}</div>
    <section class="ox-panel">
      <div class="ox-panel-h"><h2>{{ t('qu.pk.ready') }} <span class="n">{{ board?.ready.length ?? 0 }}</span></h2></div>
      <div v-if="board && !board.ready.length" class="ox-empty">{{ t('qu.pk.empty') }}</div>
      <div v-for="(n, i) in board?.ready ?? []" :key="n.order_id" class="rd" :class="{ new: i === 0 && minsSince(n.stamp, now) < 2 }" :data-code="n.code" data-testid="qp-ready">
        <div class="no">{{ n.code }}</div>
        <div class="tx">
          <b>{{ n.items || '—' }}</b>
          <span>{{ t(n.service_type) }} · {{ t('qu.ago', { n: minsSince(n.stamp ?? n.since, now) }) }}<template v-if="n.calls"> · {{ t('qu.pk.called', { n: n.calls }) }}</template>
            · {{ t('qu.pk.leaves', { m: leaves(n) }) }}</span>
        </div>
        <button class="btn" :disabled="busy === n.order_id" data-testid="qp-recall" @click="act(n, 'recall')">{{ t('qu.pk.recall') }}</button>
        <button v-if="n.can_hand_over" class="btn ok" :disabled="busy === n.order_id" data-testid="qp-done" @click="act(n, 'done')">{{ t('qu.pk.done') }}</button>
      </div>
    </section>
    <div class="ox-split">
      <section class="ox-panel">
        <div class="ox-panel-h"><h2>{{ t('qu.pk.cook') }} <span class="n">{{ board?.cook.length ?? 0 }}</span></h2></div>
        <div class="smallnums cook"><span v-for="n in board?.cook ?? []" :key="n.order_id" :title="n.items">{{ n.code }}<i v-if="n.service_type === 'delivery'">🛵</i></span></div>
      </section>
      <section class="ox-panel">
        <div class="ox-panel-h"><h2>{{ t('qu.pk.wait') }} <span class="n">{{ board?.wait.length ?? 0 }}</span></h2></div>
        <div class="smallnums"><span v-for="n in board?.wait ?? []" :key="n.order_id" :title="n.items">{{ n.code }}<i v-if="n.service_type === 'delivery'">🛵</i></span></div>
      </section>
    </div>
  </div>
</template>

<style scoped>
.qp { display: grid; gap: 12px; }
.rd { display: flex; align-items: center; gap: 12px; padding: 12px; border: 1px solid var(--line); border-radius: 12px; background: #fff; margin-bottom: 9px; flex-wrap: wrap; }
.rd.new { border-color: #e5b94c; background: #fff8e2; }
.rd .no { font-size: 34px; font-weight: 900; color: var(--brand); width: 90px; font-variant-numeric: tabular-nums; }
.rd .tx { flex: 1; min-width: 180px; color: var(--muted); font-size: 13px; display: grid; gap: 2px; } .rd .tx b { color: var(--ink); font-size: 14.5px; }
.rd .btn { min-width: 96px; font-size: 15px; padding: 10px 14px; }
.smallnums { display: flex; flex-wrap: wrap; gap: 8px; }
.smallnums span { border: 1px solid var(--line); background: #fff; border-radius: 8px; padding: 4px 12px; font-weight: 800; font-size: 17px; color: #5b4636; font-variant-numeric: tabular-nums; }
.smallnums.cook span { background: #fffbea; border-color: #f1d58e; color: #7a4a06; }
.smallnums i { font-style: normal; font-size: 12px; margin-inline-start: 4px; }
</style>
