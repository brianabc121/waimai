<script setup lang="ts">
// 点单 > 排队叫号 (docs/QUEUE_DISPLAY_DESIGN.md §7): the restaurant's line,
// the tea shop's pickup numbers, the log and the settings, one tab each.
import { computed, ref, watch } from 'vue'
import { get, post } from '../../api'
import { t } from '../../i18n'
import { useLive } from '../../live'
import { can, session } from '../../session'
import { qErr, type QueueDesk, type TeaBoard } from '../../queue'
import QueueLine from './QueueLine.vue'
import QueuePickup from './QueuePickup.vue'
import QueueLog from './QueueLog.vue'
import QueueSetup from './QueueSetup.vue'

const bid = session.branchId!
const base = `/merchant/branches/${bid}/queue`
const desk = ref<QueueDesk | null>(null)
const board = ref<TeaBoard | null>(null)
const error = ref('')
const tab = ref<'line' | 'pickup' | 'log' | 'settings' | ''>('')

async function load() {
  try {
    const d = await get<QueueDesk>(base)
    desk.value = d
    if (!tab.value) tab.value = d.settings.restaurant ? 'line' : d.settings.tea ? 'pickup' : 'settings'
    if (d.settings.tea && tab.value === 'pickup') board.value = await get<TeaBoard>(base + '/pickup')
    error.value = ''
  } catch (e) { error.value = qErr(e); throw e }
}
const { online, refreshNow } = useLive(() => `/merchant/branches/${bid}/events`, load)
watch(tab, x => { if (x === 'pickup') void refreshNow() })

const set = computed(() => desk.value?.settings)
const pausing = ref(false)
async function togglePause() {
  if (!set.value) return
  pausing.value = true
  try { await post(base + '/pause', { paused: !set.value.paused }); await refreshNow() } catch (e) { error.value = qErr(e) }
  pausing.value = false
}
const takeOpen = ref(0)
</script>

<template>
  <div class="ox warm qv">
    <div class="page ox-page">
      <div class="ox-head">
        <div><h1>{{ t('qu.nav') }}</h1><p class="ox-sub">{{ t('qu.sub') }}</p></div>
        <div class="ox-acts">
          <button v-if="set?.restaurant" class="btn primary big" data-testid="qv-take" @click="tab = 'line'; takeOpen++">{{ t('qu.take') }}</button>
          <button v-if="set?.restaurant" class="btn" :class="{ danger: !set.paused }" :disabled="pausing" data-testid="qv-pause" @click="togglePause">
            {{ set.paused ? t('qu.resume') : t('qu.pause') }}</button>
          <a class="btn" href="/screen" target="_blank" rel="noopener" data-testid="qv-screen">{{ t('qu.open_screen') }}</a>
        </div>
      </div>
      <div class="ox-tabs">
        <button v-if="set?.restaurant" :class="{ on: tab === 'line' }" data-testid="qv-tab-line" @click="tab = 'line'">{{ t('qu.tab.line') }} <span class="n">{{ desk?.stats.waiting ?? '' }}</span></button>
        <button v-if="set?.tea" :class="{ on: tab === 'pickup' }" data-testid="qv-tab-pickup" @click="tab = 'pickup'">{{ t('qu.tab.pickup') }}</button>
        <button v-if="set?.restaurant" :class="{ on: tab === 'log' }" data-testid="qv-tab-log" @click="tab = 'log'">{{ t('qu.tab.log') }}</button>
        <button :class="{ on: tab === 'settings' }" data-testid="qv-tab-settings" @click="tab = 'settings'">{{ t('qu.tab.settings') }}</button>
      </div>
      <div v-if="!online" class="banner warn">{{ t('offline') }}</div>
      <div v-if="error" class="banner bad" data-testid="qv-error">{{ error }}</div>
      <div v-if="set?.paused && tab === 'line'" class="banner warn" data-testid="qv-paused">{{ t('qu.paused_banner') }}</div>

      <template v-if="desk">
        <QueueLine v-if="tab === 'line'" :desk="desk" :base="base" :take="takeOpen" @reload="refreshNow" />
        <QueuePickup v-else-if="tab === 'pickup'" :board="board" :base="base" :settings="desk.settings" @reload="refreshNow" />
        <QueueLog v-else-if="tab === 'log'" :base="base" />
        <QueueSetup v-else-if="tab === 'settings'" :desk="desk" :base="base" :manage="can('queue.manage')" @reload="refreshNow" />
      </template>
    </div>
  </div>
</template>

<style scoped>
.qv .btn.big { font-size: 16px; padding: 10px 18px; }
</style>
