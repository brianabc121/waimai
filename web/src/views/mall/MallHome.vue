<script setup lang="ts">
// The marketplace: the stores that deliver to where the customer is, then
// the stores within 50 km that do not, marked "no delivery". The top left
// says where that is (found by the device, changed with a tap), the top right
// leads to the messages. A store's delivery area is never shown: a store
// delivers there, with its fee, minimum and time, or it does not.
import LangButton from '../LangButton.vue'
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { customer, loginRoute } from '../../customer'
import { locale, setLocale, t, sn } from '../../i18n'
import { bannerSub, bannerTitle, catName, mall, mallHome, mallName, mallStores, regionName, shortPlace, start, type MallBanner, type MallHome, type MallList, type MallStore } from '../../mall'
import CustomerTabs from '../me/CustomerTabs.vue'
import { displayName, loadProfile, me } from '../me/me'
import MallLocation from './MallLocation.vue'
import StoreCard from './StoreCard.vue'

const router = useRouter()
const home = ref<MallHome | null>(null)
const list = ref<MallList | null>(null)
const loading = ref(false), failed = ref(false)
const ask = reactive({ category: '', q: '', sort: '', filters: [] as string[] })
const typed = ref('')
const showLoc = ref<false | 'list' | 'cities'>(false)
const showFilter = ref(false)

const loc = computed(() => mall.loc)
const byRegion = computed(() => loc.value?.source === 'region')
const place = computed(() => {
  const l = loc.value
  if (!l) return mall.locating ? t('ml.locating') : t('ml.choose_loc')
  return l.name || regionName(list.value?.region) || t('ml.here')
})
const placeShort = computed(() => shortPlace(place.value))
const categories = computed(() => home.value?.categories ?? [])
const banners = computed(() => home.value?.banners ?? [])
const stores = computed(() => list.value?.stores ?? [])
const delivering = computed(() => stores.value.filter(s => !s.no_delivery))
const notDelivering = computed(() => stores.value.filter(s => s.no_delivery))
const narrowed = computed(() => !!(ask.category || ask.q || ask.filters.length))
const frequent = computed(() => {
  return (list.value?.frequent ?? []).map(slug => stores.value.find(s => s.slug === slug)).filter((s): s is MallStore => !!s)
})
const sorts = computed(() => byRegion.value ? ['', 'rating'] : ['', 'distance', 'rating', 'fee'])
const filters = computed(() => byRegion.value ? ['open', 'promo', 'schedule', 'pickup'] : ['open', 'promo', 'schedule'])
const sortName = (s: string) => t('ml.sort_' + (s || 'default'))

let seq = 0
async function load() {
  const l = loc.value
  if (!l) { list.value = null; return }
  const n = ++seq
  loading.value = true; failed.value = false
  try {
    const r = await mallStores(l, ask)
    if (n !== seq) return
    list.value = r
  } catch { if (n === seq) failed.value = true }
  if (n === seq) loading.value = false
}
// The list follows the place (not its name) and what is asked of it.
watch(() => [loc.value?.source, loc.value?.lat, loc.value?.lng, loc.value?.region?.city, loc.value?.region?.district, ask.category, ask.q, ask.sort, ask.filters.join()].join('|'), load)
watch(byRegion, on => { if (on && (ask.sort === 'distance' || ask.sort === 'fee')) ask.sort = '' })
// Nobody could say where the customer is: ask them.
watch(() => mall.denied, d => { if (d && !loc.value) showLoc.value = 'list' })

function search() { ask.q = typed.value.trim(); window.scrollTo({ top: 0 }) }
function clearSearch() { typed.value = ''; ask.q = '' }
function clearAll() { clearSearch(); ask.category = ''; ask.filters = [] }
function toggle(f: string) { ask.filters = ask.filters.includes(f) ? ask.filters.filter(x => x !== f) : [...ask.filters, f] }
function messages() { void router.push(customer.verified ? '/me/messages' : loginRoute('/me/messages')) }

// ---- banners ----
// They play by themselves every five seconds and can be swiped left and right
// (or dragged with a mouse). While a finger or the pointer is on them they hold
// still, and after a swipe the next turn waits a full five seconds.
const slide = ref(0)
const drag = ref(0)
const dragging = ref(false)
let timer = 0
let startX = 0, startY = 0, moved = false, held = false, hover = false, swallow = false, lastTouch = 0
const rtl = () => document.documentElement.dir === 'rtl'
function goTo(i: number) { const n = banners.value.length; if (n) slide.value = (i + n) % n }
function tick() {
  if (banners.value.length < 2 || held || hover || Date.now() - lastTouch < 4500) return
  goTo(slide.value + 1)
}
function bDown(e: PointerEvent) {
  if (banners.value.length < 2 || (e.pointerType === 'mouse' && e.button !== 0)) return
  startX = e.clientX; startY = e.clientY; moved = false; held = true; dragging.value = true; drag.value = 0
}
function bMove(e: PointerEvent) {
  if (!dragging.value) return
  const dx = e.clientX - startX, dy = e.clientY - startY
  if (!moved) {
    if (Math.abs(dx) < 6 && Math.abs(dy) < 6) return
    // a mostly-up-or-down move is the page scrolling, not a swipe
    if (Math.abs(dy) > Math.abs(dx)) { dragging.value = false; held = false; return }
    moved = true
    try { (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId) } catch { /* the pointer is already gone */ }
  }
  drag.value = dx
}
function bUp(e: PointerEvent) {
  held = false; lastTouch = Date.now()
  if (!dragging.value) return
  dragging.value = false
  if (moved) {
    swallow = true; window.setTimeout(() => { swallow = false }, 0)
    const w = (e.currentTarget as HTMLElement).clientWidth || 1
    if (Math.abs(drag.value) > Math.min(60, w * 0.18)) goTo(slide.value + ((drag.value < 0) !== rtl() ? 1 : -1))
  }
  drag.value = 0
}
// only a mouse resting on them holds them; a tap on a phone leaves no hover behind
function setHover(e: PointerEvent, on: boolean) { if (e.pointerType !== 'mouse') return; hover = on; if (!on) lastTouch = Date.now() }
function pickDot(i: number) { goTo(i); lastTouch = Date.now() }
function bClick(e: MouseEvent) { if (swallow) { e.stopPropagation(); e.preventDefault() } }
const trackStyle = computed(() => ({
  transform: `translateX(calc(${(rtl() ? 1 : -1) * slide.value * 100}% + ${drag.value}px))`,
  transition: dragging.value ? 'none' : undefined,
}))
function follow(b: MallBanner) {
  if (b.link_slug) void router.push(`/s/${b.link_slug}`)
  else if (b.link_url.startsWith('/')) void router.push(b.link_url)
  else if (b.link_url) window.location.href = b.link_url
}

// The store a banner leads to, named when it is among the stores listed here.
const bannerStore = (b: MallBanner) => {
  const st = b.link_slug ? stores.value.find(x => x.slug === b.link_slug) : undefined
  return st ? sn(st) : ''
}

function loadHome() { void mallHome().then(h => { home.value = h; document.title = mallName(h) }).catch(() => null) }
// categories and banners come in the customer's language
watch(locale, loadHome)

onMounted(async () => {
  document.title = t('ml.brand')
  loadHome()
  void loadProfile()
  timer = window.setInterval(tick, 5000)
  if (loc.value) void load()
  await start()
  if (mall.denied && !loc.value) showLoc.value = 'list'
})
onBeforeUnmount(() => clearInterval(timer))
</script>

<template>
  <div class="mall warm" :class="{ searching: !!ask.q }" data-testid="mall">
    <header class="mtop">
      <div class="mtop-in">
        <a class="brand" href="/" @click.prevent="clearAll">{{ mallName(home) }}</a>
        <button class="loc" data-testid="mall-loc" @click="showLoc = 'list'">
          <svg class="pin" viewBox="0 0 24 24" width="30" height="30" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linejoin="round"><path d="M12 21.500s7-6.2 7-11.500a7 7 0 1 0-14 0c0 5.3 7 11.5 7 11.500z" /><circle cx="12" cy="10" r="2.6" /></svg>
          <span class="where" :title="place"><small>{{ t('ml.deliver_to') }}</small><b>{{ placeShort }}</b></span>
          <svg class="chev" viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round"><path d="m6 9 6 6 6-6" /></svg>
        </button>
        <form class="msearch" role="search" @submit.prevent="search">
          <svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="10.5" cy="10.5" r="6.5" /><path d="m15.5 15.5 5 5" /></svg>
          <input v-model="typed" type="search" enterkeyhint="search" :placeholder="t('ml.search_ph')" :aria-label="t('ml.search_ph')" data-testid="mall-search" />
          <button v-if="ask.q" type="button" class="clear" :aria-label="t('close')" data-testid="mall-search-clear" @click="clearSearch">✕</button>
          <button v-else type="submit" class="go">{{ t('ml.search') }}</button>
        </form>
        <nav class="mlinks">
          <router-link to="/me/favorites" class="txt wide">{{ t('ml.favorites') }}</router-link>
          <router-link to="/orders" class="txt">{{ t('ml.orders') }}</router-link>
          <LangButton class="mlang" />
          <button class="msg" :aria-label="t('ml.messages')" data-testid="mall-messages" @click="messages">
            <svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="M12 4.500c4.7 0 8.5 3.1 8.5 7s-3.8 7-8.5 7c-1 0-2-.150-2.9-.400L5 20l.9-3.300C4.4 15.4 3.5 13.6 3.5 11.500c0-3.9 3.8-7 8.5-7z" /><path d="M8.5 11.500h.01M12 11.500h.01M15.5 11.500h.01" /></svg>
            <i v-if="me.profile && me.profile.unread + me.profile.chat_unread" data-testid="mall-unread">{{ me.profile.unread + me.profile.chat_unread > 99 ? '99+' : me.profile.unread + me.profile.chat_unread }}</i>
          </button>
          <router-link to="/me" class="avatar" :aria-label="t('me.me')">
            <template v-if="me.profile">{{ displayName(me.profile).slice(0, 1) }}</template>
            <svg v-else viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><circle cx="12" cy="8.5" r="3.8" /><path d="M4.5 20c1.2-3.6 4-5.4 7.5-5.400s6.3 1.8 7.5 5.4" /></svg>
          </router-link>
        </nav>
      </div>
    </header>

    <div class="mbody">
      <aside class="mside">
        <button :class="{ on: !ask.category }" @click="ask.category = ''"><span>🍽️</span>{{ t('ml.all') }}</button>
        <button v-for="c in categories" :key="c.id" :class="{ on: ask.category === c.code }" @click="ask.category = c.code"><span>{{ c.icon }}</span>{{ catName(c) }}</button>
        <hr />
        <label v-for="f in filters" :key="f" class="check"><input type="checkbox" :checked="ask.filters.includes(f)" @change="toggle(f)" />{{ t('ml.f_' + f) }}</label>
      </aside>

      <main class="mmain">
        <template v-if="!ask.q">
          <div v-if="banners.length" class="banners" :class="{ many: banners.length > 1 }" data-testid="mall-banners"
            @pointerdown="bDown" @pointermove="bMove" @pointerup="bUp" @pointercancel="bUp" @click.capture="bClick"
            @pointerenter="setHover($event, true)" @pointerleave="setHover($event, false)">
            <div class="track" :style="trackStyle">
            <a v-for="(b, i) in banners" :key="b.id" class="banner" :class="{ link: b.link_slug || b.link_url }" :aria-label="t('ml.banner')" :aria-hidden="i !== slide || undefined" draggable="false" @click="follow(b)">
              <img v-if="b.image_url" :src="b.image_url" alt="" draggable="false" />
              <div>
                <b>{{ bannerTitle(b) }}</b>
                <span v-if="bannerSub(b)" class="ribbon">{{ bannerSub(b) }}</span>
                <em v-if="bannerStore(b)">{{ bannerStore(b) }}</em>
              </div>
            </a>
            </div>
            <div v-if="banners.length > 1" class="dots"><i v-for="(b, i) in banners" :key="b.id" :class="{ on: i === slide }" @click="pickDot(i)"></i></div>
          </div>

          <div v-if="categories.length" class="cats" data-testid="mall-categories">
            <button :class="{ on: !ask.category }" @click="ask.category = ''"><span>🍽️</span>{{ t('ml.all') }}</button>
            <button v-for="c in categories" :key="c.id" :class="{ on: ask.category === c.code }" :data-code="c.code" @click="ask.category = c.code"><span>{{ c.icon }}</span>{{ catName(c) }}</button>
          </div>

          <section v-if="frequent.length && !narrowed" class="freq" data-testid="mall-frequent">
            <h2>{{ t('ml.frequent') }}</h2>
            <div>
              <router-link v-for="s in frequent" :key="s.slug" :to="`/s/${s.slug}`">
                <img v-if="s.logo_url || s.cover_url" :src="s.logo_url || s.cover_url" alt="" />
                <span v-else class="letter">{{ sn(s).slice(0, 1) }}</span>
                <span>{{ sn(s) }}</span>
              </router-link>
            </div>
            <router-link v-if="frequent.length === 1" class="go" :to="`/s/${frequent[0].slug}`" :aria-label="sn(frequent[0])" tabindex="-1">
              <svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round"><path d="m9 5 7 7-7 7" /></svg>
            </router-link>
          </section>
        </template>

        <!-- nothing known about where the customer is -->
        <div v-if="!loc" class="state" data-testid="mall-nowhere">
          <div class="em">🛵</div>
          <template v-if="mall.locating"><h2>{{ t('ml.first_title') }}</h2><p>{{ t('ml.first_hint') }}</p></template>
          <template v-else>
            <h2>{{ t('ml.sheet_title') }}</h2><p>{{ t('ml.denied') }}</p>
            <div class="acts"><button class="btn primary" @click="showLoc = 'list'">{{ t('ml.choose_loc') }}</button><button class="btn" @click="showLoc = 'cities'">{{ t('ml.browse') }}</button></div>
          </template>
        </div>

        <template v-else>
          <p v-if="byRegion" class="regnote" data-testid="mall-region-note">{{ t('ml.region_note') }}</p>
          <div class="listhead">
            <h2 v-if="ask.q" class="found" data-testid="mall-found">{{ stores.length ? t('ml.found', { n: stores.length, q: ask.q }) : t('ml.found_none', { q: ask.q }) }}</h2>
            <h2 v-else class="near">{{ byRegion ? t('ml.in_city', { city: loc.name }) : t('ml.nearby') }}
              <small v-if="list && stores.length" data-testid="mall-count">{{ byRegion ? t('ml.in_city_n', { n: stores.length }) : t('ml.nearby_n', { n: delivering.length }) }}</small></h2>
            <div class="sorts" data-testid="mall-sorts">
              <button v-for="s in sorts" :key="s" :class="{ on: ask.sort === s }" :data-sort="s" @click="ask.sort = s">{{ sortName(s) }}</button>
              <div class="filt">
                <button :class="{ on: ask.filters.length }" data-testid="mall-filter" @click="showFilter = !showFilter">{{ t('ml.filter') }}<template v-if="ask.filters.length"> {{ ask.filters.length }}</template>
                  <svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round"><path d="M4 8h9M19 8h1M4 16h1M11 16h9" /><circle cx="16" cy="8" r="2.4" /><circle cx="8" cy="16" r="2.4" /></svg></button>
                <div v-if="showFilter" class="filtmenu">
                  <label v-for="f in filters" :key="f" class="check"><input type="checkbox" :checked="ask.filters.includes(f)" :data-filter="f" @change="toggle(f)" />{{ t('ml.f_' + f) }}</label>
                </div>
              </div>
            </div>
          </div>

          <p v-if="failed" class="state small">{{ t('ml.load_failed') }} <button class="btn link" @click="load">{{ t('retry') }}</button></p>
          <div v-else-if="!list" class="grid"><div v-for="i in 3" :key="i" class="skel"></div></div>
          <template v-else>
            <template v-if="stores.length">
              <div v-if="delivering.length" class="grid" :class="{ dim: loading }" data-testid="mall-stores">
                <StoreCard v-for="s in delivering" :key="s.slug" :store="s" :categories="categories" />
              </div>
              <p v-else class="faronly" data-testid="mall-far-only">{{ t('ml.far_only') }}</p>
              <section v-if="notDelivering.length" class="far" data-testid="mall-far">
                <h3 v-if="delivering.length"><span>{{ t('ml.far_title') }}</span><small>{{ t('ml.far_hint') }}</small></h3>
                <div class="grid" :class="{ dim: loading }">
                  <StoreCard v-for="s in notDelivering" :key="s.slug" :store="s" :categories="categories" />
                </div>
              </section>
            </template>
            <div v-else-if="narrowed" class="state" data-testid="mall-no-match">
              <h2>{{ ask.q ? t('ml.found_none', { q: ask.q }) : t('ml.none_filtered') }}</h2>
              <div class="acts"><button class="btn" @click="clearAll">{{ t('ml.clear_filters') }}</button></div>
            </div>
            <div v-else class="state" data-testid="mall-none">
              <div class="em">🛵</div>
              <h2>{{ byRegion ? t('ml.cities_none') : t('ml.none_title') }}</h2>
              <p v-if="!byRegion">{{ t('ml.none_hint') }}</p>
              <div class="acts"><button class="btn primary" @click="showLoc = 'list'">{{ t('ml.change') }}</button><button class="btn" @click="showLoc = 'cities'">{{ t('ml.browse') }}</button></div>
            </div>

          </template>
        </template>

        <footer class="mfoot">
          <span>{{ t('ml.apply') }}<router-link to="/me/apply">{{ t('ml.apply_link') }} ›</router-link></span>
          <router-link to="/login" data-testid="mall-staff-login">{{ t('ml.staff_login') }}</router-link>
        </footer>
      </main>
    </div>

    <CustomerTabs active="home" class="mtabs" />
    <MallLocation v-if="showLoc" :region="list?.region" :start-at="showLoc === 'cities' ? 'cities' : undefined" @close="showLoc = false" />
  </div>
</template>

<style scoped>
.mall { min-height: 100vh; padding-bottom: calc(var(--tabbar-h) + env(safe-area-inset-bottom) + 16px); }
.mtop { position: relative; z-index: 20; background: var(--red-bg); color: #fff; }
.mtop-in { display: grid; grid-template-columns: minmax(0, 1fr) auto; grid-template-areas: 'loc links' 'search search'; gap: 6px 10px; align-items: center; padding: 6px 12px 10px; }
.brand { display: none; font-size: 22px; font-weight: 800; color: #fff3da; white-space: nowrap; letter-spacing: 1px; }
.loc { grid-area: loc; display: flex; align-items: center; gap: 4px; min-width: 0; height: 28px; border: 0; background: none; color: #fff; padding: 0; cursor: pointer; text-align: start; justify-self: start; max-width: 100%; }
.loc svg { flex: none; }
.loc .pin { width: 16px; height: 16px; }
.loc .chev { width: 13px; height: 13px; opacity: .85; }
/* on a phone: one short line, "配送至 · street" */
.loc .where { display: flex; align-items: baseline; gap: 5px; min-width: 0; line-height: 1.2; font-size: 11px; }
.loc small { flex: none; font-size: 11px; color: var(--gold); font-weight: 700; }
.loc b { font-size: 11px; font-weight: 500; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.mlinks { grid-area: links; display: flex; align-items: center; gap: 18px; }
.mlinks .txt, .mlinks .avatar { display: none; color: #fff; white-space: nowrap; }
.msg { position: relative; border: 0; background: rgba(255, 255, 255, .16); width: 30px; height: 30px; border-radius: 8px; color: #fff; cursor: pointer; display: grid; place-items: center; padding: 0; }
.msg i { position: absolute; inset-inline-end: -6px; top: -6px; min-width: 16px; height: 16px; padding: 0 3px; border-radius: 8px; background: var(--gold); color: var(--red); font-style: normal; font-size: 11px; font-weight: 800; display: grid; place-items: center; border: 1.5px solid var(--red); }
.avatar { width: 38px; height: 38px; border-radius: 50%; background: var(--gold); color: var(--red) !important; font-weight: 800; place-items: center; }
.msearch { grid-area: search; display: flex; align-items: center; gap: 8px; height: 40px; padding: 0 4px 0 12px; background: var(--paper); border-radius: 12px; color: var(--muted); }
.msearch input { flex: 1; min-width: 0; border: 0; padding: 0; background: none; color: var(--ink); outline: none; font-size: 16px; -webkit-appearance: none; appearance: none; }
.msearch input::placeholder { color: var(--muted); }
.msearch .go { border: 0; background: var(--red); color: #fff; font-weight: 800; border-radius: 9px; height: 32px; padding: 0 18px; cursor: pointer; white-space: nowrap; letter-spacing: 1px; }
.msearch .clear { border: 0; background: none; color: var(--muted); width: 36px; height: 36px; cursor: pointer; }

.mbody { padding: 0 12px; }
.mside { display: none; }
.mmain { min-width: 0; display: grid; gap: 14px; grid-template-columns: minmax(0, 1fr); align-content: start; padding-top: 12px; }

/* on a phone the banner continues the red top, from edge to edge */
.banners { position: relative; margin: -12px -12px 0; overflow: hidden; }
.banners.many { touch-action: pan-y; user-select: none; -webkit-user-select: none; }
.track { display: flex; transition: transform .38s cubic-bezier(.25, .8, .3, 1); will-change: transform; }
@media (prefers-reduced-motion: reduce) { .track { transition: none; } }
.banner { flex: 0 0 100%; min-width: 0; position: relative; display: block; height: 150px; overflow: hidden; border-radius: 0; padding: 0; font-size: inherit; color: #fff; background: var(--motif) 0 -28px / 56px 56px, linear-gradient(180deg, var(--red), #b23a26); }
.banner.link { cursor: pointer; }
.banner img { position: absolute; inset-inline-end: 0; top: 0; height: 100%; width: 68%; object-fit: cover; -webkit-mask-image: linear-gradient(90deg, transparent, #000 42%); mask-image: linear-gradient(90deg, transparent, #000 42%); }
.banner div { position: relative; padding: 18px 16px 0; max-width: 66%; display: grid; gap: 7px; justify-items: start; text-shadow: 0 1px 6px rgba(80, 20, 10, .35); }
.banner b { font-size: 24px; line-height: 1.15; font-weight: 900; color: #fff6e3; letter-spacing: 2px; font-family: "Songti SC", "Noto Serif CJK SC", "Source Han Serif SC", STSong, serif; }
.banner .ribbon { background: var(--gold); color: var(--red-deep); font-weight: 900; font-size: 15px; font-style: italic; padding: 2px 10px; transform: rotate(-3deg); transform-origin: left center; text-shadow: none; max-width: 100%; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  clip-path: polygon(0 8%, 3% 0, 97% 4%, 100% 14%, 98% 50%, 100% 88%, 96% 100%, 3% 96%, 0 86%, 2% 50%); }
.banner em { font-style: normal; font-size: 13px; opacity: .96; letter-spacing: 1px; }
.dots { position: absolute; inset-inline-start: 16px; bottom: 9px; display: flex; gap: 5px; }
.dots i { width: 6px; height: 6px; border-radius: 50%; background: rgba(255, 255, 255, .5); cursor: pointer; }
.dots i.on { background: #fff; }

.cats { display: grid; grid-template-columns: repeat(4, 1fr); gap: 2px 6px; padding: 0; }
.cats button { border: 0; background: none; border-radius: 9px; display: grid; justify-items: center; gap: 1px; font-size: 12px; font-weight: 500; color: var(--ink); cursor: pointer; padding: 5px 0; min-width: 0; white-space: nowrap; }
.cats span { font-size: 22px; line-height: 1.25; }
.cats .on { background: var(--gold-soft); color: var(--red); font-weight: 700; }

h2 { font-size: 17px; }
.freq { display: flex; align-items: center; gap: 14px; border-top: 1px solid var(--line); border-bottom: 1px solid var(--line); padding: 10px 2px; min-width: 0; }
.freq h2 { flex: none; padding-inline-end: 14px; border-inline-end: 1px solid #cbb9a2; }
.freq > div { flex: 1; min-width: 0; display: flex; gap: 18px; overflow-x: auto; scrollbar-width: none; }
.freq > div::-webkit-scrollbar { display: none; }
.freq a { flex: none; display: flex; align-items: center; gap: 10px; color: var(--ink); font-size: 16px; max-width: 220px; }
.freq img, .freq .letter { width: 36px; height: 36px; border-radius: 9px; object-fit: cover; flex: none; }
.freq .letter { background: #c2401c; color: #fff3da; display: grid; place-items: center; font-size: 17px; font-weight: 700; }
.freq a span:last-child { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.freq .go { flex: none; display: grid; max-width: none; }

.regnote { background: var(--gold-soft); color: var(--ink); border-radius: 10px; padding: 9px 12px; font-size: 13.500px; }
.listhead { display: grid; gap: 10px; min-width: 0; }
.near { font-size: 22px; font-weight: 800; display: flex; align-items: center; flex-wrap: wrap; gap: 4px 12px; }
.near small { font-weight: 500; font-size: 13.500px; color: var(--ink); background: var(--gold-soft); border-radius: 999px; padding: 3px 12px; }
.found { font-size: 14px; font-weight: 400; color: var(--muted); }
.sorts { display: flex; align-items: center; min-height: 34px; overflow-x: auto; scrollbar-width: none; }
.sorts::-webkit-scrollbar { display: none; }
.sorts button { flex: none; border: 0; background: none; height: 22px; padding: 0 14px; color: var(--ink); cursor: pointer; white-space: nowrap; border-inline-start: 1px solid #cbb9a2; display: inline-flex; align-items: center; gap: 6px; font-size: 15px; }
.sorts > button:first-child { border-inline-start: 0; }
.sorts > button.on { background: var(--red); color: #fff; font-weight: 700; height: 32px; border-radius: 9px; border-inline-start: 0; padding: 0 16px; margin-inline-end: 8px; }
.sorts > button.on + button { border-inline-start: 0; }
.sorts > button:not(.on) + button.on { margin-inline-start: 8px; }
.filt { flex: none; margin-inline-start: auto; }
.filt button.on { color: var(--red); font-weight: 700; }
.filtmenu { position: absolute; z-index: 15; inset-inline-end: 12px; margin-top: 6px; background: #fff; border-radius: 12px; box-shadow: 0 8px 28px rgba(60, 30, 10, .18); padding: 12px 16px 4px; min-width: 170px; }

.grid { display: grid; gap: 12px; grid-template-columns: minmax(0, 1fr); }
.far { display: grid; gap: 10px; min-width: 0; }
.far h3 { display: flex; align-items: center; flex-wrap: wrap; gap: 2px 10px; padding-top: 6px; font-size: 16px; font-weight: 800; color: var(--muted); }
.far h3::before { content: ""; flex: 0 0 100%; height: 1px; margin-bottom: 8px; background: repeating-linear-gradient(90deg, #cbb9a2 0 6px, transparent 6px 11px); }
.far h3 small { font-size: 12.500px; font-weight: 400; }
.faronly { background: var(--gold-soft); color: var(--ink); border-radius: 10px; padding: 9px 12px; font-size: 13.500px; }
.grid.dim { opacity: .6; }
.grid.rows { gap: 0; }
.skel { height: 120px; border-radius: 14px; background: var(--paper); border: 1px solid var(--line); }
.state { text-align: center; padding: 30px 12px; display: grid; gap: 8px; justify-items: center; }
.state .em { font-size: 54px; line-height: 1; }
.state p { color: var(--muted); }
.state .acts { display: flex; gap: 10px; margin-top: 8px; flex-wrap: wrap; justify-content: center; }
.state.small { padding: 16px; display: block; }
.mfoot { display: flex; flex-wrap: wrap; gap: 6px 18px; justify-content: center; align-items: center; color: var(--muted); font-size: 13.500px; padding: 14px 0 6px; }
.mlang { height: 30px; border: 1px solid rgba(244, 207, 115, .7); background: rgba(255, 255, 255, .16); border-radius: 15px; padding: 0 10px; color: #fff; cursor: pointer; font-size: 13px; }

@media (max-width: 430px) {
  .sorts button { padding: 0 9px; font-size: 14.5px; }
  .sorts > button.on { padding: 0 13px; margin-inline-end: 6px; }
}
@media (min-width: 768px) {
  .mall { padding-bottom: 32px; }
  .mtabs { display: none; }
  .mtop-in { max-width: 1320px; margin: 0 auto; grid-template-columns: auto minmax(0, max-content) minmax(180px, 1fr) auto; grid-template-areas: 'brand loc search links'; gap: 18px; padding: 12px 24px; }
  .brand { display: block; grid-area: brand; }
  .loc { background: rgba(255, 255, 255, .14); border-radius: 10px; height: 38px; padding: 0 10px; max-width: 300px; gap: 6px; }
  .loc .pin { width: 20px; height: 20px; }
  .loc .where { display: grid; gap: 0; }
  .loc b { font-size: 13.5px; font-weight: 700; } .loc small { font-size: 10.5px; }
  .msg { width: 36px; height: 36px; }
  .mlinks .txt { display: block; }
  .mlinks .txt.wide { display: none; }
  .mlinks .avatar { display: grid; }
  .mbody { max-width: 1320px; margin: 0 auto; padding: 20px 24px 0; display: grid; grid-template-columns: 196px minmax(0, 1fr); gap: 22px; align-items: start; }
  .mside { display: grid; gap: 2px; background: var(--paper); border: 1px solid var(--line); border-radius: 14px; padding: 10px; position: sticky; top: 16px; }
  .mside button { border: 0; background: none; border-radius: 10px; padding: 9px 12px; text-align: start; cursor: pointer; display: flex; gap: 10px; align-items: center; font-weight: 600; }
  .mside button span { font-size: 22px; line-height: 1; }
  .mside button:hover { background: var(--bg); }
  .mside button.on { background: var(--gold-soft); color: var(--red); font-weight: 800; }
  .mside hr { border: 0; border-top: 1px solid var(--line); margin: 8px 6px; width: auto; }
  .mside .check { padding: 7px 12px; margin: 0; }
  .mmain { gap: 18px; padding-top: 0; }
  .cats { display: none; }
  .banners { margin: 0; }
  .banner { height: 180px; }
  .banner img { width: 58%; }
  .banner div { padding: 30px 30px 0; max-width: 56%; gap: 10px; }
  .banner b { font-size: 32px; } .banner .ribbon { font-size: 19px; padding: 3px 14px; } .banner em { font-size: 15px; }
  .dots { inset-inline-start: 30px; bottom: 14px; }
  .listhead { grid-template-columns: minmax(0, 1fr) auto; align-items: center; }
  .sorts { overflow: visible; }
  .filt { position: relative; margin-inline-start: 0; }
  .filtmenu { inset-inline-end: 0; }
  .grid, .grid.rows { grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 18px; }
  .grid.rows { gap: 0 28px; }
  .skel { height: 260px; }
}
@media (min-width: 1200px) {
  .mlinks .txt.wide { display: block; }
  .grid, .grid.rows { grid-template-columns: repeat(3, minmax(0, 1fr)); }
}
</style>
