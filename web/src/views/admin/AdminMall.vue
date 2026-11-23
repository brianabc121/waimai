<script setup lang="ts">
// The platform's side of the marketplace: the banners at its top, the names
// and order of the categories, and which stores show (and why one does not).
import { computed, onMounted, reactive, ref } from 'vue'
import { del, get, post, put, upload } from '../../api'
import { pick, t } from '../../i18n'
import type { MallBanner, MallCategory } from '../../mall'
import { shrink } from '../../photo'

interface Store { id: number; merchant_id: number; slug: string; name: string; name_en: string; logo_url: string; city: string
  categories: string[]; listed: boolean; hidden: boolean; state: 'shown' | 'hidden' | 'off' | 'suspended' | 'no_location' | 'no_zones' }
const view = ref<{ banners: MallBanner[]; categories: MallCategory[]; stores: Store[] } | null>(null)
const error = ref('')
const q = ref(''), only = ref('')

async function load() {
  try { view.value = await get('/admin/mall'); error.value = '' } catch (e) { error.value = (e as Error).message }
}
onMounted(load)
async function run(fn: () => Promise<unknown>) {
  error.value = ''
  try { await fn(); await load(); return true } catch (e) { error.value = (e as Error).message; return false }
}

// ---- banners ----
const blank = () => ({ id: 0, title: '', title_en: '', subtitle: '', subtitle_en: '', link_slug: '', link_url: '', sort: 0, active: true })
const b = reactive(blank())
const editing = ref(false)
function edit(x: MallBanner | null) {
  Object.assign(b, x ? { id: x.id, title: x.title, title_en: x.title_en, subtitle: x.subtitle, subtitle_en: x.subtitle_en, link_slug: x.link_slug, link_url: x.link_url, sort: x.sort, active: x.active } : blank())
  editing.value = true
}
async function saveBanner() {
  const body = { ...b, sort: Number(b.sort) || 0 }
  if (await run(() => b.id ? put(`/admin/mall/banners/${b.id}`, body) : post('/admin/mall/banners', body))) editing.value = false
}
function removeBanner(x: MallBanner) { if (confirm(t('adm.banner_delete', { name: x.title }))) void run(() => del(`/admin/mall/banners/${x.id}`)) }
async function picture(x: MallBanner, e: Event) {
  const input = e.target as HTMLInputElement
  const f = input.files?.[0]
  input.value = ''
  if (f) await run(async () => upload(`/admin/mall/banners/${x.id}/image`, await shrink(f)))
}
const full = computed(() => (view.value?.banners.length ?? 0) >= 5)

// ---- categories ----
const saveCat = (c: MallCategory) => run(() => put(`/admin/mall/categories/${c.id}`, { name: c.name, name_en: c.name_en, icon: c.icon, sort: Number(c.sort) || 0, active: c.active }))

// ---- stores ----
const stores = computed(() => (view.value?.stores ?? []).filter(s =>
  (!only.value || s.state === only.value) && (!q.value.trim() || `${s.name} ${s.name_en} ${s.slug} ${s.city}`.toLowerCase().includes(q.value.trim().toLowerCase()))))
const counts = computed(() => {
  const n: Record<string, number> = {}
  for (const s of view.value?.stores ?? []) n[s.state] = (n[s.state] ?? 0) + 1
  return n
})
const hide = (s: Store, hidden: boolean) => run(() => put(`/admin/mall/stores/${s.id}`, { hidden }))
const catNames = (s: Store) => s.categories.map(code => view.value!.categories.find(c => c.code === code)).filter(Boolean).map(c => pick(c!.name, c!.name_en)).join(' · ')
const STATES = ['shown', 'hidden', 'off', 'suspended', 'no_location', 'no_zones']
</script>

<template>
  <div class="adm-page" data-testid="admin-mall">
    <div class="banner info">{{ t('adm.intro') }} <a href="/" target="_blank" rel="noopener">{{ t('adm.open') }}</a></div>
    <div v-if="error" class="banner bad" data-testid="admin-mall-error">{{ error }}</div>
    <template v-if="view">
      <div class="card stack">
        <div class="row between"><div><h2>{{ t('adm.banners') }}</h2><div class="small muted">{{ t('adm.banners_hint') }}</div></div>
          <button class="btn primary" :disabled="full" data-testid="banner-new" @click="edit(null)">{{ t('adm.banner_new') }}</button></div>
        <div class="table-wrap"><table v-if="view.banners.length" class="data">
          <thead><tr><th>{{ t('adm.picture') }}</th><th>{{ t('adm.b_title') }}</th><th>{{ t('adm.link') }}</th><th>{{ t('adm.sort') }}</th><th>{{ t('ad.status') }}</th><th></th></tr></thead>
          <tbody><tr v-for="x in view.banners" :key="x.id" data-testid="banner-row">
            <td><img v-if="x.image_url" :src="x.image_url" alt="" class="thumb" /><span v-else class="thumb none">{{ t('adm.no_picture') }}</span></td>
            <td><b>{{ x.title }}</b><div class="small muted">{{ x.subtitle }}</div><div v-if="x.title_en" class="small muted">{{ x.title_en }}</div></td>
            <td class="small">{{ x.link_slug ? '/s/' + x.link_slug : x.link_url || '—' }}</td>
            <td>{{ x.sort }}</td>
            <td><span class="tag" :class="x.active ? 'ok' : ''">{{ t(x.active ? 'enabled' : 'disabled') }}</span></td>
            <td class="right nowrap">
              <label class="btn small">{{ t('adm.upload') }}<input type="file" accept="image/jpeg,image/png,image/webp" hidden data-testid="banner-file" @change="picture(x, $event)" /></label>
              <button v-if="x.image_url" class="btn small" @click="run(() => del(`/admin/mall/banners/${x.id}/image`))">{{ t('adm.no_picture_btn') }}</button>
              <button class="btn small" data-testid="banner-edit" @click="edit(x)">{{ t('edit') }}</button>
              <button class="btn small danger" data-testid="banner-delete" @click="removeBanner(x)">{{ t('delete') }}</button>
            </td>
          </tr></tbody>
        </table></div>
        <form v-if="editing" class="bform" data-testid="banner-form" @submit.prevent="saveBanner">
          <label class="field"><span>{{ t('adm.b_title') }}</span><input v-model="b.title" name="title" maxlength="60" required /></label>
          <label class="field"><span>{{ t('adm.b_title') }} (EN)</span><input v-model="b.title_en" maxlength="60" /></label>
          <label class="field"><span>{{ t('adm.b_sub') }}</span><input v-model="b.subtitle" name="subtitle" maxlength="120" /></label>
          <label class="field"><span>{{ t('adm.b_sub') }} (EN)</span><input v-model="b.subtitle_en" maxlength="120" /></label>
          <label class="field"><span>{{ t('adm.link_store') }}</span>
            <select v-model="b.link_slug" name="link_slug"><option value="">—</option><option v-for="s in view.stores" :key="s.id" :value="s.slug">{{ s.name }} ({{ s.slug }})</option></select></label>
          <label class="field"><span>{{ t('adm.link_url') }}</span><input v-model="b.link_url" placeholder="https://" :disabled="!!b.link_slug" /></label>
          <label class="field"><span>{{ t('adm.sort') }}</span><input v-model="b.sort" type="number" /></label>
          <label class="check" style="align-self:end"><input v-model="b.active" type="checkbox" />{{ t('enabled') }}</label>
          <div class="row" style="gap:8px;grid-column:1/-1"><button class="btn primary" data-testid="banner-save">{{ t('save') }}</button><button type="button" class="btn" @click="editing = false">{{ t('cancel') }}</button></div>
        </form>
      </div>

      <div class="card stack">
        <div><h2>{{ t('adm.cats') }}</h2><div class="small muted">{{ t('adm.cats_hint') }}</div></div>
        <div class="table-wrap"><table class="data">
          <thead><tr><th>{{ t('adm.icon') }}</th><th>{{ t('name_zh') }}</th><th>{{ t('name_en') }}</th><th>{{ t('adm.sort') }}</th><th>{{ t('enabled') }}</th><th></th></tr></thead>
          <tbody><tr v-for="c in view.categories" :key="c.id" data-testid="cat-row">
            <td><input v-model="c.icon" class="narrow" maxlength="8" /></td>
            <td><input v-model="c.name" maxlength="20" /></td>
            <td><input v-model="c.name_en" maxlength="30" /></td>
            <td><input v-model="c.sort" class="narrow" type="number" /></td>
            <td><input v-model="c.active" type="checkbox" /></td>
            <td class="right"><button class="btn small" data-testid="cat-save" @click="saveCat(c)">{{ t('save') }}</button></td>
          </tr></tbody>
        </table></div>
      </div>

      <div class="card stack">
        <div><h2>{{ t('adm.stores') }}</h2><div class="small muted">{{ t('adm.stores_hint') }}</div></div>
        <div class="row wrap" style="gap:8px">
          <input v-model="q" :placeholder="t('adm.find')" style="flex:1 1 200px;max-width:300px" />
          <button class="btn small" :class="{ primary: !only }" @click="only = ''">{{ t('ad.all') }} {{ view.stores.length }}</button>
          <button v-for="s in STATES.filter(x => counts[x])" :key="s" class="btn small" :class="{ primary: only === s }" @click="only = s">{{ t('adm.st_' + s) }} {{ counts[s] }}</button>
        </div>
        <div class="table-wrap"><table class="data">
          <thead><tr><th>{{ t('adm.store') }}</th><th>{{ t('adm.city') }}</th><th>{{ t('adm.cats') }}</th><th>{{ t('ad.status') }}</th><th></th></tr></thead>
          <tbody><tr v-for="s in stores" :key="s.id" data-testid="mall-row" :data-slug="s.slug">
            <td><a :href="'/s/' + s.slug" target="_blank" rel="noopener"><b>{{ s.name }}</b></a><div class="small muted">{{ s.slug }}</div></td>
            <td>{{ s.city || '—' }}</td>
            <td class="small">{{ catNames(s) || '—' }}</td>
            <td><span class="tag" :class="{ ok: s.state === 'shown', bad: s.state === 'hidden' }" data-testid="mall-state">{{ t('adm.st_' + s.state) }}</span></td>
            <td class="right"><button v-if="s.hidden" class="btn small" data-testid="mall-unhide" @click="hide(s, false)">{{ t('adm.unhide') }}</button>
              <button v-else class="btn small danger" data-testid="mall-hide" @click="hide(s, true)">{{ t('adm.hide') }}</button></td>
          </tr></tbody>
        </table></div>
      </div>
    </template>
  </div>
</template>

<style scoped>
.thumb { display: block; width: 120px; height: 44px; object-fit: cover; border-radius: 6px; }
.thumb.none { display: grid; place-items: center; background: linear-gradient(100deg, #d9480f, #ee7b3c); color: #fff; font-size: 12px; }
.bform { display: grid; grid-template-columns: repeat(auto-fit, minmax(220px, 1fr)); gap: 0 14px; border-top: 1px solid var(--line); padding-top: 14px; }
.narrow { width: 76px; }
.table-wrap { overflow-x: auto; }
.nowrap { white-space: nowrap; }
.nowrap .btn { margin-inline-start: 6px; }
td input:not([type=checkbox]) { min-width: 76px; }
</style>
