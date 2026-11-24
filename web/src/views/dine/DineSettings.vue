<script setup lang="ts">
// The dine-in desk's choices, for the owner and managers: the tags staff tap
// (table notes, whole-round notes, void reasons, payment methods), when a
// table turns red, where the desk goes after sending a round, and photos.
import { reactive, ref } from 'vue'
import { get, put } from '../../api'
import { t } from '../../i18n'
import { session } from '../../session'
import type { DinePresets } from '../../types'
import { presetList } from './strings'

const props = defineProps<{ presets: DinePresets | null; country: string }>()
const emit = defineEmits<{ close: []; saved: [p: DinePresets] }>()
type Which = 'table_tags' | 'round_tags' | 'void_reasons' | 'pay_methods'
const LISTS: [Which, string][] = [['table_tags', 'dd.set.table_tags'], ['round_tags', 'dd.set.round_tags'], ['void_reasons', 'dd.set.void'], ['pay_methods', 'dd.set.pay']]

const form = reactive({
  lists: Object.fromEntries(LISTS.map(([w]) => [w, [...presetList(props.presets, w, props.country)]])) as Record<Which, string[]>,
  long: props.presets?.long_minutes || 90,
  stay: !!props.presets?.stay_after_send,
  photos: props.presets?.show_photos !== false,
})
const adding = reactive<Record<string, string | null>>({})
function add(w: Which) {
  const s = (adding[w] ?? '').trim().slice(0, 20)
  if (s && !form.lists[w].includes(s) && form.lists[w].length < 10) form.lists[w].push(s)
  adding[w] = null
}
const drop = (w: Which, x: string) => { form.lists[w] = form.lists[w].filter(y => y !== x) }
const error = ref('')
const busy = ref(false)
async function save(reset = false) {
  busy.value = true; error.value = ''
  const body: DinePresets = reset
    ? { table_tags: null, round_tags: null, void_reasons: null, pay_methods: null, long_minutes: 0, stay_after_send: false, show_photos: null }
    : { ...form.lists, long_minutes: form.long, stay_after_send: form.stay, show_photos: form.photos }
  try {
    await put(`/merchant/branches/${session.branchId}/dine-desk`, body)
    const r = await get<{ presets: DinePresets }>(`/merchant/branches/${session.branchId}/dine-desk`)
    emit('saved', r.presets); emit('close')
  } catch (e) { error.value = (e as Error).message }
  busy.value = false
}
</script>

<template>
  <div class="dd-mask" @click.self="emit('close')">
    <div class="dd-modal wide" data-testid="dd-settings-panel">
      <div class="mh"><h3>{{ t('dd.settings') }}</h3><button class="x" @click="emit('close')">✕</button></div>
      <div class="mb scroll">
        <div class="set-row"><span class="dd-lbl">{{ t('dd.set.long') }}</span>
          <div class="dd-chs sm"><button v-for="n in [60, 90, 120, 150, 180]" :key="n" class="dd-ch" :class="{ on: form.long === n }" @click="form.long = n">{{ t('dd.min', { n }) }}</button></div></div>
        <div class="set-row"><span class="dd-lbl">{{ t('dd.set.after') }}</span>
          <div class="dd-chs sm"><button class="dd-ch" :class="{ on: !form.stay }" @click="form.stay = false">{{ t('dd.set.after_tables') }}</button>
            <button class="dd-ch" :class="{ on: form.stay }" @click="form.stay = true">{{ t('dd.set.after_stay') }}</button></div></div>
        <div class="set-row"><span class="dd-lbl">{{ t('dd.set.photos') }}</span>
          <div class="dd-chs sm"><button class="dd-ch" :class="{ on: form.photos }" @click="form.photos = true">{{ t('dd.on') }}</button>
            <button class="dd-ch" :class="{ on: !form.photos }" @click="form.photos = false">{{ t('dd.off') }}</button></div></div>
        <div class="muted small" style="margin:12px 0 6px">{{ t('dd.set.hint') }}</div>
        <div v-for="[w, label] in LISTS" :key="w" class="set-list">
          <div class="dd-lbl">{{ t(label) }}</div>
          <div class="dd-chs sm">
            <button v-for="x in form.lists[w]" :key="x" class="dd-ch" @click="drop(w, x)">{{ x }} ✕</button>
            <button v-if="adding[w] == null" class="dd-ch add" @click="adding[w] = ''">{{ t('dd.other') }}</button>
            <span v-else class="dd-write"><input v-model="adding[w]" maxlength="20" :placeholder="t('dd.write_ph')" @keydown.enter.prevent="add(w)" />
              <button class="dd-ch on" @click="add(w)">{{ t('dd.write_add') }}</button></span>
          </div>
        </div>
        <div v-if="error" class="banner bad small">{{ error }}</div>
      </div>
      <div class="mf"><button class="dd-big ghost" :disabled="busy" @click="save(true)">{{ t('dd.set.reset') }}</button>
        <button class="dd-big" style="flex:2" :disabled="busy" data-testid="dd-settings-save" @click="save()">{{ t('dd.save') }}</button></div>
    </div>
  </div>
</template>
