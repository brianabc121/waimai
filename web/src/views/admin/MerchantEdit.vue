<script setup lang="ts">
import { reactive, ref } from 'vue'
import { put } from '../../api'
import { t } from '../../i18n'
import type { MerchantRow } from './types'

const props = defineProps<{ merchant: MerchantRow }>()
const emit = defineEmits<{ saved: []; close: [] }>()
const f = reactive({ name: props.merchant.name, contact_name: props.merchant.contact_name, contact_phone: props.merchant.contact_phone, note: props.merchant.note })
const error = ref('')

async function save() {
  error.value = ''
  try { await put(`/admin/merchants/${props.merchant.id}`, f); emit('saved') } catch (e) { error.value = (e as Error).message }
}
</script>

<template>
  <div class="adm-mask" @click="emit('close')"></div>
  <form class="adm-drawer" data-testid="merchant-edit" @submit.prevent="save">
    <div class="dh"><h2>{{ t('edit') }} · {{ merchant.name }}</h2></div>
    <div class="db">
      <div v-if="error" class="banner bad" style="margin-bottom:12px">{{ error }}</div>
      <label class="field"><span>{{ t('merchant_name') }}</span><input v-model="f.name" name="name" required /></label>
      <div class="f2">
        <label class="field"><span>{{ t('ad.contact') }}</span><input v-model="f.contact_name" name="contact_name" /></label>
        <label class="field"><span>{{ t('ad.contact_phone') }}</span><input v-model="f.contact_phone" name="contact_phone" /></label>
      </div>
      <label class="field"><span>{{ t('ad.note_private') }}</span><input v-model="f.note" name="note" /></label>
    </div>
    <div class="df"><button type="button" class="btn" @click="emit('close')">{{ t('cancel') }}</button><button class="btn primary">{{ t('save') }}</button></div>
  </form>
</template>
