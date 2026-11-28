<script setup lang="ts">
// The one "Languages" button (docs/I18N_SIMPLE_DESIGN.md §4.2). It sits next to
// whatever the merchant writes for customers: nothing of the other languages
// shows until it is clicked, and then LangsSheet opens for that one record.
// A red dot means the translation service keeps failing.
import { computed, ref } from 'vue'
import { t } from '../i18n'
import { can } from '../session'
import { langStatus } from '../langStatus'
import LangsSheet from './LangsSheet.vue'
import './langsStrings'

const props = defineProps<{
  base: string
  // "item/12", "branch/3"…; null while the record is not saved yet
  scope: string | null
  name: string
  small?: boolean
  // the platform's pages show it whatever the role
  always?: boolean
  readonly?: boolean
}>()
const open = ref(false)
// the platform's own pages (their address starts with /admin/) show it whatever the role
const show = computed(() => props.always || props.base.startsWith('/admin/') || can('menu.translate'))
const status = computed(() => langStatus(props.base))
</script>

<template>
  <button v-if="show" type="button" class="langs-btn" :class="{ small, on: open }" :disabled="!scope"
    :title="scope ? t('lg.btn_hint') : t('lg.unsaved')" :aria-label="t('lg.btn')" data-testid="langs-btn" :data-scope="scope ?? ''" @click.stop="open = true">
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true"><circle cx="12" cy="12" r="9" /><path d="M3 12h18M12 3c2.8 3 2.8 15 0 18M12 3c-2.8 3-2.8 15 0 18" /></svg>
    <span>{{ t('lg.btn') }}</span>
    <i v-if="status.failed" class="dot" data-testid="langs-failed" :title="t('lg.set.failed')"></i>
  </button>
  <Teleport v-if="open && scope" to="body">
    <LangsSheet :base="base" :scope="scope" :name="name" :readonly="readonly" @close="open = false" />
  </Teleport>
</template>

<style scoped>
.langs-btn { display: inline-flex; align-items: center; gap: 5px; position: relative; border: 1px solid var(--line, #e3d6c3); background: var(--card, #fff);
  color: #6b5a4e; border-radius: 16px; padding: 5px 12px; font: inherit; font-size: 13px; line-height: 1.3; cursor: pointer; white-space: nowrap; }
.langs-btn:hover:not(:disabled), .langs-btn.on { border-color: var(--brand, #a02c20); color: var(--brand, #a02c20); }
.langs-btn:disabled { opacity: .5; cursor: default; }
.langs-btn.small { padding: 2px 9px; font-size: 12px; }
.langs-btn svg { width: 15px; height: 15px; flex: none; }
.langs-btn.small svg { width: 13px; height: 13px; }
.dot { position: absolute; top: -3px; inset-inline-end: -3px; width: 9px; height: 9px; border-radius: 50%; background: #d0342c; border: 2px solid #fff; }
</style>
