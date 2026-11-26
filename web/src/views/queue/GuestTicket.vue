<script setup lang="ts">
// The guest's number (docs/QUEUE_DISPLAY_DESIGN.md §8): how many are ahead,
// the wait, and — when called — a red page that vibrates. The printed slip's
// QR code opens this page too.
import { storePath } from '../../site'
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { ApiError, get, post } from '../../api'
import { pick, t } from '../../i18n'
import { useLive } from '../../live'
import LangButton from '../LangButton.vue'
import { groupName, minsSince, placeText, qErr, type GuestTicket } from '../../queue'
import { saveToken } from './guest'

const props = defineProps<{ token: string }>()
const g = ref<GuestTicket | null>(null)
const missing = ref(false)
const error = ref('')

async function load() {
  try {
    g.value = await get<GuestTicket>(`/queue/tickets/${encodeURIComponent(props.token)}`)
    saveToken(g.value.queue.slug, props.token)
  } catch (e) { if (e instanceof ApiError && e.status === 404) missing.value = true; else throw e }
}
useLive(() => missing.value ? null : `/queue/tickets/${encodeURIComponent(props.token)}/events`, load)

const store = computed(() => g.value ? pick(g.value.queue.store, g.value.queue.store_en) : '')
const open = computed(() => g.value && ['waiting', 'called', 'skipped'].includes(g.value.status))

// the bar fills as the line moves: the most seen ahead is the empty bar
const maxAhead = ref(0)
watch(() => g.value?.ahead, a => { if (a != null && a > maxAhead.value) maxAhead.value = a })
const progress = computed(() => !g.value ? 0 : maxAhead.value ? Math.round((1 - g.value.ahead / maxAhead.value) * 90) + 10 : 10)

// called: vibrate and notify once per call
const now = ref(Date.now())
let tick = 0
onMounted(() => { tick = window.setInterval(() => { now.value = Date.now() }, 15000) })
onBeforeUnmount(() => window.clearInterval(tick))
const left = computed(() => !g.value?.called_at ? 0 : Math.max(0, g.value.queue.settings.call_alert_minutes - minsSince(g.value.called_at, now.value)))
let knownCalls = -1
watch(() => g.value && [g.value.status, g.value.calls] as const, v => {
  if (!v) return
  const [st, calls] = v
  if (knownCalls >= 0 && st === 'called' && calls > knownCalls) {
    try { navigator.vibrate?.([400, 200, 400, 200, 800]) } catch { /* no vibration */ }
    try {
      if ('Notification' in window && Notification.permission === 'granted')
        new Notification(t('qu.g.notify_title', { store: store.value }), { body: `${g.value!.number} · ${placeText(g.value!.table)}` })
    } catch { /* not allowed here */ }
  }
  knownCalls = calls
})

const canNotify = 'Notification' in window
const notifyOn = ref(canNotify && Notification.permission === 'granted')
async function askNotify() {
  try { notifyOn.value = (await Notification.requestPermission()) === 'granted' } catch { notifyOn.value = false }
}

const cancelling = ref(false)
async function cancel() {
  if (!confirm(t('qu.g.cancel_q'))) return
  cancelling.value = true
  try { await post(`/queue/tickets/${encodeURIComponent(props.token)}/cancel`, {}); await load() }
  catch (e) { error.value = qErr(e) }
  cancelling.value = false
}
</script>

<template>
  <div class="warm qt" :class="{ called: g?.status === 'called' }">
    <header class="qt-top">
      <div class="row">
        <img v-if="g?.queue.logo" class="av" :src="g.queue.logo" alt="" />
        <div v-else class="av">{{ store.slice(0, 1) || '·' }}</div>
        <div class="grow"><h1>{{ store || t('qu.g.title') }}</h1><small v-if="g">{{ groupName(g.group) }} · {{ t('qu.people', { n: g.people }) }}</small></div>
        <LangButton class="qt-lang" />
      </div>
    </header>

    <main v-if="missing" class="qt-body"><div class="pcard center" data-testid="qt-missing">{{ t('qu.g.notfound') }}</div></main>

    <!-- it's my turn -->
    <main v-else-if="g && g.status === 'called'" class="callme" data-testid="qt-called">
      <div class="ring">🔔</div>
      <h2>{{ t('qu.g.called') }}</h2>
      <div class="no">{{ g.number }}</div>
      <div class="to" data-testid="qt-place">{{ placeText(g.table) }}</div>
      <p>{{ t('qu.people', { n: g.people }) }}</p>
      <div class="cd">{{ t('qu.g.come', { n: left }) }}</div>
    </main>

    <main v-else-if="g" class="qt-body">
      <div v-if="g.status === 'waiting'" class="bigt" data-testid="qt-waiting">
        <small>{{ t('qu.g.title') }}</small>
        <b data-testid="qt-number">{{ g.number }}</b>
        <div class="meta">{{ groupName(g.group) }} · {{ t('qu.people', { n: g.people }) }}</div>
        <div class="ahead">
          <div><b data-testid="qt-ahead">{{ g.ahead }}</b><small>{{ t('qu.g.ahead') }} · {{ t('qu.g.unit_tables') }}</small></div>
          <div><b>{{ g.est_minutes }}</b><small>{{ t('qu.g.est') }} · {{ t('qu.g.unit_min') }}</small></div>
          <div><b>{{ g.group.current || '—' }}</b><small>{{ t('qu.g.current') }}</small></div>
        </div>
        <div class="prog"><i :style="{ width: progress + '%' }"></i></div>
      </div>
      <div v-else-if="g.status === 'skipped'" class="missedp" data-testid="qt-skipped">
        <b>{{ g.number }}</b>{{ t('qu.g.skipped') }}<p>{{ t('qu.g.skipped_d') }}</p>
      </div>
      <div v-else class="pcard center" data-testid="qt-closed">
        <b class="dim">{{ g.number }}</b>
        <p>{{ t(g.status === 'seated' ? 'qu.g.seated' : g.status === 'cancelled' ? 'qu.g.cancelled' : 'qu.g.void') }}</p>
        <router-link class="pbtn" :to="`/q/${g.queue.slug}`">{{ t('qu.g.again') }}</router-link>
      </div>

      <template v-if="open">
        <p class="keep">{{ t('qu.g.keep') }}</p>
        <div class="row2">
          <router-link class="pbtn ghost" :to="storePath(g.queue.slug)">{{ t('qu.g.menu') }}</router-link>
          <button v-if="canNotify" class="pbtn ghost" :disabled="notifyOn" @click="askNotify">{{ notifyOn ? t('qu.g.notify_on') : t('qu.g.notify') }}</button>
        </div>
        <button v-if="g.status === 'waiting'" class="pbtn ghost gray" :disabled="cancelling" data-testid="qt-cancel" @click="cancel">{{ t('qu.g.cancel') }}</button>
        <p v-if="error" class="err">{{ error }}</p>
      </template>
    </main>
  </div>
</template>

<style scoped>
.qt { min-height: 100vh; min-height: 100dvh; display: flex; flex-direction: column; }
.qt-top { background: var(--red-bg); color: #fff; padding: 18px 16px 16px; }
.qt-top .row { display: flex; align-items: center; gap: 10px; max-width: 560px; margin: 0 auto; }
.av { width: 44px; height: 44px; border-radius: 12px; background: var(--gold); color: var(--red-deep); font-weight: 900; display: grid; place-items: center; font-size: 20px; object-fit: cover; flex: none; }
.qt-top h1 { margin: 0; font-size: 17px; } .qt-top small { color: var(--gold-soft); font-size: 12.5px; }
.grow { flex: 1; min-width: 0; }
.qt-lang { color: #fff; }
.qt-body { padding: 14px; display: flex; flex-direction: column; gap: 12px; width: 100%; max-width: 560px; margin: 0 auto; }
.pcard { background: var(--paper); border: 1px solid var(--line); border-radius: 14px; padding: 12px 14px; }
.pcard.center { text-align: center; padding: 26px 18px; color: var(--muted); display: grid; gap: 8px; }
.dim { font-size: 48px; color: #b9aea0; text-decoration: line-through; }
.bigt { text-align: center; padding: 18px 14px; background: var(--paper); border: 1px solid var(--line); border-radius: 16px; }
.bigt small { color: var(--muted); font-size: 13px; }
.bigt > b { display: block; font-size: 64px; font-weight: 900; color: var(--red); line-height: 1.1; letter-spacing: 2px; font-variant-numeric: tabular-nums; }
.bigt .meta { font-size: 14px; color: #5b4636; }
.ahead { display: flex; text-align: center; border-top: 1px dashed var(--line); margin-top: 12px; padding-top: 12px; }
.ahead div { flex: 1; min-width: 0; } .ahead b { display: block; font-size: 26px; color: var(--ink); line-height: 1.2; } .ahead small { font-size: 12px; }
.prog { height: 8px; border-radius: 999px; background: #efe3cf; overflow: hidden; margin-top: 12px; }
.prog i { display: block; height: 100%; background: linear-gradient(90deg, var(--gold), var(--red)); transition: width .6s; }
.keep { color: var(--muted); font-size: 12.5px; text-align: center; margin: 0; }
.row2 { display: flex; gap: 10px; } .row2 > * { flex: 1; }
.pbtn { display: block; text-align: center; background: var(--red); color: #fff; border: 0; border-radius: 999px; padding: 13px; font-weight: 800; font-size: 16px; cursor: pointer; text-decoration: none; }
.pbtn.ghost { background: #fff; color: var(--red); border: 1.5px solid var(--red); padding: 10px; font-size: 14px; }
.pbtn.gray { border-color: var(--line); color: var(--muted); }
.pbtn:disabled { opacity: .6; cursor: default; }
.missedp { background: #fff3e6; border: 1.5px dashed #e8b27a; border-radius: 16px; padding: 18px; text-align: center; color: #a2510a; font-weight: 700; }
.missedp b { display: block; font-size: 48px; text-decoration: line-through; text-decoration-thickness: 3px; }
.missedp p { margin: 6px 0 0; font-weight: 500; }
.err { color: #b42318; font-weight: 700; text-align: center; }
.callme { flex: 1; background: var(--red); background-image: var(--motif); background-size: 56px 56px; color: #fff; display: flex; flex-direction: column; align-items: center; justify-content: center; text-align: center; padding: 30px 24px; gap: 8px; }
.callme .ring { width: 96px; height: 96px; border-radius: 50%; background: var(--gold); color: var(--red-deep); display: grid; place-items: center; font-size: 46px; box-shadow: 0 0 0 14px rgba(244,207,115,.25), 0 0 0 30px rgba(244,207,115,.12); margin-bottom: 26px; animation: ring 1.2s ease-in-out infinite; }
@keyframes ring { 50% { transform: scale(1.08); } }
.callme h2 { margin: 0; font-size: 34px; } .callme .no { font-size: 68px; font-weight: 900; color: var(--gold); letter-spacing: 3px; line-height: 1.1; }
.callme .to { font-size: 24px; font-weight: 800; background: rgba(255,255,255,.12); border-radius: 14px; padding: 10px 22px; }
.callme p { color: var(--gold-soft); font-size: 14px; margin: 6px 0 0; }
.callme .cd { font-size: 15px; background: rgba(0,0,0,.18); border-radius: 999px; padding: 6px 16px; margin-top: 14px; }
@media (prefers-reduced-motion: reduce) { .callme .ring { animation: none; } }
</style>
