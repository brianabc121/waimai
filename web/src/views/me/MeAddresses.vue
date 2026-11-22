<script setup lang="ts">
// The address book as a page of its own.
import { ref } from 'vue'
import { t } from '../../i18n'
import AddressSheet from '../AddressSheet.vue'
import MeIntro from './MeIntro.vue'
import MePage from './MePage.vue'
import { homeStore } from './me'

const home = homeStore()
const limit = ref(20)
</script>

<template>
  <MePage :title="t('me.addresses')">
    <MeIntro :title="t('mi.addr')" :sub="t('mi.addr_sub', { n: limit })" icon="M12 13a2.6 2.6 0 1 0 0-5.2 2.6 2.6 0 0 0 0 5.2zM12 21s6.5-5.8 6.5-10.6a6.5 6.5 0 0 0-13 0C5.5 15.2 12 21 12 21z" />
    <AddressSheet page manage @limit="n => limit = n" :slug="home?.slug" :currency="home?.currency" />
  </MePage>
</template>
