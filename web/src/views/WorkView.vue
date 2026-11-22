<script setup lang="ts">
// The staff workbench: one screen for people who work in the store or deliver.
// A person's posts decide which identities they have; they switch between
// "in store" (their stations, the packing counter) and "rider" (deliveries).
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { locale, pick, setLocale, t } from '../i18n'
import { currentRole, identities, logout, session, type Identity } from '../session'
import type { Post } from '../types'
import WorkRider from './work/WorkRider.vue'
import WorkStore from './work/WorkStore.vue'
import MyShifts from './work/MyShifts.vue'
import { disablePush, enablePush, forgetPush, push, setupWorkbench } from './work/push'
import { native, platform } from './work/native'
import { openAppSettings } from './work/duty'
import './work/strings'

const router = useRouter()
const role = computed(() => currentRole())
const ids = computed(() => identities())
const posts = computed(() => role.value?.posts ?? [])
const KEY = computed(() => `work-identity-${session.branchId}`)
const saved = localStorage.getItem(KEY.value) as Identity | null
const identity = ref<Identity>(saved && ids.value.includes(saved) ? saved : ids.value[0] ?? 'store')
// A manager can take a post away while the workbench is open.
watch(ids, v => { if (v.length && !v.includes(identity.value)) identity.value = v[0] })
function become(i: Identity) { identity.value = i; localStorage.setItem(KEY.value, i); sheet.value = false; tab.value = 'tasks' }

const tab = ref<'tasks' | 'done' | 'me'>('tasks')
const sheet = ref(false)
const counts = reactive<Record<Identity, number>>({ store: 0, rider: 0 })
const other = computed<Identity>(() => identity.value === 'store' ? 'rider' : 'store')
const postName = (p: Post) => p.kind === 'station' ? pick(p.station_name ?? '', p.station_name_en ?? '') : p.kind === 'packing' ? t('wk.packing') : t('rider')
const storePosts = computed(() => posts.value.filter(p => p.kind !== 'rider').map(postName).join('、'))
const office = computed(() => ['owner', 'manager', 'cashier'].includes(role.value?.role ?? ''))
async function out() {
  await forgetPush()
  await logout()
  // What this person left on the phone is not the next person's business.
  for (const k of Object.keys(localStorage)) if (/^work-(ops|points|tasks)/.test(k)) localStorage.removeItem(k)
  session.loaded = true; router.replace('/login')
}
onMounted(setupWorkbench)
const pushError = ref('')
async function togglePush() {
  pushError.value = ''
  try { await (push.on ? disablePush() : enablePush()) } catch (e) { pushError.value = (e as Error).message }
}
const standalone = native || window.matchMedia('(display-mode: standalone)').matches
</script>

<template>
  <div class="phone wk" :data-identity="identity">
    <div class="topbar row between">
      <div><h1 style="font-size:18px">{{ t('wk.title') }}</h1>
        <div class="small" style="opacity:.85">{{ session.user?.display_name }} · {{ role?.branch_name }}</div></div>
      <button v-if="ids.length > 1" class="idsw" data-testid="identity" @click="sheet = true">{{ t('wk.id.' + identity) }}</button>
      <span v-else-if="ids.length" class="idsw solo" data-testid="identity">{{ t('wk.id.' + identity) }}</span>
    </div>

    <div v-if="!ids.length" class="section stack" data-testid="no-posts">
      <div class="card stack" style="text-align:center;padding:28px 18px">
        <h2>{{ t('wk.no_posts') }}</h2><p class="muted">{{ t('wk.no_posts_hint') }}</p></div>
      <router-link v-if="office" class="btn block" to="/merchant/orders">{{ t('wk.back_office') }}</router-link>
      <button class="btn block" @click="out">{{ t('sign_out') }}</button>
    </div>

    <template v-else>
      <!-- Both identities stay mounted so each keeps its live stream and its count. -->
      <WorkStore v-if="ids.includes('store')" v-show="identity === 'store' && tab !== 'me'" :tab="tab === 'done' ? 'done' : 'tasks'"
        :rider-tasks="ids.includes('rider') ? counts.rider : 0" @count="counts.store = $event" @switch="become('rider')" />
      <WorkRider v-if="ids.includes('rider')" v-show="identity === 'rider' && tab !== 'me'" :tab="tab === 'done' ? 'done' : 'tasks'"
        :active="identity === 'rider'" @count="counts.rider = $event" />

      <div v-if="tab === 'me'" class="section stack" data-testid="me">
        <div class="card stack">
          <div class="row between"><span class="muted">{{ t('display_name') }}</span><b>{{ session.user?.display_name }}</b></div>
          <div class="row between"><span class="muted">{{ t('wk.identity') }}</span><b>{{ t('wk.id.' + identity + '_long') }}</b></div>
          <div class="row between" style="align-items:flex-start"><span class="muted">{{ t('wk.posts') }}</span>
            <span style="text-align:right" data-testid="my-posts"><span v-for="p in posts" :key="p.kind + (p.station_id ?? '')" class="tag" style="margin:0 0 4px 4px">{{ postName(p) }}</span></span></div>
        </div>
        <MyShifts />
        <div class="card stack">
          <div class="row between"><span>{{ t('wk.language') }}</span>
            <button class="btn small" @click="setLocale(locale === 'zh' ? 'en' : 'zh')">{{ locale === 'zh' ? 'English' : '中文' }}</button></div>
          <router-link v-if="ids.includes('store')" class="row between" to="/kitchen"><span>{{ t('wk.kitchen_screen') }}</span><span>›</span></router-link>
          <router-link v-if="office" class="row between" to="/merchant/orders"><span>{{ t('wk.back_office') }}</span><span>›</span></router-link>
        </div>
        <div v-if="ids.includes('rider') && push.supported" class="card stack" data-testid="push-card">
          <div class="row between"><span>{{ t('wk.push') }} <span v-if="push.on" class="tag ok">{{ t('wk.push.on') }}</span></span>
            <button class="btn small" :disabled="push.busy || (push.denied && !native)" data-act="push" @click="togglePush">{{ push.on ? t('wk.push.disable') : t('wk.push.enable') }}</button></div>
          <div class="small muted">{{ push.denied ? t(native ? 'wk.push.denied_app' : 'wk.push.denied') : t('wk.push.hint') }}</div>
          <div v-if="pushError" class="banner bad small">{{ pushError }}</div>
        </div>
        <div v-if="native && platform === 'android' && ids.includes('rider')" class="card stack" data-testid="battery-card">
          <b>{{ t('wk.battery') }}</b>
          <div class="small muted">{{ t('wk.battery_hint') }}</div>
          <button class="btn small" @click="openAppSettings">{{ t('wk.app_settings') }}</button>
        </div>
        <p v-if="!standalone" class="muted small" style="text-align:center">{{ t('wk.install') }}</p>
        <button class="btn block" data-testid="sign-out" @click="out">{{ t('sign_out') }}</button>
      </div>

      <nav class="wk-tabs">
        <button :class="{ on: tab === 'tasks' }" data-testid="tab-tasks" @click="tab = 'tasks'">{{ t('wk.tab.tasks') }}<em v-if="counts[identity]">{{ counts[identity] }}</em></button>
        <button :class="{ on: tab === 'done' }" data-testid="tab-done" @click="tab = 'done'">{{ t('wk.tab.done') }}</button>
        <button :class="{ on: tab === 'me' }" data-testid="tab-me" @click="tab = 'me'">{{ t('wk.tab.me') }}</button>
      </nav>
    </template>

    <div v-if="sheet" class="sheet-mask" @click.self="sheet = false">
      <div class="sheet" data-testid="identity-sheet">
        <div class="sheet-body stack">
          <h2>{{ t('wk.switch') }}</h2>
          <button v-for="i in ids" :key="i" class="idopt" :class="{ on: i === identity }" :data-id="i" @click="become(i)">
            <span class="row between"><b>{{ t('wk.id.' + i + '_long') }}</b>
              <span v-if="i === identity" class="tag ok">{{ t('wk.current') }}</span>
              <span v-else-if="counts[i]" class="tag brand">{{ counts[i] }}</span></span>
            <span class="muted small">{{ i === 'store' ? t('wk.posts') + '：' + storePosts + ' · ' : '' }}{{ counts[i] ? t('wk.n_tasks', { n: counts[i] }) : (i === 'rider' && i !== identity ? t('wk.rider_hint') : t('wk.no_tasks_now')) }}</span>
          </button>
          <p class="muted small">{{ t('wk.switch_note') }}</p>
          <button class="btn primary block big" data-testid="switch-go" @click="become(other)">{{ t('wk.switch_to', { name: t('wk.id.' + other) }) }}</button>
        </div>
      </div>
    </div>
  </div>
</template>

<style>
.wk { padding-bottom: 76px; }
.wk .idsw { border: 0; background: rgba(255,255,255,.22); color: #fff; border-radius: 999px; padding: 6px 12px; font-weight: 700; font-size: 13px; cursor: pointer; white-space: nowrap; }
.wk .idsw::after { content: " ▾"; font-size: 11px; } .wk .idsw.solo::after { content: ""; }
.wk-tabs { position: fixed; left: 0; right: 0; bottom: 0; z-index: 20; max-width: 520px; margin: 0 auto; display: flex; background: #fff; border-top: 1px solid var(--line); padding-bottom: env(safe-area-inset-bottom); }
.wk-tabs button { flex: 1; border: 0; background: none; padding: 13px 0; color: var(--muted); font-size: 13.5px; cursor: pointer; }
.wk-tabs button.on { color: var(--brand); font-weight: 700; }
.wk-tabs em { font-style: normal; background: var(--brand); color: #fff; border-radius: 999px; font-size: 11px; padding: 0 6px; margin-left: 4px; }
.wk .idopt { display: grid; gap: 3px; width: 100%; text-align: left; border: 1.5px solid var(--line); border-radius: 14px; padding: 12px 14px; background: #fff; cursor: pointer; }
.wk .idopt.on { border-color: var(--brand); background: var(--brand-soft); } .wk .idopt b { font-size: 16px; }
.wk .chips { display: flex; gap: 6px; flex-wrap: wrap; }
.wk .chips button { padding: 6px 12px; border-radius: 999px; background: #fff; border: 1px solid var(--line); font-size: 13px; color: var(--muted); cursor: pointer; }
.wk .chips button.on { background: var(--brand); border-color: var(--brand); color: #fff; font-weight: 700; }
.wk .task { display: grid; grid-template-columns: 1fr auto; gap: 4px 10px; align-items: center; }
.wk .task .q { font-size: 17px; font-weight: 700; } .wk .task .src { font-size: 12.5px; color: var(--muted); }
.wk .task.preparing { border-left: 4px solid var(--info); } .wk .late { color: var(--bad); font-weight: 600; }
.wk .card.fresh { border: 2px solid var(--brand); box-shadow: 0 2px 10px rgba(217,72,15,.15); }
.wk .no { font-size: 18px; } .wk .line { display: flex; gap: 8px; align-items: baseline; }
.wk .pt { width: 9px; height: 9px; border-radius: 50%; flex: none; background: var(--info); } .wk .pt.s { background: var(--brand); }
.wk .k { font-size: 12px; color: var(--muted); } .wk .amt { font-size: 22px; color: var(--brand-dark); font-weight: 700; }
.wk .subbar { position: sticky; top: 0; z-index: 5; background: #fff; border-bottom: 1px solid var(--line); padding: 10px 16px; }
.wk .back { border: 0; background: none; color: var(--ink); font-size: 17px; font-weight: 700; padding: 0; cursor: pointer; }
.wk .quick { display: flex; gap: 8px; flex-wrap: wrap; }
.wk .quick button { border: 1px solid var(--line); border-radius: 999px; padding: 5px 12px; font-size: 13px; background: #fff; cursor: pointer; }
.wk .quick button.on { border-color: var(--brand); color: var(--brand-dark); background: var(--brand-soft); font-weight: 600; }
.wk .duty { display: grid; gap: 6px; border-left: 4px solid var(--line); } .wk .duty.on { border-left-color: var(--ok); }
.wk .cash input { font-size: 26px; font-weight: 700; border: 2px solid var(--brand); }
</style>
