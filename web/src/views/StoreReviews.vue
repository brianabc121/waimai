<script setup lang="ts">
// The reviews tab of the store page: the score, the filters and the list.
import { reactive, ref, watch } from 'vue'
import { get } from '../api'
import { locale, pick, t } from '../i18n'
import type { Review, ReviewPage, ReviewSummary } from '../types'

const props = defineProps<{ slug: string; summary: ReviewSummary; timezone: string; delivery: boolean }>()

const filters = ['all', 'latest', 'good', 'bad'] as const
const filter = ref<typeof filters[number]>('all')
const state = reactive<{ reviews: Review[]; more: boolean; busy: boolean; error: string }>({ reviews: [], more: false, busy: true, error: '' })
const open = reactive(new Set<number>())

async function load(reset: boolean) {
  state.busy = true; state.error = ''
  try {
    const p = await get<ReviewPage>(`/stores/${props.slug}/reviews?filter=${filter.value}&offset=${reset ? 0 : state.reviews.length}`)
    state.reviews = reset ? p.reviews : [...state.reviews, ...p.reviews]
    state.more = p.more
  } catch (e) { state.error = (e as Error).message }
  state.busy = false
}
watch(filter, () => load(true), { immediate: true })

const one = (n: number) => n.toFixed(1).replace(/\.0$/, '')
// 2026-11-19 18:38 on the store's clock
function when(iso: string) {
  try { return new Date(iso).toLocaleString('sv-SE', { timeZone: props.timezone, dateStyle: 'short', timeStyle: 'short' }) }
  catch { return new Date(iso).toLocaleString(locale.value) }
}
</script>

<template>
  <div class="reviews">
    <div class="sum" data-testid="review-summary">
      <b class="avg">{{ summary.count ? one(summary.average) : '–' }}</b>
      <div class="grow">
        <div class="muted">{{ t('sf.rating_title') }}</div>
        <div class="stars big"><i v-for="n in 5" :key="n" :class="{ on: summary.count > 0 && n <= Math.round(summary.average) }">★</i></div>
      </div>
      <div class="part"><span>{{ t('sf.taste') }}</span><b>{{ summary.count ? one(summary.taste) : '–' }}</b></div>
      <div class="part"><span>{{ t('sf.packaging') }}</span><b>{{ summary.count ? one(summary.packaging) : '–' }}</b></div>
      <div class="part" v-if="delivery"><span>{{ t('sf.delivery_score') }}</span><b>{{ summary.delivery ? one(summary.delivery) : '–' }}</b></div>
    </div>
    <div class="filters">
      <button v-for="f in filters" :key="f" :class="{ on: filter === f }" :data-filter="f" @click="filter = f">{{ t('sf.f_' + f) }}</button>
    </div>
    <div v-if="state.error" class="banner bad" style="margin:0 16px">{{ state.error }}</div>
    <p v-else-if="!state.busy && !state.reviews.length" class="muted none">{{ t('sf.no_reviews') }}</p>
    <div class="list">
      <article v-for="r in state.reviews" :key="r.id" class="rv" data-testid="review">
        <div class="who">
          <span class="avatar" aria-hidden="true"><svg viewBox="0 0 24 24" width="26" height="26"><circle cx="12" cy="9" r="4.2" fill="currentColor" /><path d="M3.5 21c.8-4.6 4.3-7 8.5-7s7.7 2.4 8.5 7z" fill="currentColor" /></svg></span>
          <div class="grow"><div class="nm">{{ r.display_name }}</div>
            <div class="stars"><i v-for="n in r.rating" :key="n" class="on">★</i></div></div>
          <time class="muted small">{{ when(r.created_at) }}</time>
        </div>
        <p :class="{ muted: !r.comment }">{{ r.comment || t('sf.no_comment') }}</p>
        <div class="chips" :class="{ open: open.has(r.id) }" v-if="r.items.length">
          <span v-for="(it, i) in r.items" :key="i">{{ pick(it.name, it.name_en) }}</span>
          <button v-if="r.items.length > 3" :aria-label="t('sf.more')" @click="open.has(r.id) ? open.delete(r.id) : open.add(r.id)">{{ open.has(r.id) ? '︿' : '﹀' }}</button>
        </div>
      </article>
    </div>
    <div class="section" v-if="state.more"><button class="btn block" :disabled="state.busy" @click="load(false)">{{ t('sf.more') }}</button></div>
  </div>
</template>

<style scoped>
.reviews { background: #fff; padding-bottom: 16px; }
.sum { display: flex; align-items: center; gap: 14px; padding: 18px 16px; border-bottom: 6px solid #f4f4f4; }
.avg { font-size: 40px; line-height: 1; color: var(--star); min-width: 64px; text-align: center; }
.stars i { font-style: normal; color: #cfd3d9; font-size: 15px; letter-spacing: 1px; }
.stars i.on { color: var(--star); }
.stars.big i { font-size: 20px; }
.part { display: flex; flex-direction: column; gap: 4px; font-size: 15px; min-width: 44px; }
.part b { font-weight: 500; }
.filters { display: flex; gap: 10px; padding: 14px 16px 6px; flex-wrap: wrap; }
.filters button { border: 0; background: #f2f2f2; color: var(--ink); border-radius: 8px; padding: 6px 14px; font-size: 14px; cursor: pointer; }
.filters button.on { background: var(--brand); color: #fff; font-weight: 600; }
.none { text-align: center; padding: 36px 0; margin: 0; }
.rv { padding: 16px; border-bottom: 1px solid #f0f0f0; }
.rv p { margin: 12px 0; font-size: 15px; white-space: pre-wrap; overflow-wrap: anywhere; }
.who { display: flex; align-items: flex-start; gap: 12px; }
.avatar { width: 44px; height: 44px; border-radius: 50%; background: var(--brand-soft); color: var(--brand); display: grid; place-items: center; flex: none; overflow: hidden; }
.nm { font-size: 15px; }
.chips { display: flex; flex-wrap: wrap; gap: 6px; max-height: 28px; overflow: hidden; position: relative; padding-right: 34px; }
.chips.open { max-height: none; }
.chips span { border: 1px solid #c9cdd3; color: #8a8f97; border-radius: 6px; padding: 1px 7px; font-size: 13px; line-height: 22px; white-space: nowrap; max-width: 100%; overflow: hidden; text-overflow: ellipsis; }
.chips button { position: absolute; right: 0; top: 0; width: 28px; height: 26px; border: 1px solid #c9cdd3; border-radius: 6px; background: #fff; color: #8a8f97; cursor: pointer; font-size: 11px; }
@media (min-width: 768px) {
  .sum { padding: 22px 24px; gap: 22px; }
  .part { min-width: 64px; }
  .filters, .rv { padding-left: 24px; padding-right: 24px; }
}
</style>
