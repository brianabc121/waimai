<script setup lang="ts">
// Storefront templates: each ordering channel picks its own layout and theme.
// Edits are a draft that is previewed here; guests see it only after publishing.
import { computed, onMounted, reactive, ref } from 'vue'
import { get, post, put } from '../api'
import { money, pick, t } from '../i18n'
import { currentRole, session } from '../session'
import { themeVars } from '../theme'
import type { Channel, ChannelTemplate, Storefront, Template } from '../types'
import ImageUpload from './ImageUpload.vue'
import MenuList from './MenuList.vue'

const bid = session.branchId!
const role = currentRole()!
const channels = ref<ChannelTemplate[]>([])
const store = ref<Storefront | null>(null)
const channel = ref<Channel>('delivery')
const form = reactive<Template>({ layout: 'list', theme_color: '#d9480f', logo_url: '', cover_url: '' })
const error = ref('')
const notice = ref('')
const busy = ref(false)
const layouts = ['list', 'featured', 'grid'] as const

const current = computed(() => channels.value.find(c => c.channel === channel.value) ?? null)
const dirty = computed(() => {
  const base = current.value?.draft ?? current.value?.published
  return !!base && (['layout', 'theme_color', 'logo_url', 'cover_url'] as const).some(k => (base[k] ?? '') !== form[k])
})

function fill() { const c = current.value; if (c) Object.assign(form, { logo_url: '', cover_url: '' }, c.draft ?? c.published) }
async function load() {
  channels.value = (await get<{ channels: ChannelTemplate[] }>(`/merchant/branches/${bid}/templates`)).channels
  fill()
}
onMounted(async () => {
  try { store.value = await get<Storefront>(`/stores/${role.slug}`); await load() } catch (e) { error.value = (e as Error).message }
})
function pickChannel(c: Channel) { channel.value = c; error.value = ''; notice.value = ''; fill() }

async function run(fn: () => Promise<unknown>, done = '') {
  busy.value = true; error.value = ''; notice.value = ''
  try { await fn(); await load(); notice.value = done } catch (e) { error.value = (e as Error).message }
  busy.value = false
}
const base = () => `/merchant/branches/${bid}/templates/${channel.value}`
const save = () => run(() => put(base(), form), t('saved'))
const publish = () => run(async () => { if (dirty.value || !current.value?.draft) await put(base(), form); await post(base() + '/publish') }, t('published_ok'))
const discard = () => run(() => post(base() + '/discard'))
</script>

<template>
  <div class="page">
    <h1 style="margin-bottom:12px">{{ t('templates') }}</h1>
    <div class="row wrap" style="gap:8px;margin-bottom:14px">
      <button v-for="c in channels" :key="c.channel" class="btn" :class="{ primary: c.channel === channel }" :data-channel="c.channel" @click="pickChannel(c.channel)">
        {{ t('ch.' + c.channel) }} · {{ t('ly.' + c.published.layout) }}<span v-if="c.draft" class="dot" :title="t('has_draft')"></span>
      </button>
    </div>
    <div v-if="error" class="banner bad" style="margin-bottom:12px">{{ error }}</div>
    <div v-if="notice" class="banner ok" style="margin-bottom:12px" data-test="tpl-notice">{{ notice }}</div>

    <div class="split" v-if="current && store">
      <div class="card stack">
        <div class="row between">
          <h2>{{ t('ch.' + channel) }}</h2>
          <span><span class="tag ok">{{ t('in_use') }}: {{ t('ly.' + current.published.layout) }}</span>
            <span v-if="current.draft" class="tag warn" style="margin-inline-start:6px" data-test="has-draft">{{ t('has_draft') }}</span></span>
        </div>
        <div>
          <div class="small muted" style="margin-bottom:6px">{{ t('layout') }}</div>
          <label v-for="l in layouts" :key="l" class="lay" :class="{ on: form.layout === l }">
            <input type="radio" name="layout" :value="l" v-model="form.layout" />
            <span><b>{{ t('ly.' + l) }}</b><br /><span class="small muted">{{ t('ly.' + l + '.d') }}</span></span>
          </label>
        </div>
        <label class="field"><span>{{ t('theme_color') }}</span>
          <span class="row" style="gap:8px"><input type="color" v-model="form.theme_color" name="theme" style="width:56px;height:38px;padding:2px" />
            <input v-model="form.theme_color" maxlength="7" style="width:120px" /></span></label>
        <ImageUpload v-model="form.logo_url" :base="`/merchant/branches/${bid}`" :ratio="1" :width="400" :label="t('im.logo')" testid="tpl-logo" />
        <ImageUpload v-model="form.cover_url" :base="`/merchant/branches/${bid}`" :ratio="16 / 9" :width="1280" :label="t('im.cover')" :hint="t('im.cover_hint')" testid="tpl-cover" />
        <div class="row wrap" style="gap:8px">
          <button class="btn" :disabled="busy || !dirty" data-test="save-draft" @click="save">{{ t('save_draft') }}</button>
          <button class="btn primary" :disabled="busy || (!dirty && !current.draft)" data-test="publish-tpl" @click="publish">{{ t('publish_tpl') }}</button>
          <button class="btn link" v-if="current.draft" :disabled="busy" @click="discard">{{ t('discard') }}</button>
        </div>
        <div class="small muted">{{ t('preview_hint') }}</div>
      </div>

      <div>
        <div class="small muted" style="margin-bottom:6px">{{ t('preview') }}</div>
        <div class="device" :class="channel" :style="themeVars(form)" data-test="preview">
          <div class="topbar row between">
            <h1 class="row" style="gap:8px"><img v-if="form.logo_url" :src="form.logo_url" alt="" class="logo" />
              {{ pick(store.branch.name, store.branch.name_en) }}<span v-if="channel !== 'delivery'"> · {{ t('table') }} {{ t('preview_table') }}</span></h1>
          </div>
          <img v-if="form.cover_url" :src="form.cover_url" alt="" class="cover" :style="channel === 'delivery' ? { height: 'auto', aspectRatio: '16 / 9' } : undefined" />
          <MenuList :categories="store.categories" :currency="store.branch.currency" :layout="form.layout"
            :can-order="it => !it.sold_out" :qty-of="() => 0" />
          <div class="pbar"><span class="btn">{{ t('cart') }} · 2</span><span class="grow right bold">{{ money(23800, store.branch.currency) }}</span>
            <span class="btn primary">{{ channel === 'delivery' ? t('checkout') : t('send_to_kitchen') }}</span></div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.split { display: grid; grid-template-columns: minmax(300px, 420px) minmax(0, 1fr); gap: 18px; align-items: start; }
@media (max-width: 900px) { .split { grid-template-columns: minmax(0, 1fr); } }
.lay { display: flex; gap: 10px; align-items: flex-start; border: 1px solid var(--line); border-radius: 10px; padding: 10px; margin-bottom: 8px; cursor: pointer; }
.lay.on { border-color: var(--brand); background: var(--brand-soft); }
.dot { display: inline-block; width: 8px; height: 8px; border-radius: 50%; background: var(--warn); margin-inline-start: 6px; }
.device { border: 8px solid #1f2328; border-radius: 22px; overflow: auto; background: var(--bg); width: 390px; max-width: 100%; height: 640px; position: relative; }
.device.ipad { width: 100%; max-width: 900px; height: 600px; }
.pbar { position: sticky; bottom: 0; display: flex; align-items: center; gap: 10px; padding: 10px 14px; background: #fff; border-top: 1px solid var(--line); pointer-events: none; z-index: 20; }
</style>
