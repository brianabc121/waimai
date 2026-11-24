<script setup lang="ts">
// The payment type (cash, card, online) and the online method
// (docs/CHECKOUT_DISCOUNT_DESIGN.md §6.1). Online only records how the bill
// was paid; nothing is scanned or charged.
import { computed, watch } from 'vue'
import { t } from '../../i18n'
import { channelName } from '../../checkout'
import type { PayChoice, PayOptions } from '../../types'

const props = defineProps<{ options: PayOptions | null; types?: ('cash' | 'card' | 'online')[] }>()
const model = defineModel<PayChoice>({ required: true })
const shown = computed(() => (props.types ?? ['cash', 'card', 'online']).filter(x =>
  !props.options || (x === 'cash' ? props.options.cash : x === 'card' ? props.options.card : props.options.channels.length > 0)))
watch(shown, s => { if (s.length && !s.includes(model.value.type)) model.value = { type: s[0], channel_id: 0 } }, { immediate: true })
function pickType(x: PayChoice['type']) {
  const ch = props.options?.channels ?? []
  model.value = { type: x, channel_id: x === 'online' ? (ch.length === 1 ? ch[0].id : model.value.channel_id || 0) : 0 }
}
</script>

<template>
  <div class="pp">
    <div v-if="!shown.length" class="banner warn small">{{ t('ck.no_types') }}</div>
    <div class="pp-types">
      <button v-for="x in shown" :key="x" class="pp-t" :class="{ on: model.type === x }" :data-testid="'pay-' + x" @click="pickType(x)">{{ t('ck.pt.' + x) }}</button>
    </div>
    <div v-if="model.type === 'online'" class="pp-chs">
      <button v-for="c in options?.channels ?? []" :key="c.id" class="pp-c" :class="{ on: model.channel_id === c.id }" data-testid="pay-channel"
        @click="model = { type: 'online', channel_id: c.id }">
        <img v-if="c.icon_url" :src="c.icon_url" alt="" /><span v-else class="pp-i">{{ channelName(c).slice(0, 1) }}</span>{{ channelName(c) }}</button>
    </div>
  </div>
</template>

<style scoped>
.pp-types { display: grid; grid-template-columns: repeat(auto-fit, minmax(80px, 1fr)); gap: 8px; }
.pp-t { min-height: 46px; border-radius: 10px; border: 1px solid #e3d6c8; background: #fff; color: #2b1d16; font-size: 15px; font-weight: 600; cursor: pointer; }
.pp-t.on { border: 2px solid #a4161a; color: #a4161a; background: #fdf6f4; }
.pp-chs { display: grid; grid-template-columns: repeat(auto-fill, minmax(120px, 1fr)); gap: 8px; margin-top: 10px; }
.pp-c { display: flex; align-items: center; gap: 8px; min-height: 44px; padding: 6px 10px; border-radius: 10px; border: 1px solid #e3d6c8; background: #fff; color: #2b1d16; font-size: 14px; cursor: pointer; text-align: start; }
.pp-c.on { border: 2px solid #a4161a; background: #fdf6f4; }
.pp-c img, .pp-i { width: 24px; height: 24px; border-radius: 6px; flex: none; object-fit: cover; }
.pp-i { display: inline-flex; align-items: center; justify-content: center; background: #f3e9dc; color: #8a5a00; font-weight: 700; font-size: 13px; }
</style>
