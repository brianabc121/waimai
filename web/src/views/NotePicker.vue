<script setup lang="ts">
// The flavour notes of a dish as tag chips: each group says whether one or
// several may be picked; a pick-one group swaps the choice. Used by the store
// page's option sheet, the table menu and the order taken by staff.
import { mn, t } from '../i18n'
import { toggleTag } from '../notes'
import type { NoteGroup } from '../types'

const props = defineProps<{ groups: NoteGroup[]; modelValue: number[]; filter?: string }>()
const emit = defineEmits<{ 'update:modelValue': [ids: number[]] }>()

const shown = (name: string) => !props.filter || name.toLowerCase().includes(props.filter.toLowerCase())
</script>

<template>
  <div class="np" data-testid="note-picker">
    <div v-for="g in groups" :key="g.id" class="np-g">
      <div class="np-gn"><b v-if="g.name">{{ mn(g) }}</b><small>{{ g.multi ? t('nt.multi') : t('nt.single') }}</small></div>
      <div class="np-chips">
        <button v-for="x in g.tags" :key="x.id" type="button" class="np-chip" :class="{ on: modelValue.includes(x.id), dim: !shown(mn(x)) && !modelValue.includes(x.id) }"
          :aria-pressed="modelValue.includes(x.id)" data-testid="note-tag" @click="emit('update:modelValue', toggleTag(modelValue, g, x.id))">{{ mn(x) }}</button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.np { display: grid; gap: 10px; }
.np-gn { display: flex; align-items: baseline; gap: 6px; margin-bottom: 6px; font-size: 13px; }
.np-gn small { color: var(--muted); font-size: 12px; }
.np-chips { display: flex; flex-wrap: wrap; gap: 8px; }
.np-chip { border: 1px solid var(--line); background: #fff; border-radius: 8px; padding: 6px 12px; font: inherit; font-size: 13.5px; color: var(--ink);
  cursor: pointer; min-height: 34px; max-width: 100%; overflow-wrap: anywhere; text-align: start; }
.np-chip.on { border-color: var(--brand); background: var(--gold-soft, var(--brand-soft)); color: var(--brand-dark); font-weight: 600; }
.np-chip.dim { opacity: .35; }
</style>
