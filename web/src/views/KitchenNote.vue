<script setup lang="ts">
// A line's flavour notes and own note on the store's screens (orders,
// kitchen, packing): the tags as marked blocks, the note after "Note:".
import { t } from '../i18n'
import '../notes'
import type { LineNoteTag } from '../types'

defineProps<{ it: { note?: string; note_tags?: LineNoteTag[] }; inline?: boolean }>()
</script>

<template>
  <component :is="inline ? 'span' : 'div'" v-if="it.note || it.note_tags?.length" class="note-kitchen" data-testid="line-notes">
    <span v-for="x in it.note_tags ?? []" :key="x.id" class="t">{{ x.name }}</span>
    <template v-if="it.note"><br v-if="it.note_tags?.length && !inline" />{{ t('note') }}: {{ it.note }}</template>
  </component>
</template>
