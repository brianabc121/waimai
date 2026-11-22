<script setup lang="ts">
// The stores the customer keeps, and the ones they have ordered from. With no
// store remembered on this device, "home" leads here too, so the page is open
// to someone who is not logged in.
import { computed, ref, watch } from 'vue'
import { pick, t } from '../../i18n'
import CustomerTabs from './CustomerTabs.vue'
import MePage from './MePage.vue'
import { describe, me, myStores, setFavorite, type MyStore } from './me'

const stores = ref<MyStore[] | null>(null)
const error = ref('')
const kept = computed(() => (stores.value ?? []).filter(s => s.favorite))
const visited = computed(() => (stores.value ?? []).filter(s => !s.favorite))
watch(() => me.profile?.id, async id => {
  if (!id) return
  try { stores.value = await myStores() } catch (e) { error.value = describe(e) }
}, { immediate: true })

async function toggle(s: MyStore) {
  try { await setFavorite(s.slug, !s.favorite); s.favorite = !s.favorite; if (me.profile) me.profile.favorites += s.favorite ? 1 : -1 }
  catch (e) { error.value = describe(e) }
}
</script>

<template>
  <MePage :title="t('mf.title')" open>
    <div v-if="error" class="banner bad">{{ error }}</div>
    <div v-if="!me.profile" class="card empty">{{ t('mf.no_store') }}<br /><router-link to="/account/login?next=/me/favorites" class="btn primary" style="margin-top:14px">{{ t('me.login') }}</router-link></div>
    <template v-else-if="stores">
      <div v-if="!kept.length" class="card empty" data-testid="fav-empty">{{ t(visited.length ? 'mf.none' : 'mf.no_store') }}</div>
      <div v-else class="card list">
        <div v-for="s in kept" :key="s.slug" class="store" data-testid="fav-store">
          <router-link :to="`/s/${s.slug}`" class="lnk">
            <img v-if="s.logo_url" :src="s.logo_url" alt="" class="me-logo" /><span v-else class="me-logo">{{ pick(s.name, s.name_en).slice(0, 1) }}</span>
            <span class="grow"><b>{{ pick(s.name, s.name_en) }}</b><span class="small muted">{{ s.address }}</span></span>
          </router-link>
          <button class="btn small" data-testid="fav-remove" @click="toggle(s)">{{ t('mf.unfav') }}</button>
        </div>
      </div>
      <template v-if="visited.length">
        <h3 style="padding:4px 6px 0">{{ t('mf.visited') }}</h3>
        <div class="card list">
          <div v-for="s in visited" :key="s.slug" class="store" data-testid="visited-store">
            <router-link :to="`/s/${s.slug}`" class="lnk">
              <img v-if="s.logo_url" :src="s.logo_url" alt="" class="me-logo" /><span v-else class="me-logo">{{ pick(s.name, s.name_en).slice(0, 1) }}</span>
              <span class="grow"><b>{{ pick(s.name, s.name_en) }}</b><span class="small muted">{{ s.address }}</span></span>
            </router-link>
            <button class="btn small" data-testid="fav-add" @click="toggle(s)">{{ t('mf.fav') }}</button>
          </div>
        </div>
      </template>
    </template>
    <CustomerTabs active="home" />
  </MePage>
</template>

<style scoped>
.list { padding: 2px 14px; }
.store { display: flex; align-items: center; gap: 10px; padding: 12px 0; border-bottom: 1px solid #f0f1f3; }
.store:last-child { border-bottom: 0; }
.lnk { display: flex; align-items: center; gap: 12px; flex: 1; min-width: 0; color: var(--ink); }
.lnk .grow { display: grid; } .lnk .small { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
</style>
