<script setup lang="ts">
// The message box: order updates, answers from the platform, wallet changes.
import { ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { pick, t } from '../../i18n'
import MePage from './MePage.vue'
import { dateTime, describe, me, myMessages, readMessages, type Message } from './me'

const router = useRouter()
const list = ref<Message[] | null>(null)
const more = ref(false), error = ref('')
async function load(reset = false) {
  try {
    const r = await myMessages(reset ? 0 : list.value?.length ?? 0)
    list.value = reset ? r.messages : [...(list.value ?? []), ...r.messages]
    more.value = r.more
  } catch (e) { error.value = describe(e) }
}
watch(() => me.profile?.id, id => { if (id) void load(true) }, { immediate: true })

async function open(m: Message) {
  if (!m.read) {
    m.read = true
    if (me.profile) me.profile.unread = Math.max(0, me.profile.unread - 1)
    await readMessages(m.id).catch(() => null)
  }
  if (m.link.startsWith('/') && !m.link.startsWith('//')) void router.push(m.link)
}
async function readAll() {
  await readMessages().catch(() => null)
  list.value?.forEach(m => { m.read = true })
  if (me.profile) me.profile.unread = 0
}
</script>

<template>
  <MePage :title="t('mm.title')">
    <template #right><button v-if="me.profile?.unread" class="btn link small" data-testid="msg-read-all" @click="readAll">{{ t('mm.read_all') }}</button></template>
    <div v-if="error" class="banner bad">{{ error }}</div>
    <div v-if="list && !list.length" class="card empty" data-testid="msg-empty">{{ t('mm.none') }}</div>
    <button v-for="m in list ?? []" :key="m.id" class="card msg" :class="{ unread: !m.read }" data-testid="msg" :data-kind="m.kind" @click="open(m)">
      <div class="row between"><span class="tag" :class="m.kind === 'order' ? 'brand' : m.kind === 'wallet' ? 'ok' : 'info'">{{ t('mm.kind_' + m.kind) }}</span>
        <span class="small muted">{{ dateTime(m.created_at) }}</span></div>
      <b><i v-if="!m.read" class="dot"></i>{{ pick(m.title, m.title_en) }}</b>
      <p v-if="m.body" class="small muted" style="white-space:pre-wrap">{{ pick(m.body, m.body_en) }}</p>
    </button>
    <button v-if="more" class="btn block" @click="load()">{{ t('mm.load_more') }}</button>
  </MePage>
</template>

<style scoped>
.msg { display: grid; gap: 6px; text-align: left; width: 100%; cursor: pointer; }
.msg b { font-size: 15px; } .msg:not(.unread) b { font-weight: 500; color: #4b5563; }
.dot { display: inline-block; width: 8px; height: 8px; border-radius: 50%; background: #e5352b; margin-right: 6px; vertical-align: 1px; }
</style>
