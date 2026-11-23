<script setup lang="ts">
// What the customer has said about their orders.
import { ref, watch } from 'vue'
import { pick, t } from '../../i18n'
import MeIntro from './MeIntro.vue'
import MePage from './MePage.vue'
import { dateTime, describe, me, myReviews, type MyReview } from './me'

const reviews = ref<MyReview[] | null>(null)
const error = ref('')
watch(() => me.profile?.id, async id => {
  if (!id) return
  try { reviews.value = await myReviews() } catch (e) { error.value = describe(e) }
}, { immediate: true })
</script>

<template>
  <MePage :title="t('mr.title')">
    <MeIntro :title="t('mi.reviews')" :sub="t('mi.reviews_sub')" :icon="'M19 12v5.5a2 2 0 0 1-2 2H6.5a2 2 0 0 1-2-2v-11a2 2 0 0 1 2-2H14M8 10h4M8 14h3M14.5 13.5l6-6-2-2-6 6-.5 2.5z'" />
    <div v-if="error" class="banner bad">{{ error }}</div>
    <div v-if="reviews && !reviews.length" class="card empty" data-testid="reviews-empty">{{ t('mr.none') }}</div>
    <div v-for="r in reviews ?? []" :key="r.id" class="card stack" data-testid="my-review">
      <div class="row">
        <img v-if="r.store.logo_url" :src="r.store.logo_url" alt="" class="me-logo" /><span v-else class="me-logo">{{ pick(r.store.name, r.store.name_en).slice(0, 1) }}</span>
        <div class="grow"><router-link :to="`/s/${r.store.slug}`" class="bold" style="color:var(--ink);font-size:14px;font-weight:600">{{ pick(r.store.name, r.store.name_en) }}</router-link>
          <div class="small muted">{{ dateTime(r.created_at) }}</div></div>
        <span class="me-stars"><i v-for="n in 5" :key="n" :class="{ on: n <= r.rating }">★</i></span>
      </div>
      <p v-if="r.comment" style="white-space:pre-wrap">{{ r.comment }}</p>
      <div class="small muted">{{ r.items.map(i => pick(i.name, i.name_en)).join(' · ') }}</div>
      <router-link :to="`/o/${r.token}`" class="me-go">{{ t('mr.order') }} ›</router-link>
    </div>
  </MePage>
</template>
