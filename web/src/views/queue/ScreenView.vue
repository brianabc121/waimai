<script setup lang="ts">
// The queue screen (docs/QUEUE_DISPLAY_DESIGN.md §5, §9): a TV or tablet
// paired with the store shows either the pickup numbers (tea shop) or the
// restaurant's line. It is drawn on a fixed 1920×1080 (or 1080×1920) stage
// scaled to the screen, so it looks the same on any TV.
import QRCode from 'qrcode'
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { ApiError, get, post } from '../../api'
import { loadLocale, locale, t, tIn, type Locale } from '../../i18n'
import { isLocale } from '../../langs'
import { useLive } from '../../live'
import type { PromoMedia } from '../../types'
import {
  announce, estText, groupName, groupRange, placeText, qErr, sayCall, soundReady, unlockSound,
  type QueuePublic, type QueueScreen, type TeaBoard,
} from '../../queue'

interface View {
  access: { screen: QueueScreen; slug: string; store: string; store_en: string; logo: string; timezone: string }
  paused: boolean; tea?: TeaBoard; queue?: QueuePublic; media: PromoMedia[]; ready_stay_minutes: number
}

const phase = ref<'loading' | 'pair' | 'ready'>('loading')
const view = ref<View | null>(null)
const error = ref('')

const lang = computed<Locale>(() => { const l = view.value?.access.screen.lang; return isLocale(l) ? l : locale.value })
const lang2 = computed<Locale | null>(() => { const l = view.value?.access.screen.lang2; return isLocale(l) && l !== lang.value ? l : null })
const T = (k: string, v?: Record<string, string | number>) => tIn(lang.value, k, v)
const T2 = (k: string, v?: Record<string, string | number>) => lang2.value ? tIn(lang2.value, k, v) : ''
watch([lang, lang2], ([a, b]) => { void loadLocale(a); if (b) void loadLocale(b) })

const screen = computed(() => view.value?.access.screen)
const portrait = computed(() => screen.value?.orientation === 'portrait')
const zhScreen = computed(() => lang.value === 'zh' || lang.value === 'zh-Hant')
const store = computed(() => { const a = view.value?.access; return !a ? '' : zhScreen.value ? a.store : (a.store_en || a.store) })

async function load() {
  try {
    const v = await get<View>('/queue-screens/current')
    view.value = v
    phase.value = 'ready'
    error.value = ''
    react(v)
  } catch (e) {
    if (e instanceof ApiError && (e.code === 'SCREEN_NOT_PAIRED' || e.status === 401)) phase.value = 'pair'
    else if (phase.value === 'loading') error.value = qErr(e)
    if (!(e instanceof ApiError) || e.status !== 401) throw e
  }
}
const live = useLive(() => phase.value === 'ready' ? '/queue-screens/current/events' : null, load)

// ---- pairing ----
const code = ref('')
const pairing = ref(false)
async function pair() {
  unlockSound()
  pairing.value = true
  error.value = ''
  try {
    await post('/queue-screens/pair', { code: code.value })
    first = true
    await load()
    live.reconnect()
  } catch (e) { error.value = qErr(e) }
  pairing.value = false
}

// ---- sound: announce what is new since the last read ----
const sound = ref(false)
let first = true
const seen = new Set<string>()
const flash = ref('') // the number just announced grows for a few seconds
let flashTimer = 0
function react(v: View) {
  const news: { key: string; no: string; text: string }[] = []
  if (screen.value?.mode === 'tea' && v.tea) {
    for (const r of v.tea.ready) {
      const key = `t${r.order_id}:${r.stamp ?? ''}`
      if (!seen.has(key)) news.push({ key, no: r.code, text: T('qu.sc.say_ready', { no: r.code }) })
    }
  } else if (v.queue) {
    for (const c of [...v.queue.calling].reverse()) {
      const key = `q${c.number}:${c.calls}`
      if (!seen.has(key)) news.push({ key, no: c.number, text: sayCall(c.number, c.table, T) })
    }
  }
  for (const n of news) seen.add(n.key)
  if (first) { first = false; return }
  const last = news[news.length - 1]
  if (!last) return
  flash.value = last.no
  window.clearTimeout(flashTimer)
  flashTimer = window.setTimeout(() => { flash.value = '' }, 6000)
  if (sound.value) for (const n of news.slice(-3)) announce(n.text, lang.value)
}
function tap() { if (unlockSound()) sound.value = true; else window.setTimeout(() => { sound.value = soundReady() }, 300) }

// ---- the stage: fixed size, scaled to the window ----
const scale = ref(1)
function fit() {
  const w = portrait.value ? 1080 : 1920, h = portrait.value ? 1920 : 1080
  scale.value = Math.min(window.innerWidth / w, window.innerHeight / h)
}
watch(portrait, fit)

// ---- clock, in the store's time zone ----
const now = ref(new Date())
const clock = computed(() => {
  try { return now.value.toLocaleTimeString('en-GB', { hour: '2-digit', minute: '2-digit', timeZone: view.value?.access.timezone || undefined }) }
  catch { return now.value.toLocaleTimeString('en-GB', { hour: '2-digit', minute: '2-digit' }) }
})

// ---- keep the screen on ----
let lock: { release(): Promise<void> } | null = null
async function wake() {
  try { lock = await (navigator as any).wakeLock?.request('screen') ?? null } catch { lock = null }
}
const onVisible = () => { if (document.visibilityState === 'visible') void wake() }

let tick = 0
onMounted(() => {
  fit()
  window.addEventListener('resize', fit)
  document.addEventListener('visibilitychange', onVisible)
  void wake()
  tick = window.setInterval(() => { now.value = new Date() }, 10000)
  sound.value = soundReady()
})
onBeforeUnmount(() => {
  window.removeEventListener('resize', fit)
  document.removeEventListener('visibilitychange', onVisible)
  window.clearInterval(tick)
  window.clearTimeout(mediaTimer)
  void lock?.release().catch(() => {})
})

// ---- tea board ----
const tea = computed(() => view.value?.tea)
const hero = computed(() => tea.value?.ready[0])
const caps = computed(() => portrait.value ? { wait: 12, cook: 12, ready: 9 } : { wait: 18, cook: 18, ready: 6 })
const readyRest = computed(() => (tea.value?.ready ?? []).slice(1, 1 + caps.value.ready))
const readyMore = computed(() => Math.max(0, (tea.value?.ready.length ?? 0) - 1 - caps.value.ready))

// ---- restaurant ----
const q = computed(() => view.value?.queue)
const calling = computed(() => q.value?.calling[0])
const recent = computed(() => (q.value?.calling ?? []).slice(1, 4))
const qr = ref('')
watch(() => view.value?.access.slug, async s => {
  if (!s) return
  qr.value = await QRCode.toDataURL(`${location.origin}/q/${s}`, { width: 360, margin: 1 }).catch(() => '')
}, { immediate: true })

// ---- promo media: one at a time ----
const media = computed(() => view.value?.media ?? [])
const mi = ref(0)
let mediaTimer = 0
const cur = computed(() => media.value.length ? media.value[mi.value % media.value.length] : null)
function nextMedia() { if (media.value.length) mi.value = (mi.value + 1) % media.value.length }
watch(cur, m => {
  window.clearTimeout(mediaTimer)
  if (m && m.kind !== 'video' && media.value.length > 1) mediaTimer = window.setTimeout(nextMedia, 8000)
}, { immediate: true })
function videoReady(e: Event) {
  const v = e.target as HTMLVideoElement
  if (cur.value?.trim_start) v.currentTime = cur.value.trim_start
  void v.play().catch(() => {})
}
function videoTime(e: Event) {
  const v = e.target as HTMLVideoElement
  if (cur.value?.trim_end && v.currentTime >= cur.value.trim_end) { if (media.value.length > 1) nextMedia(); else v.currentTime = cur.value.trim_start || 0 }
}
</script>

<template>
  <div class="qs-root" :class="{ portrait }" @click="tap">
    <!-- pairing -->
    <div v-if="phase !== 'ready'" class="qs-pair warm">
      <div class="qs-pcard">
        <h1>{{ t('qu.sc.pair_title') }}</h1>
        <p>{{ t('qu.sc.pair_hint') }}</p>
        <form v-if="phase === 'pair'" @submit.prevent="pair">
          <input v-model="code" inputmode="numeric" autocomplete="one-time-code" maxlength="7" placeholder="000000" data-testid="qs-code" />
          <button class="btn primary" :disabled="pairing || code.replace(/\s/g, '').length < 6" data-testid="qs-pair">{{ t('qu.sc.pair_go') }}</button>
        </form>
        <p v-if="error" class="qs-err" data-testid="qs-error">{{ error }}</p>
      </div>
    </div>

    <div v-else-if="view" class="qs-stage" :style="{ transform: `translate(-50%, -50%) scale(${scale})` }" :lang="lang" data-testid="qs-stage">
      <div class="tvtop">
        <img v-if="view.access.logo" class="logo" :src="view.access.logo" alt="" />
        <div v-else class="logo">{{ store.slice(0, 1) }}</div>
        <div class="ttl"><h1>{{ store }}</h1><div class="sub">{{ T(screen!.mode === 'tea' ? 'qu.sc.tea_sub' : 'qu.sc.rest_sub') }}<template v-if="lang2"> · {{ T2(screen!.mode === 'tea' ? 'qu.sc.tea_sub' : 'qu.sc.rest_sub') }}</template></div></div>
        <span v-if="!live.online.value" class="snd bad" data-testid="qs-offline">{{ T('qu.sc.offline') }}</span>
        <span class="snd" :class="{ off: !sound }" data-testid="qs-sound">{{ sound ? '🔊 ' + T('qu.sc.sound_on') : '🔇 ' + T('qu.sc.sound_off') }}</span>
        <div class="clock num">{{ clock }}</div>
      </div>

      <!-- tea shop -->
      <template v-if="screen!.mode === 'tea'">
        <div v-if="!tea" class="qs-off">{{ T('qu.sc.off') }}</div>
        <template v-else-if="!portrait">
          <div class="tea">
            <div class="tcol wait" data-testid="qs-wait">
              <h2>{{ T('qu.pk.wait') }} <small>{{ lang2 ? T2('qu.pk.wait') : T('qu.sc.wait_d') }}</small><span class="cnt num">{{ tea.wait.length }}</span></h2>
              <div class="nums num"><span v-for="n in tea.wait.slice(0, caps.wait)" :key="n.order_id">{{ n.code }}</span></div>
              <div v-if="tea.wait.length > caps.wait" class="more">{{ T('qu.sc.more', { n: tea.wait.length - caps.wait }) }}</div>
            </div>
            <div class="tcol cook" data-testid="qs-cook">
              <h2>{{ T('qu.pk.cook') }} <small>{{ lang2 ? T2('qu.pk.cook') : T('qu.sc.cook_d') }}</small><span class="cnt num">{{ tea.cook.length }}</span></h2>
              <div class="nums num"><span v-for="n in tea.cook.slice(0, caps.cook)" :key="n.order_id">{{ n.code }}</span></div>
              <div v-if="tea.cook.length > caps.cook" class="more">{{ T('qu.sc.more', { n: tea.cook.length - caps.cook }) }}</div>
            </div>
            <div class="tcol ready" data-testid="qs-ready">
              <h2>{{ T('qu.pk.ready') }} <small>{{ lang2 ? T2('qu.pk.ready') : T('qu.sc.ready_d') }}</small><span class="cnt num">{{ tea.ready.length }}</span></h2>
              <div v-if="hero" class="hero" :class="{ pulse: flash === hero.code }"><b class="num" data-testid="qs-hero">{{ hero.code }}</b><em>🔔 {{ T('qu.sc.say_ready', { no: hero.code }) }}</em></div>
              <div class="nums num"><span v-for="n in readyRest" :key="n.order_id">{{ n.code }}</span></div>
              <div v-if="readyMore" class="more">{{ T('qu.sc.more', { n: readyMore }) }}</div>
            </div>
          </div>
          <div class="tvfoot">
            <div v-if="cur" class="promo"><div class="mframe">
              <video v-if="cur.kind === 'video'" :key="cur.id" :src="cur.url" :poster="cur.poster_url || undefined" muted playsinline :loop="media.length === 1"
                @loadedmetadata="videoReady" @timeupdate="videoTime" @ended="nextMedia"></video>
              <img v-else :key="cur.id" :src="cur.url" alt="" />
            </div></div>
            <div class="tip"><b>{{ T('qu.sc.tip') }}</b>{{ T('qu.sc.tip_d', { n: view.ready_stay_minutes }) }}</div>
          </div>
        </template>
        <template v-else>
          <div class="pready" data-testid="qs-ready">
            <h2>{{ T('qu.pk.ready') }} <small>{{ lang2 ? T2('qu.pk.ready') : T('qu.sc.ready_d') }}</small></h2>
            <div v-if="hero" class="hero" :class="{ pulse: flash === hero.code }"><b class="num" data-testid="qs-hero">{{ hero.code }}</b><em>🔔 {{ T('qu.sc.say_ready', { no: hero.code }) }}</em></div>
            <div v-else class="pnone">{{ T('qu.sc.none') }}</div>
            <div class="g num"><span v-for="n in readyRest" :key="n.order_id">{{ n.code }}</span></div>
          </div>
          <div class="p2">
            <div class="tcol cook" data-testid="qs-cook">
              <h2>{{ T('qu.pk.cook') }}<span class="cnt num">{{ tea.cook.length }}</span></h2>
              <div class="nums num"><span v-for="n in tea.cook.slice(0, caps.cook)" :key="n.order_id">{{ n.code }}</span></div>
              <div v-if="tea.cook.length > caps.cook" class="more">{{ T('qu.sc.more', { n: tea.cook.length - caps.cook }) }}</div>
            </div>
            <div class="tcol wait" data-testid="qs-wait">
              <h2>{{ T('qu.pk.wait') }}<span class="cnt num">{{ tea.wait.length }}</span></h2>
              <div class="nums num"><span v-for="n in tea.wait.slice(0, caps.wait)" :key="n.order_id">{{ n.code }}</span></div>
              <div v-if="tea.wait.length > caps.wait" class="more">{{ T('qu.sc.more', { n: tea.wait.length - caps.wait }) }}</div>
            </div>
          </div>
          <div v-if="cur" class="pfoot promo"><div class="mframe">
            <video v-if="cur.kind === 'video'" :key="cur.id" :src="cur.url" :poster="cur.poster_url || undefined" muted playsinline :loop="media.length === 1"
              @loadedmetadata="videoReady" @timeupdate="videoTime" @ended="nextMedia"></video>
            <img v-else :key="cur.id" :src="cur.url" alt="" />
          </div></div>
        </template>
      </template>

      <!-- restaurant -->
      <template v-else>
        <div v-if="!q || !q.open" class="qs-off">{{ T('qu.sc.off') }}</div>
        <template v-else>
          <div class="rest">
            <div class="calling" :class="{ pulse: calling && flash === calling.number }" data-testid="qs-calling">
              <div class="lab"><i></i>{{ T('qu.sc.calling') }}<small v-if="lang2"> {{ T2('qu.sc.calling') }}</small></div>
              <template v-if="calling">
                <div class="bigno num" data-testid="qs-callno">{{ calling.number }}</div>
                <div class="bigto">{{ placeText(calling.table, T) }}<small>{{ T('qu.people', { n: calling.people }) }} · {{ T('qu.sc.nth', { n: calling.calls }) }}</small></div>
              </template>
              <div v-else class="bigno idle">—</div>
              <div v-if="recent.length" class="recent">
                <h4>{{ T('qu.sc.recent') }}</h4>
                <div><span v-for="c in recent" :key="c.number + c.calls"><b class="num">{{ c.number }}</b>{{ placeText(c.table, T) }}</span></div>
              </div>
            </div>
            <div class="rright">
              <div class="gtable">
                <table>
                  <tr><th>{{ T('qu.sc.group') }}</th><th>{{ T('qu.sc.current') }}</th><th>{{ T('qu.sc.waiting') }}</th><th>{{ T('qu.sc.est') }}</th></tr>
                  <tr v-for="g in q.groups" :key="g.id" :data-testid="'qs-group-' + g.letter">
                    <td class="g"><b>{{ g.letter }}</b>{{ groupName(g, T, lang) }}<small>{{ groupRange(g, T) }}</small></td>
                    <td class="c num">{{ g.current || '—' }}</td>
                    <td class="w"><b class="num">{{ g.waiting }}</b></td>
                    <td class="t">{{ g.waiting ? estText(g.est_minutes, T) : '—' }}</td>
                  </tr>
                </table>
              </div>
              <div class="waitgrid">
                <div class="wlist">
                  <h3>{{ T('qu.sc.waitlist') }}<small v-if="lang2"> {{ T2('qu.sc.waitlist') }}</small></h3>
                  <div v-for="g in q.groups" :key="g.id" class="wrow"><div class="l">{{ g.letter }}</div>
                    <span v-for="n in g.next.slice(0, 5)" :key="n" class="num">{{ n }}</span><em v-if="!g.next.length">{{ T('qu.sc.none') }}</em></div>
                </div>
                <div class="missed" data-testid="qs-missed">
                  <h3>{{ T('qu.skipped') }}</h3>
                  <p>{{ T('qu.sc.missed_d', { n: q.settings.requeue_after }) }}</p>
                  <div><span v-for="n in q.skipped.slice(0, 9)" :key="n" class="num">{{ n }}</span></div>
                </div>
              </div>
            </div>
          </div>
          <div class="tvfoot rfoot">
            <div class="qrbox" data-testid="qs-qr">
              <template v-if="!view.paused"><img v-if="qr" class="qr" :src="qr" alt="" /><div><b>{{ T('qu.sc.scan') }}</b><p>{{ T('qu.sc.scan_d') }}</p></div></template>
              <div v-else><b>{{ T('qu.sc.paused') }}</b></div>
            </div>
            <div v-if="cur" class="promo"><div class="mframe">
              <video v-if="cur.kind === 'video'" :key="cur.id" :src="cur.url" :poster="cur.poster_url || undefined" muted playsinline :loop="media.length === 1"
                @loadedmetadata="videoReady" @timeupdate="videoTime" @ended="nextMedia"></video>
              <img v-else :key="cur.id" :src="cur.url" alt="" />
            </div></div>
          </div>
        </template>
      </template>
    </div>
  </div>
</template>

<style scoped>
.qs-root { position: fixed; inset: 0; overflow: hidden; background: #2a140d; cursor: default;
  --red: #a02c20; --red-deep: #8f251b; --gold: #f4cf73; --gold-soft: #fbe7b3; --paper: #fffaf2; --bg: #f8efe1; --ink: #3b1f14; --muted: #7b6657; --line: #e6d8c4;
  --motif: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='56' height='56'%3E%3Cg fill='none' stroke='%23f4cf73' stroke-opacity='.17'%3E%3Ccircle cx='0' cy='0' r='28'/%3E%3Ccircle cx='56' cy='0' r='28'/%3E%3Ccircle cx='0' cy='56' r='28'/%3E%3Ccircle cx='56' cy='56' r='28'/%3E%3Ccircle cx='28' cy='28' r='28'/%3E%3C/g%3E%3Cg fill='%23f4cf73' fill-opacity='.3'%3E%3Cpath d='M28 25l3 3-3 3-3-3zM0 -3l3 3-3 3-3-3zM56 -3l3 3-3 3-3-3zM0 53l3 3-3 3-3-3zM56 53l3 3-3 3-3-3z'/%3E%3C/g%3E%3C/svg%3E");
  --red-bg: linear-gradient(90deg, rgba(143,37,27,.8) 0, rgba(143,37,27,.25) 45%, rgba(143,37,27,0) 75%), var(--motif) 0 0 / 56px 56px, var(--red); }
.num { font-variant-numeric: tabular-nums; letter-spacing: 1px; }
.qs-pair { position: absolute; inset: 0; display: grid; place-items: center; padding: 24px; background: var(--red-bg); }
.qs-pcard { background: var(--paper); border-radius: 20px; padding: 32px; width: min(520px, 100%); text-align: center; box-shadow: 0 20px 50px rgba(0,0,0,.3); }
.qs-pcard h1 { margin: 0 0 8px; color: var(--red); }
.qs-pcard p { color: var(--muted); line-height: 1.6; }
.qs-pcard form { display: flex; gap: 10px; margin-top: 16px; }
.qs-pcard input { flex: 1; font-size: 32px; letter-spacing: 8px; text-align: center; font-weight: 800; padding: 10px; min-width: 0; }
.qs-pcard .btn { font-size: 20px; padding: 0 24px; }
.qs-err { color: #b42318 !important; font-weight: 700; }

.qs-stage { position: absolute; left: 50%; top: 50%; width: 1920px; height: 1080px; background: var(--bg); display: flex; flex-direction: column; overflow: hidden; color: var(--ink);
  font-family: -apple-system, "PingFang SC", "Noto Sans CJK SC", "Microsoft YaHei", sans-serif; }
.portrait .qs-stage { width: 1080px; height: 1920px; }
.qs-off { flex: 1; display: grid; place-items: center; font-size: 40px; color: var(--muted); padding: 60px; text-align: center; }
.tvtop { height: 104px; background: var(--red-bg); color: #fff; display: flex; align-items: center; padding: 0 44px; gap: 20px; flex: none; }
.tvtop .logo { width: 64px; height: 64px; border-radius: 16px; background: var(--gold); color: var(--red-deep); display: grid; place-items: center; font-size: 34px; font-weight: 900; object-fit: cover; flex: none; }
.tvtop .ttl { min-width: 0; }
.tvtop h1 { margin: 0; font-size: 40px; letter-spacing: 2px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.tvtop .sub { font-size: 24px; color: var(--gold-soft); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.tvtop .clock { margin-inline-start: auto; font-size: 40px; font-weight: 700; flex: none; }
.tvtop .snd { font-size: 18px; background: rgba(255,255,255,.15); border-radius: 999px; padding: 6px 14px; white-space: nowrap; flex: none; }
.tvtop .snd:first-of-type { margin-inline-start: auto; }
.tvtop .snd + .clock { margin-inline-start: 0; }
.tvtop .snd.off { background: var(--gold); color: var(--red-deep); font-weight: 700; }
.tvtop .snd.bad { background: #fff; color: #b42318; font-weight: 700; }
.portrait .tvtop { height: 120px; }

.tea { flex: 1; display: grid; grid-template-columns: 1fr 1fr 1.35fr; gap: 24px; padding: 28px 36px 20px; min-height: 0; }
.tcol { border-radius: 24px; background: var(--paper); border: 2px solid var(--line); display: flex; flex-direction: column; overflow: hidden; min-height: 0; }
.tcol h2 { margin: 0; padding: 20px 28px; font-size: 40px; display: flex; align-items: baseline; gap: 14px; white-space: nowrap; }
.tcol h2 small { font-size: 22px; color: var(--muted); font-weight: 500; overflow: hidden; text-overflow: ellipsis; min-width: 0; }
.tcol h2 .cnt { margin-inline-start: auto; font-size: 30px; border-radius: 999px; padding: 2px 18px; }
.tcol.wait h2 { background: #efe7da; color: #5b4636; } .tcol.wait .cnt { background: #fff; }
.tcol.cook h2 { background: var(--gold); color: #6b3a05; } .tcol.cook .cnt { background: #fff6d8; }
.tcol.ready { background: var(--red); border-color: var(--red); color: #fff; }
.tcol.ready h2 { background: var(--red-bg); color: #fff; } .tcol.ready h2 small { color: var(--gold-soft); } .tcol.ready .cnt { background: var(--gold); color: var(--red-deep); }
.nums { display: flex; flex-wrap: wrap; gap: 16px; padding: 24px 26px; align-content: flex-start; }
.nums span { font-size: 58px; font-weight: 800; width: calc(33.33% - 11px); text-align: center; padding: 10px 0; border-radius: 16px; background: #fff; border: 2px solid var(--line); }
.wait .nums span { color: #8a7563; font-size: 52px; }
.cook .nums span { border-color: #f1d58e; color: #7a4a06; background: #fffbea; }
.ready .nums span { background: rgba(255,255,255,.1); border-color: rgba(244,207,115,.45); color: #fff; width: calc(50% - 8px); font-size: 72px; }
.more { margin: auto 26px 20px; color: var(--muted); font-size: 26px; }
.ready .more { color: var(--gold-soft); }
.hero { margin: 26px 26px 6px; border-radius: 22px; background: var(--gold); color: var(--red-deep); text-align: center; padding: 18px 0 14px; box-shadow: 0 0 0 8px rgba(244,207,115,.3), 0 18px 40px rgba(0,0,0,.25); }
.hero b { display: block; font-size: 150px; line-height: 1; font-weight: 900; }
.hero em { font-style: normal; font-size: 30px; font-weight: 700; }
.pulse { animation: qs-pulse 1s ease-in-out 5; }
@keyframes qs-pulse { 50% { transform: scale(1.06); box-shadow: 0 0 0 22px rgba(244,207,115,.45), 0 18px 40px rgba(0,0,0,.25); } }
.tvfoot { height: 170px; flex: none; margin: 0 36px 26px; display: flex; gap: 20px; }
.promo { flex: 1; border-radius: 22px; overflow: hidden; background: #2a140d; display: flex; justify-content: center; }
.mframe { position: relative; width: 100%; height: 100%; overflow: hidden; }
.mframe img, .mframe video { position: absolute; inset: 0; width: 100%; height: 100%; object-fit: cover; }
.tip { width: 620px; border-radius: 22px; background: var(--paper); border: 2px solid var(--line); padding: 20px 28px; font-size: 24px; color: var(--muted); display: flex; flex-direction: column; justify-content: center; gap: 8px; }
.tip b { color: var(--ink); font-size: 28px; }
.tvfoot:not(:has(.promo)) .tip { flex: 1; width: auto; }

/* portrait tea */
.pready { margin: 30px 36px 0; border-radius: 28px; background: var(--red); background-image: var(--motif); background-size: 56px 56px; color: #fff; padding: 28px 36px 36px; flex: none; }
.pready h2 { margin: 0 0 20px; font-size: 54px; display: flex; align-items: baseline; gap: 16px; } .pready h2 small { font-size: 28px; color: var(--gold-soft); font-weight: 500; }
.pready .hero { margin: 0 0 24px; } .pready .hero b { font-size: 200px; }
.pready .pnone { font-size: 40px; color: var(--gold-soft); padding: 40px 0; text-align: center; }
.pready .g { display: grid; grid-template-columns: repeat(3, 1fr); gap: 18px; }
.pready .g span { font-size: 86px; font-weight: 800; text-align: center; border-radius: 18px; border: 2px solid rgba(244,207,115,.45); background: rgba(255,255,255,.1); padding: 8px 0; }
.p2 { display: grid; grid-template-columns: 1fr 1fr; gap: 24px; margin: 28px 36px 0; flex: 1; min-height: 0; }
.p2 .tcol h2 { font-size: 46px; }
.p2 .nums span { width: calc(50% - 8px); font-size: 64px; }
.pfoot { height: 340px; margin: 28px 36px 36px; flex: none; }

/* restaurant */
.rest { flex: 1; display: grid; grid-template-columns: 760px minmax(0, 1fr); gap: 24px; padding: 26px 36px 0; min-height: 0; }
.calling { border-radius: 26px; background: var(--red); color: #fff; padding: 26px 34px; display: flex; flex-direction: column; background-image: var(--motif); background-size: 56px 56px; min-height: 0; }
.calling.pulse { animation: qs-glow 1s ease-in-out 5; }
@keyframes qs-glow { 50% { box-shadow: 0 0 0 14px rgba(244,207,115,.55); } }
.calling .lab { font-size: 32px; color: var(--gold-soft); display: flex; align-items: center; gap: 12px; }
.calling .lab small { font-size: 22px; margin-inline-start: 4px; }
.calling .lab i { width: 18px; height: 18px; border-radius: 50%; background: var(--gold); box-shadow: 0 0 0 8px rgba(244,207,115,.3); }
.bigno { font-size: 220px; font-weight: 900; line-height: 1; margin: 18px 0 4px; color: var(--gold); letter-spacing: 6px; }
.bigno.idle { opacity: .4; }
.bigto { font-size: 58px; font-weight: 800; } .bigto small { display: block; font-size: 34px; font-weight: 500; color: var(--gold-soft); margin-top: 6px; }
.recent { margin-top: auto; border-top: 2px solid rgba(244,207,115,.35); padding-top: 18px; }
.recent h4 { margin: 0 0 10px; font-size: 24px; color: var(--gold-soft); font-weight: 500; }
.recent div { display: flex; gap: 14px; }
.recent span { flex: 1; min-width: 0; background: rgba(255,255,255,.1); border-radius: 14px; padding: 10px 14px; font-size: 22px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.recent span b { display: block; font-size: 40px; }
.rright { min-width: 0; display: grid; grid-template-rows: auto minmax(0, 1fr); gap: 20px; min-height: 0; }
.gtable { background: var(--paper); border: 2px solid var(--line); border-radius: 22px; overflow: hidden; }
.gtable table { width: 100%; border-collapse: collapse; font-size: 30px; }
.gtable th { background: #f4ebde; color: var(--muted); font-size: 22px; font-weight: 600; text-align: start; padding: 12px 24px; }
.gtable td { padding: 12px 24px; border-top: 2px solid #f1e6d4; white-space: nowrap; }
.gtable td.g b { display: inline-grid; place-items: center; width: 52px; height: 52px; border-radius: 14px; background: var(--red); color: #fff; font-size: 30px; margin-inline-end: 14px; vertical-align: middle; }
.gtable td.g small { color: var(--muted); font-size: 22px; margin-inline-start: 8px; }
.gtable td.c { font-weight: 900; color: var(--red); font-size: 38px; }
.gtable td.w b { font-size: 38px; } .gtable td.t { color: #7a4a06; font-weight: 700; }
.waitgrid { display: grid; grid-template-columns: minmax(0, 1fr) 330px; gap: 20px; min-height: 0; }
.wlist { background: var(--paper); border: 2px solid var(--line); border-radius: 22px; padding: 16px 24px; overflow: hidden; }
.wlist h3, .missed h3 { margin: 0 0 10px; font-size: 28px; }
.wlist h3 small { font-size: 20px; color: var(--muted); font-weight: 500; margin-inline-start: 10px; }
.wrow { display: flex; align-items: center; gap: 12px; padding: 8px 0; border-top: 2px dashed #efe2cf; }
.wrow:first-of-type { border-top: 0; }
.wrow .l { width: 46px; height: 46px; border-radius: 12px; background: #f4ebde; color: var(--red); font-weight: 900; font-size: 26px; display: grid; place-items: center; flex: none; }
.wrow span { font-size: 30px; font-weight: 700; color: #5b4636; width: 118px; }
.wrow em { font-style: normal; color: var(--muted); font-size: 22px; }
.missed { background: #fff3e6; border: 2px dashed #e8b27a; border-radius: 22px; padding: 16px 22px; overflow: hidden; }
.missed p { margin: 0 0 12px; color: #a2510a; font-size: 22px; }
.missed div { display: flex; flex-wrap: wrap; gap: 12px; }
.missed span { font-size: 34px; font-weight: 800; color: #a2510a; background: #fff; border-radius: 12px; padding: 4px 14px; text-decoration: line-through; text-decoration-thickness: 2px; text-decoration-color: rgba(162,81,10,.4); }
.rfoot { margin-top: 22px; }
.qrbox { width: 620px; border-radius: 22px; background: var(--paper); border: 2px solid var(--line); display: flex; align-items: center; gap: 22px; padding: 0 24px; flex: none; }
.qrbox .qr { width: 130px; height: 130px; flex: none; border-radius: 6px; }
.qrbox b { font-size: 34px; display: block; } .qrbox p { margin: 6px 0 0; color: var(--muted); font-size: 22px; }
.rfoot:not(:has(.promo)) .qrbox { flex: 1; width: auto; }
</style>
