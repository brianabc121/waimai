<script setup lang="ts">
// The telephone code before a number: a button showing "+63 ▾" that opens a
// list of countries to search by name, code or "+86". The countries where most
// overseas Chinese customers are come first, after the one given as `first`
// (usually the store's country).
import { computed, nextTick, ref } from 'vue'
import { CODES, COMMON, countryName } from '../dial'
import { extend, intlTag, t } from '../i18n'

extend({
  'dp.title': ['选择国家或地区', 'Choose a country or region'],
  'dp.search': ['搜索国家、地区或区号', 'Search a country or code'],
  'dp.common': ['常用', 'Common'], 'dp.all': ['全部', 'All'],
  'dp.none': ['没有找到', 'Nothing found'],
})

defineOptions({ inheritAttrs: false })
const props = defineProps<{ modelValue: string; first?: string; label?: string; testid?: string }>()
const emit = defineEmits<{ 'update:modelValue': [dial: string] }>()

const open = ref(false), q = ref('')
const box = ref<HTMLInputElement | null>(null)
// Which of the countries sharing a code (+1, +7, +44 …) was picked, to tick it.
const picked = ref('')

interface Row { cc: string; dial: string; name: string; en: string }
const rows = computed<Row[]>(() => {
  const tag = intlTag()
  return Object.entries(CODES).map(([cc, dial]) => ({ cc, dial, name: countryName(cc, tag), en: countryName(cc, 'en') }))
})
const byCC = computed(() => new Map(rows.value.map(r => [r.cc, r])))
const common = computed(() => {
  const first = (props.first ?? '').toUpperCase()
  return [...new Set([first, ...COMMON])].map(cc => byCC.value.get(cc)).filter((r): r is Row => !!r)
})
const all = computed(() => [...rows.value].sort((a, b) => a.name.localeCompare(b.name, intlTag())))
const found = computed(() => {
  const s = q.value.trim().toLowerCase().replace(/^\+/, '')
  if (!s) return null
  const digits = /^\d+$/.test(s)
  return all.value.filter(r => digits ? r.dial.startsWith(s) : r.name.toLowerCase().includes(s) || r.en.toLowerCase().includes(s) || r.cc.toLowerCase() === s)
    .sort((a, b) => digits ? Number(b.dial === s) - Number(a.dial === s) || a.dial.length - b.dial.length
      : Number(starts(b, s)) - Number(starts(a, s)))
})
// A name that begins with what was typed comes before one that only contains it.
const starts = (r: Row, s: string) => r.name.toLowerCase().startsWith(s) || r.en.toLowerCase().startsWith(s)
const isOn = (r: Row) => r.dial === props.modelValue && (!picked.value || picked.value === r.cc)

async function show() {
  q.value = ''; open.value = true
  await nextTick()
  // A phone's keyboard would cover half the list: focus only where there is room.
  if (window.matchMedia('(min-width: 768px)').matches) box.value?.focus()
}
function choose(r: Row) {
  picked.value = r.cc
  emit('update:modelValue', r.dial)
  open.value = false
}
</script>

<template>
  <button type="button" class="dp-btn" v-bind="$attrs" :aria-label="label || t('dp.title')" :data-testid="testid" :data-dial="modelValue" @click="show">
    +{{ modelValue }}<span class="dp-caret" aria-hidden="true">▾</span>
  </button>
  <Teleport to="body">
    <div v-if="open" class="sheet-mask" @click.self="open = false">
      <div class="sheet dp-sheet" role="dialog" :aria-label="t('dp.title')" data-testid="dial-sheet">
        <div class="sheet-head dp-head">
          <div class="row between"><b>{{ t('dp.title') }}</b><button type="button" class="dp-x" :aria-label="t('close')" @click="open = false">×</button></div>
          <input ref="box" v-model="q" type="search" class="dp-q" :placeholder="t('dp.search')" data-testid="dial-search" />
        </div>
        <div class="sheet-body dp-body">
          <template v-if="found">
            <button v-for="r in found" :key="r.cc" type="button" class="dp-row" :class="{ on: isOn(r) }" :data-cc="r.cc" @click="choose(r)">
              <span class="dp-name">{{ r.name }}</span><span class="dp-dial">+{{ r.dial }}</span></button>
            <div v-if="!found.length" class="muted small dp-none">{{ t('dp.none') }}</div>
          </template>
          <template v-else>
            <div class="dp-sec">{{ t('dp.common') }}</div>
            <button v-for="r in common" :key="'c' + r.cc" type="button" class="dp-row" :class="{ on: isOn(r) }" :data-cc="r.cc" @click="choose(r)">
              <span class="dp-name">{{ r.name }}</span><span class="dp-dial">+{{ r.dial }}</span></button>
            <div class="dp-sec">{{ t('dp.all') }}</div>
            <button v-for="r in all" :key="r.cc" type="button" class="dp-row" :class="{ on: isOn(r) }" @click="choose(r)">
              <span class="dp-name">{{ r.name }}</span><span class="dp-dial">+{{ r.dial }}</span></button>
          </template>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.dp-btn { border: 0; background: #f3f4f6; border-radius: 8px; padding: 6px 8px 6px 10px; font: inherit; font-weight: 600; color: inherit; cursor: pointer;
  display: inline-flex; align-items: center; gap: 4px; flex: none; white-space: nowrap; }
.dp-btn:focus-visible { outline: 2px solid var(--brand); outline-offset: 2px; }
.dp-caret { font-size: 10px; color: #9aa1ab; }
.dp-sheet { height: 80vh; }
.dp-head { display: flex; flex-direction: column; gap: 10px; }
.dp-x { border: 0; background: none; font-size: 24px; line-height: 1; color: #9aa1ab; cursor: pointer; padding: 0 4px; }
.dp-q { width: 100%; height: 40px; border: 1px solid #cfd4da; border-radius: 10px; padding: 0 12px; font-size: 16px; box-sizing: border-box; }
.dp-q:focus { outline: none; border-color: var(--brand); }
.dp-body { padding-top: 4px; flex: 1; }
.dp-sec { font-size: 12px; color: var(--muted); padding: 12px 4px 4px; }
.dp-row { display: flex; width: 100%; justify-content: space-between; align-items: center; gap: 12px; border: 0; border-bottom: 1px solid var(--line);
  background: none; padding: 12px 4px; font: inherit; font-size: 15px; color: inherit; cursor: pointer; text-align: start; }
.dp-row:hover { background: #fafafa; }
.dp-row.on { color: var(--brand); font-weight: 600; }
.dp-dial { color: var(--muted); flex: none; }
.dp-row.on .dp-dial { color: var(--brand); }
.dp-none { padding: 20px 4px; text-align: center; }
</style>
