<script setup lang="ts">
// The marketplace: the stores that deliver to where the customer is. The top
// left says where that is (found by the device, changed with a tap), the top
// right leads to the messages. A store's delivery area is never shown: a
// store is either in the list, with its fee, minimum and time, or it is not.
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { customer, loginRoute } from '../../customer'
import { locale, pick, setLocale, t } from '../../i18n'
import { catName, mall, mallHome, mallName, mallStores, regionName, start, type MallBanner, type MallHome, type MallList, type MallStore } from '../../mall'
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
const categories = computed(() => home.value?.categories ?? [])
const banners = computed(() => home.value?.banners ?? [])
const stores = computed(() => list.value?.stores ?? [])
const pickup = computed(() => list.value?.pickup ?? [])
const narrowed = computed(() => !!(ask.category || ask.q || ask.filters.length))
const frequent = computed(() => {
  const all = [...stores.value, ...pickup.value]
  return (list.value?.frequent ?? []).map(slug => all.find(s => s.slug === slug)).filter((s): s is MallStore => !!s)
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
const slide = ref(0)
let timer = 0
function follow(b: MallBanner) {
  if (b.link_slug) void router.push(`/s/${b.link_slug}`)
  else if (b.link_url.startsWith('/')) void router.push(b.link_url)
  else if (b.link_url) window.location.href = b.link_url
}

onMounted(async () => {
  document.title = t('ml.brand')
  void mallHome().then(h => { home.value = h; document.title = mallName(h) }).catch(() => null)
  void loadProfile()
  timer = window.setInterval(() => { if (banners.value.length > 1) slide.value = (slide.value + 1) % banners.value.length }, 5000)
  if (loc.value) void load()
  await start()
  if (mall.denied && !loc.value) showLoc.value = 'list'
})
onBeforeUnmount(() => clearInterval(timer))
</script>

<template>
  <div class="mall" :class="{ searching: !!ask.q }" data-testid="mall">
    <header class="mtop">
      <div class="mtop-in">
        <a class="brand" href="/" @click.prevent="clearAll">{{ mallName(home) }}</a>
        <button class="loc" data-testid="mall-loc" @click="showLoc = 'list'">
          <svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="1.8"><circle cx="12" cy="12" r="8.5" /><circle cx="12" cy="12" r="4.5" /></svg>
          <b>{{ place }}</b><span>›</span>
        </button>
        <form class="msearch" role="search" @submit.prevent="search">
          <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="10.5" cy="10.5" r="6.5" /><path d="m15.5 15.5 5 5" /></svg>
          <input v-model="typed" type="search" enterkeyhint="search" :placeholder="t('ml.search_ph')" :aria-label="t('ml.search_ph')" data-testid="mall-search" />
          <button v-if="ask.q" type="button" class="clear" :aria-label="t('close')" data-testid="mall-search-clear" @click="clearSearch">✕</button>
          <button v-else type="submit" class="go">{{ t('ml.search') }}</button>
        </form>
        <nav class="mlinks">
          <router-link to="/me/favorites" class="txt wide">{{ t('ml.favorites') }}</router-link>
          <router-link to="/orders" class="txt">{{ t('ml.orders') }}</router-link>
          <button class="msg" :aria-label="t('ml.messages')" data-testid="mall-messages" @click="messages">
            <svg viewBox="0 0 24 24" width="26" height="26" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="M5.5 4.5h13a2 2 0 0 1 2 2v8.500a2 2 0 0 1-2 2H11l-4.500 3.500v-3.500h-1a2 2 0 0 1-2-2V6.500a2 2 0 0 1 2-2z" /><path d="M8.500 11h.01M12 11h.01M15.500 11h.01" /></svg>
            <i v-if="me.profile && me.profile.unread + me.profile.chat_unread" data-testid="mall-unread">{{ me.profile.unread + me.profile.chat_unread > 99 ? '99+' : me.profile.unread + me.profile.chat_unread }}</i>
          </button>
          <router-link to="/me" class="avatar" :aria-label="t('me.me')">
            <template v-if="me.profile">{{ displayName(me.profile).slice(0, 1) }}</template>
            <svg v-else viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><circle cx="12" cy="8.5" r="3.8" /><path d="M4.5 20c1.2-3.6 4-5.4 7.5-5.4s6.3 1.8 7.500 5.4" /></svg>
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
          <div v-if="banners.length" class="banners" data-testid="mall-banners">
            <a v-for="(b, i) in banners" v-show="i === slide" :key="b.id" class="banner" :class="{ link: b.link_slug || b.link_url }" :aria-label="t('ml.banner')" @click="follow(b)">
              <img v-if="b.image_url" :src="b.image_url" alt="" />
              <div><b>{{ pick(b.title, b.title_en) }}</b><span>{{ pick(b.subtitle, b.subtitle_en) }}</span></div>
            </a>
            <div v-if="banners.length > 1" class="dots"><i v-for="(b, i) in banners" :key="b.id" :class="{ on: i === slide }" @click="slide = i"></i></div>
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
                <span v-else class="letter">{{ pick(s.name, s.name_en).slice(0, 1) }}</span>
                <span>{{ pick(s.name, s.name_en) }}</span>
              </router-link>
            </div>
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
            <h2 v-else>{{ byRegion ? t('ml.in_city', { city: loc.name }) : t('ml.nearby') }}
              <small v-if="list && stores.length" data-testid="mall-count">{{ t(byRegion ? 'ml.in_city_n' : 'ml.nearby_n', { n: stores.length }) }}</small></h2>
            <div class="sorts" data-testid="mall-sorts">
              <button v-for="s in sorts" :key="s" :class="{ on: ask.sort === s }" :data-sort="s" @click="ask.sort = s">{{ sortName(s) }}</button>
              <span class="bar"></span>
              <div class="filt">
                <button :class="{ on: ask.filters.length }" data-testid="mall-filter" @click="showFilter = !showFilter">{{ t('ml.filter') }}<template v-if="ask.filters.length"> {{ ask.filters.length }}</template> ▾</button>
                <div v-if="showFilter" class="filtmenu">
                  <label v-for="f in filters" :key="f" class="check"><input type="checkbox" :checked="ask.filters.includes(f)" :data-filter="f" @change="toggle(f)" />{{ t('ml.f_' + f) }}</label>
                </div>
              </div>
            </div>
          </div>

          <p v-if="failed" class="state small">{{ t('ml.load_failed') }} <button class="btn link" @click="load">{{ t('retry') }}</button></p>
          <div v-else-if="!list" class="grid"><div v-for="i in 3" :key="i" class="skel"></div></div>
          <template v-else>
            <div v-if="stores.length" class="grid" :class="{ dim: loading }" data-testid="mall-stores">
              <StoreCard v-for="s in stores" :key="s.slug" :store="s" :categories="categories" />
            </div>
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

            <section v-if="pickup.length" class="pick" data-testid="mall-pickup">
              <h2>{{ t('ml.pickup_title') }} <small>{{ t('ml.pickup_hint') }}</small></h2>
              <div class="grid" :class="{ dim: loading }"><StoreCard v-for="s in pickup" :key="s.slug" :store="s" :categories="categories" /></div>
            </section>
          </template>
        </template>

        <footer class="mfoot">
          <span>{{ t('ml.apply') }}<router-link to="/me/apply">{{ t('ml.apply_link') }} ›</router-link></span>
          <router-link to="/login" data-testid="mall-staff-login">{{ t('ml.staff_login') }}</router-link>
          <button class="lang" @click="setLocale(locale === 'zh' ? 'en' : 'zh')">{{ locale === 'zh' ? 'EN' : '中文' }}</button>
        </footer>
      </main>
    </div>

    <CustomerTabs active="home" class="mtabs" />
    <MallLocation v-if="showLoc" :region="list?.region" :start-at="showLoc === 'cities' ? 'cities' : undefined" @close="showLoc = false" />
  </div>
</template>

<style scoped>
.mall { min-height: 100vh; padding-bottom: calc(var(--tabbar-h) + env(safe-area-inset-bottom) + 16px); }
.mtop { position: sticky; top: 0; z-index: 20; background: linear-gradient(#ffe2d2, #fff 70%); }
.mall.searching .mtop { background: #fff; border-bottom: 1px solid var(--line); }
.mtop-in { display: grid; grid-template-columns: minmax(0, 1fr) auto; grid-template-areas: 'loc links' 'search search'; gap: 8px 10px; align-items: center; padding: 12px 14px 10px; }
.brand { display: none; font-size: 21px; font-weight: 800; color: var(--brand); white-space: nowrap; }
.loc { grid-area: loc; display: flex; align-items: center; gap: 6px; min-width: 0; border: 0; background: none; padding: 4px 0; cursor: pointer; text-align: left; justify-self: start; max-width: 100%; }
.loc svg { flex: none; }
.loc b { font-size: 17.5px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.loc span { color: #4b5563; font-size: 20px; line-height: 1; }
.mlinks { grid-area: links; display: flex; align-items: center; gap: 18px; }
.mlinks .txt, .mlinks .avatar { display: none; color: var(--ink); white-space: nowrap; }
.msg { position: relative; border: 0; background: none; padding: 2px; color: var(--ink); cursor: pointer; display: grid; }
.msg i { position: absolute; right: -5px; top: -4px; min-width: 17px; height: 17px; padding: 0 3px; border-radius: 9px; background: #e5352b; color: #fff; font-style: normal; font-size: 11px; font-weight: 700; display: grid; place-items: center; border: 1.5px solid #fff; }
.avatar { width: 38px; height: 38px; border-radius: 50%; background: var(--brand-soft); color: var(--brand) !important; font-weight: 700; place-items: center; }
.msearch { grid-area: search; display: flex; align-items: center; gap: 6px; height: 42px; padding: 0 4px 0 14px; background: #fff; border: 1.5px solid var(--brand); border-radius: 22px; color: #8b919a; }
.msearch input { flex: 1; min-width: 0; border: 0; padding: 0; background: none; color: var(--ink); outline: none; -webkit-appearance: none; appearance: none; }
.msearch .go { border: 0; background: var(--brand); color: #fff; font-weight: 700; border-radius: 17px; height: 32px; padding: 0 15px; cursor: pointer; white-space: nowrap; }
.msearch .clear { border: 0; background: none; color: #8b919a; width: 34px; height: 34px; cursor: pointer; }

.mbody { padding: 12px 12px 0; }
.mside { display: none; }
.mmain { min-width: 0; display: grid; gap: 12px; grid-template-columns: minmax(0, 1fr); align-content: start; }

.banners { position: relative; }
.banner { position: relative; display: block; height: 118px; border-radius: 14px; overflow: hidden; color: #fff; background: linear-gradient(100deg, #d9480f, #ee7b3c); }
.banner.link { cursor: pointer; }
.banner img { position: absolute; right: 0; top: 0; height: 100%; width: 46%; object-fit: cover; border-radius: 60px 0 0 60px / 50% 0 0 50%; }
.banner div { position: relative; padding: 16px 16px 0; max-width: 60%; display: grid; gap: 4px; }
.banner b { font-size: 17.5px; line-height: 1.25; }
.banner span { font-size: 12.5px; opacity: .95; display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden; }
.dots { position: absolute; left: 18px; bottom: 10px; display: flex; gap: 5px; }
.dots i { width: 6px; height: 6px; border-radius: 3px; background: rgba(255, 255, 255, .6); cursor: pointer; }
.dots i.on { width: 14px; background: #fff; }

.cats { display: grid; grid-template-columns: repeat(4, 1fr); gap: 12px 4px; background: #fff; border-radius: 14px; padding: 14px 6px; }
.cats button { border: 0; background: none; display: grid; justify-items: center; gap: 4px; font-size: 13.5px; cursor: pointer; padding: 0; }
.cats span { width: 46px; height: 46px; border-radius: 50%; background: var(--brand-soft); display: grid; place-items: center; font-size: 24px; border: 2px solid transparent; }
.cats .on { color: var(--brand); font-weight: 700; }
.cats .on span { border-color: var(--brand); }

h2 { font-size: 17px; }
h2 small { font-weight: 400; font-size: 13px; color: #6b7280; margin-left: 6px; }
.freq > div { display: flex; gap: 14px; overflow-x: auto; padding: 10px 2px 2px; scrollbar-width: none; }
.freq a { flex: none; width: 72px; display: grid; justify-items: center; gap: 5px; color: var(--ink); font-size: 13px; text-align: center; }
.freq img, .freq .letter { width: 66px; height: 66px; border-radius: 12px; object-fit: cover; }
.freq .letter { background: var(--brand-soft); color: var(--brand); display: grid; place-items: center; font-size: 24px; font-weight: 700; }
.freq a span:last-child { width: 100%; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

.regnote { background: var(--info-soft); color: #1e3a8a; border-radius: 10px; padding: 9px 12px; font-size: 13.5px; }
.listhead { display: grid; gap: 10px; min-width: 0; }
.found { font-size: 14px; font-weight: 400; color: #4b5563; }
.sorts { display: flex; align-items: center; gap: 8px; min-height: 34px; overflow-x: auto; scrollbar-width: none; }
.sorts button { flex: none; border: 0; background: #fff; border-radius: 17px; height: 32px; padding: 0 13px; color: #4b5563; cursor: pointer; white-space: nowrap; }
.sorts button.on { background: var(--brand-soft); color: var(--brand); font-weight: 700; }
.sorts .bar { flex: none; width: 1px; height: 16px; background: #d5d8dd; }
.filt { flex: none; }
.filtmenu { position: absolute; z-index: 15; right: 12px; margin-top: 6px; background: #fff; border-radius: 12px; box-shadow: 0 8px 28px rgba(0, 0, 0, .16); padding: 12px 16px 4px; min-width: 170px; }

.grid { display: grid; gap: 12px; grid-template-columns: minmax(0, 1fr); }
.grid.dim { opacity: .6; }
.skel { height: 108px; border-radius: 14px; background: #fff; }
.pick { display: grid; gap: 10px; margin-top: 6px; }
.state { text-align: center; padding: 30px 12px; display: grid; gap: 8px; justify-items: center; }
.state .em { font-size: 54px; line-height: 1; }
.state p { color: #6b7280; }
.state .acts { display: flex; gap: 10px; margin-top: 8px; flex-wrap: wrap; justify-content: center; }
.state.small { padding: 16px; display: block; }
.mfoot { display: flex; flex-wrap: wrap; gap: 6px 18px; justify-content: center; align-items: center; color: #6b7280; font-size: 13.5px; padding: 14px 0 6px; }
.mfoot .lang { border: 1px solid #d5d8dd; background: none; border-radius: 12px; padding: 1px 10px; color: #4b5563; cursor: pointer; font-size: 12.5px; }

@media (min-width: 768px) {
  .mall { padding-bottom: 32px; }
  .mtabs { display: none; }
  .mtop, .mall.searching .mtop { background: #fff; border-bottom: 1px solid var(--line); }
  .mtop-in { max-width: 1320px; margin: 0 auto; grid-template-columns: auto minmax(0, max-content) minmax(180px, 1fr) auto; grid-template-areas: 'brand loc search links'; gap: 18px; padding: 12px 24px; }
  .brand { display: block; grid-area: brand; }
  .loc { background: #f3f4f6; border-radius: 21px; padding: 9px 14px; max-width: 300px; }
  .loc b { font-size: 15px; }
  .mlinks .txt { display: block; }
  .mlinks .txt.wide { display: none; }
  .mlinks .avatar { display: grid; }
  .mbody { max-width: 1320px; margin: 0 auto; padding: 20px 24px 0; display: grid; grid-template-columns: 196px minmax(0, 1fr); gap: 22px; align-items: start; }
  .mside { display: grid; gap: 2px; background: #fff; border-radius: 14px; padding: 10px; position: sticky; top: 86px; }
  .mside button { border: 0; background: none; border-radius: 9px; padding: 10px 12px; text-align: left; cursor: pointer; display: flex; gap: 8px; align-items: center; }
  .mside button:hover { background: #f6f6f4; }
  .mside button.on { background: var(--brand-soft); color: var(--brand); font-weight: 700; }
  .mside hr { border: 0; border-top: 1px solid var(--line); margin: 8px 6px; width: auto; }
  .mside .check { padding: 7px 12px; margin: 0; }
  .mmain { gap: 16px; }
  .cats { display: none; }
  .banner { height: 170px; }
  .banner img { width: 40%; }
  .banner div { padding: 26px 28px 0; max-width: 56%; }
  .banner b { font-size: 24px; } .banner span { font-size: 14.5px; }
  .dots { left: 28px; bottom: 16px; }
  .listhead { grid-template-columns: minmax(0, 1fr) auto; align-items: center; }
  .sorts { overflow: visible; }
  .filt { position: relative; }
  .filtmenu { right: 0; }
  .grid { grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 18px; }
  .skel { height: 260px; }
}
@media (min-width: 1200px) {
  .mlinks .txt.wide { display: block; }
  .grid { grid-template-columns: repeat(3, minmax(0, 1fr)); }
}
</style>
