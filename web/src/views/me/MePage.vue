<script setup lang="ts">
// The frame of every page under "me": a bar with a way back and the title.
// A page that needs an account sends whoever is not logged in to the login
// page, and shows nothing until the profile is there.
import { onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { loginRoute } from '../../customer'
import { t } from '../../i18n'
import CustomerFrame from './CustomerFrame.vue'
import { loadProfile, me } from './me'

// layout: how the page uses a wide screen. "two" puts the page's cards
// (those marked .cell) two by two; "split" keeps the form on the left and
// what was sent before (.side) on the right; "narrow" keeps a short form
// readable. A phone always shows one column.
const props = withDefaults(defineProps<{ title: string; open?: boolean; back?: string; layout?: 'one' | 'two' | 'split' | 'narrow' }>(), { open: false, back: '/me', layout: 'one' })
const route = useRoute(), router = useRouter()
const ready = ref(props.open)

onMounted(async () => {
  const p = await loadProfile()
  if (!p && !props.open) { await router.replace(loginRoute(route.fullPath)); return }
  ready.value = true
})
function goBack() { if (window.history.state?.back) router.back(); else void router.replace(props.back) }
</script>

<template>
  <div class="phone mepage warm">
    <CustomerFrame active="me">
      <header class="mp-bar">
        <button :aria-label="t('back')" data-testid="me-back" @click="goBack">
          <svg viewBox="0 0 24 24" width="22" height="22" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m15 5-7 7 7 7" /></svg></button>
        <b>{{ title }}</b>
        <span class="right"><slot name="right" /></span>
      </header>
      <div v-if="ready" class="mp-body" :class="'l-' + layout" :data-ready="!!me.profile || open"><slot /></div>
      <div v-else class="mp-body muted">{{ t('loading') }}</div>
    </CustomerFrame>
  </div>
</template>

<style>
/* type sizes for the customer's pages: body 14, secondary 12, card titles 15, the bar 17 */
.mepage { padding-bottom: 32px; font-size: 14px; }
.mp-bar { position: sticky; top: 0; z-index: 10; display: grid; grid-template-columns: 48px 1fr 48px; align-items: center; height: 52px; background: var(--red-bg); color: #fff; }
.mp-bar > button { border: 0; background: none; height: 52px; display: grid; place-items: center; cursor: pointer; color: #fff; }
.mp-bar .right .btn.link { color: #fff; }
.mp-bar > b { text-align: center; font-size: 17px; font-weight: 600; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.mp-bar .right { display: flex; justify-content: flex-end; padding-inline-end: 8px; white-space: nowrap; }
.mp-body { padding: 12px; display: grid; gap: 10px; align-content: start; }
.mp-body > div.side:not(.card) { display: grid; gap: 10px; align-content: start; min-width: 0; }
.mepage .card { border: 0; border-radius: 12px; padding: 12px 14px; background: var(--paper); box-shadow: 0 3px 12px rgba(90, 40, 15, .08); }
.mepage .card h2 { font-size: 15px; font-weight: 600; }
.mepage .me-sec { font-size: 15px; font-weight: 600; padding: 4px 4px 0; }
.mepage .btn { border-radius: 999px; background: none; border-color: #d9c7ae; font-weight: 500; min-height: 36px; padding: 7px 14px; }
.mepage .btn.small { min-height: 28px; padding: 3px 12px; font-size: 12px; }
.mepage .btn.big { min-height: 44px; font-size: 15px; }
.mepage .small { font-size: 12px; }
.mepage .tag { font-size: 11px; font-weight: 500; padding: 1px 7px; }
.mepage .banner { font-size: 13px; }
.mepage .btn.primary { background: var(--red); border-color: var(--red); color: #fff; }
.mepage .btn.primary:hover:not(:disabled) { background: var(--red-deep); }
.mepage .btn.link { border: 0; min-height: 0; padding: 4px; }
.mepage input, .mepage textarea, .mepage select { background: #fff; border-color: var(--line); border-radius: 10px; }
.mepage label.field > span { color: var(--ink); font-weight: 500; font-size: 13px; }
.mepage .pill { flex: none; border: 1px solid #cdb9a0; background: none; color: var(--ink); border-radius: 999px; padding: 3px 12px; font-size: 12px; cursor: pointer; white-space: nowrap; }
.mepage .pill.red { border-color: var(--red); background: #fff; color: var(--red); font-weight: 600; }
.mepage .me-go { justify-self: start; color: var(--red); font-weight: 500; font-size: 13px; }
.mepage .empty { text-align: center; color: var(--muted); padding: 40px 20px; font-size: 13px; }
.mepage .note { font-size: 12px; color: var(--muted); line-height: 1.6; }
.mepage .card.me-rows, .me-rows { padding: 0 14px; }
.me-rows > * { display: flex; align-items: center; gap: 10px; min-height: 48px; padding: 6px 0; border-bottom: 1px solid var(--line); color: var(--ink); width: 100%; background: none; border-inline-start: 0; border-inline-end: 0; border-top: 0; text-align: start; cursor: pointer; }
.me-rows > *:last-child { border-bottom: 0; }
.me-rows .v { margin-inline-start: auto; color: var(--muted); display: flex; align-items: center; gap: 6px; min-width: 0; text-align: end; }
.me-rows .chev { color: #b9a793; flex: none; }
.me-logo { width: 40px; height: 40px; border-radius: 10px; object-fit: cover; flex: none; background: var(--brand-soft); color: var(--brand-dark); display: grid; place-items: center; font-size: 16px; font-weight: 600; }
.me-toast { position: fixed; left: 50%; bottom: calc(var(--tabbar-h) + 32px); transform: translateX(-50%); background: rgba(0,0,0,.8); color: #fff; padding: 7px 14px; border-radius: 999px; font-size: 13px; z-index: 90; white-space: nowrap; }
.me-stars i { font-style: normal; color: #e3d6c3; font-size: 13px; } .me-stars i.on { color: #e9a92b; }

/* an iPad or a computer: the red bar is the top bar above (CustomerNav); the
   page's own bar becomes its heading */
@media (min-width: 768px) {
  .mepage.phone { max-width: none; padding-bottom: 40px; }
  .mp-bar { position: static; height: auto; background: none; color: var(--ink); grid-template-columns: auto minmax(0, 1fr) auto; gap: 4px; padding: 0 0 12px; }
  .mp-bar > button { color: var(--ink); height: 36px; width: 36px; margin-inline-start: -8px; border-radius: 50%; }
  .mp-bar > button:hover { background: var(--gold-soft); }
  .mp-bar > b { text-align: start; font-size: 20px; }
  .mp-bar .right { padding-inline-end: 0; }
  .mp-bar .right .btn.link { color: var(--red); }
  .mp-body { padding: 0; gap: 12px; }
  .mepage .card { padding: 16px 18px; }
  .mepage .card.me-rows, .mepage .me-rows { padding: 0 18px; }
  .mp-body.l-narrow { max-width: 640px; }
  .mp-body.l-two { grid-template-columns: repeat(2, minmax(0, 1fr)); align-items: start; }
  .mp-body.l-two > :not(.cell) { grid-column: 1 / -1; }
  .me-toast { bottom: 40px; }
}
/* a computer: the side list is the way back */
@media (min-width: 1024px) {
  .mp-bar > button { display: none; }
  .mp-body.l-split { grid-template-columns: minmax(0, 1.15fr) minmax(0, 1fr); grid-auto-flow: row dense; align-items: start; }
  .mp-body.l-split > :not(.main):not(.side) { grid-column: 1 / -1; }
  .mp-body.l-split > .main { grid-column: 1; }
  .mp-body.l-split > .side { grid-column: 2; }
  /* nothing sent before: the form alone, at a width that reads well */
  .mp-body.l-split:not(:has(> .side)) > .main { grid-column: 1 / -1; max-width: 680px; }
}
@media (min-width: 1200px) {
  .mp-body.l-two.three { grid-template-columns: repeat(3, minmax(0, 1fr)); }
}
</style>
