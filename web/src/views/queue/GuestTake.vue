<script setup lang="ts">
// "Take a number" (docs/QUEUE_DISPLAY_DESIGN.md §8): the QR code at the door
// opens this page. A browser holds one number per store; coming back here
// with a number still in line goes straight to it.
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ApiError, get, newKey, post } from '../../api'
import { locale, pick, t } from '../../i18n'
import { useLive } from '../../live'
import LangButton from '../LangButton.vue'
import { estText, groupName, groupRange, qErr, type QueuePublic } from '../../queue'
import { guestClient, savedToken, saveToken } from './guest'

const props = defineProps<{ slug: string }>()
const router = useRouter()
const q = ref<QueuePublic | null>(null)
const error = ref('')
const missing = ref(false)

async function load() {
  try { q.value = await get<QueuePublic>(`/queue/${encodeURIComponent(props.slug)}`) }
  catch (e) { if (e instanceof ApiError && e.status === 404) missing.value = true; else throw e }
}
useLive(() => null, load)

onMounted(async () => {
  const tok = savedToken(props.slug)
  if (!tok) return
  const g = await get<{ status: string }>(`/queue/tickets/${tok}`).catch(() => null)
  if (g && (g.status === 'waiting' || g.status === 'called' || g.status === 'skipped')) void router.replace(`/q/t/${tok}`)
})

const people = ref(0)
const more = ref(false)
const phone = ref('')
const agree = ref(false)
const busy = ref(false)
const store = computed(() => q.value ? pick(q.value.store, q.value.store_en) : '')
const canTake = computed(() => !!q.value?.open && !q.value.paused)
const fits = computed(() => q.value?.groups.find(g => people.value >= g.min_people && people.value <= g.max_people))

async function take() {
  if (!people.value || !agree.value) return
  busy.value = true
  error.value = ''
  try {
    const r = await post<{ token: string }>(`/queue/${encodeURIComponent(props.slug)}/tickets`,
      { people: people.value, phone: phone.value.trim(), client: guestClient(), lang: locale.value }, { 'Idempotency-Key': newKey() })
    saveToken(props.slug, r.token)
    void router.replace(`/q/t/${r.token}`)
  } catch (e) {
    const tok = savedToken(props.slug)
    if (e instanceof ApiError && e.code === 'QUEUE_HAS_TICKET' && tok) { void router.replace(`/q/t/${tok}`); return }
    error.value = qErr(e)
  }
  busy.value = false
}
</script>

<template>
  <div class="warm qg">
    <header class="qg-top">
      <div class="row">
        <img v-if="q?.logo" class="av" :src="q.logo" alt="" />
        <div v-else class="av">{{ store.slice(0, 1) || '·' }}</div>
        <div class="grow"><h1 data-testid="qg-store">{{ store || t('qu.g.title') }}</h1>
          <small>{{ q ? (canTake ? t('qu.g.open') : t('qu.g.closed')) : '' }}</small></div>
        <LangButton class="qg-lang" />
      </div>
    </header>
    <main class="qg-body">
      <div v-if="missing" class="pcard center">{{ t('qu.g.notfound') }}</div>
      <template v-else-if="q">
        <div v-if="!canTake" class="pcard warnbox" data-testid="qg-closed">{{ t('qu.g.closed') }}</div>
        <section class="pcard">
          <h2>{{ t('qu.g.lines') }}</h2>
          <div v-for="g in q.groups" :key="g.id" class="gl" :class="{ on: fits?.id === g.id }">
            <span class="l">{{ g.letter }}</span>{{ groupName(g) }} <small>{{ groupRange(g) }}</small>
            <em v-if="g.waiting">{{ t('qu.g.waiting_n', { n: g.waiting }) + ' · ' + estText(g.est_minutes) }}</em><em v-else>{{ t('qu.g.line_none') }}</em>
          </div>
        </section>
        <section v-if="canTake" class="pcard">
          <h2>{{ t('qu.g.people') }}</h2>
          <div class="ppg">
            <button v-for="n in 12" :key="n" :class="{ on: people === n }" :data-testid="'qg-p' + n" @click="people = n; more = false">{{ n }}</button>
          </div>
          <div class="morep">
            <button class="lnk" :class="{ on: more }" @click="more = true; if (people <= 12) people = 13">{{ t('qu.t.more') }}</button>
            <input v-if="more" v-model.number="people" type="number" min="13" max="99" inputmode="numeric" />
          </div>
          <h2 class="mt">{{ t('qu.t.phone') }} <small>{{ t('qu.g.phone_hint') }}</small></h2>
          <input v-model="phone" class="pin" type="tel" inputmode="tel" autocomplete="tel" data-testid="qg-phone" />
          <label class="agree"><input v-model="agree" type="checkbox" data-testid="qg-agree" /> {{ t('qu.g.agree', { n: q.settings.requeue_after }) }}</label>
          <p v-if="error" class="err" data-testid="qg-error">{{ error }}</p>
          <button class="pbtn" :disabled="busy || !people || !agree" data-testid="qg-take" @click="take">{{ t('qu.g.take') }}</button>
        </section>
      </template>
    </main>
  </div>
</template>

<style scoped>
.qg { min-height: 100vh; min-height: 100dvh; }
.qg-top { background: var(--red-bg); color: #fff; padding: 18px 16px 16px; }
.qg-top .row { display: flex; align-items: center; gap: 10px; max-width: 560px; margin: 0 auto; }
.av { width: 44px; height: 44px; border-radius: 12px; background: var(--gold); color: var(--red-deep); font-weight: 900; display: grid; place-items: center; font-size: 20px; object-fit: cover; flex: none; }
.qg-top h1 { margin: 0; font-size: 17px; } .qg-top small { color: var(--gold-soft); font-size: 12.5px; }
.grow { flex: 1; min-width: 0; }
.qg-lang { color: #fff; }
.qg-body { padding: 14px; display: flex; flex-direction: column; gap: 12px; max-width: 560px; margin: 0 auto; }
.pcard { background: var(--paper); border: 1px solid var(--line); border-radius: 14px; padding: 12px 14px; }
.pcard.center { text-align: center; padding: 30px; color: var(--muted); }
.warnbox { background: #fff3e6; border-color: #e8b27a; color: #a2510a; font-weight: 700; text-align: center; }
.pcard h2 { margin: 0 0 8px; font-size: 15px; } .pcard h2 small { color: var(--muted); font-weight: 500; font-size: 12px; }
.pcard h2.mt { margin-top: 14px; }
.gl { display: flex; align-items: center; gap: 8px; padding: 7px 0; font-size: 13.5px; border-top: 1px dashed #eadbc5; flex-wrap: wrap; }
.gl:first-of-type { border-top: 0; }
.gl.on { color: var(--red); font-weight: 700; }
.gl .l { width: 22px; height: 22px; border-radius: 6px; background: var(--red); color: #fff; font-weight: 800; display: grid; place-items: center; font-size: 12px; }
.gl small { color: var(--muted); } .gl em { font-style: normal; margin-inline-start: auto; color: var(--muted); }
.ppg { display: grid; grid-template-columns: repeat(6, 1fr); gap: 6px; }
.ppg button { height: 42px; border-radius: 10px; border: 1.5px solid var(--line); background: #fff; font-weight: 800; font-size: 16px; color: var(--ink); cursor: pointer; }
.ppg button.on { background: var(--red); color: #fff; border-color: var(--red); }
.morep { display: flex; align-items: center; gap: 10px; margin-top: 8px; }
.lnk { border: 0; background: none; color: var(--red); font-weight: 700; padding: 4px 0; cursor: pointer; }
.morep input { width: 90px; }
.pin { width: 100%; }
.agree { display: flex; gap: 8px; align-items: flex-start; font-size: 13px; color: var(--muted); margin: 12px 0; line-height: 1.5; }
.err { color: #b42318; font-weight: 700; margin: 0 0 10px; }
.pbtn { width: 100%; background: var(--red); color: #fff; border: 0; border-radius: 999px; padding: 13px; font-weight: 800; font-size: 16px; cursor: pointer; }
.pbtn:disabled { opacity: .5; cursor: default; }
</style>
