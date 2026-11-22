<script setup lang="ts">
// A picture field: upload a photo and crop it to the shape this picture is
// shown in, or paste a link as before. The cropped picture is what is stored.
import { onBeforeUnmount, reactive, ref } from 'vue'
import { ApiError } from '../api'
import { t } from '../i18n'
import '../media'
import MediaCropper, { type CropResult } from './MediaCropper.vue'
import { cropUpload, PICTURE_TYPES } from './cropUpload'

// base is the API prefix of the branch; ratio is width / height; width is the stored width in pixels.
const props = defineProps<{ modelValue: string; base: string; ratio: number; width: number; label: string; hint?: string; testid?: string }>()
const emit = defineEmits<{ 'update:modelValue': [v: string] }>()

const picker = ref<HTMLInputElement | null>(null)
const crop = reactive({ open: false, src: '', busy: false, error: '' })
const error = ref('')

function picked(e: Event) {
  const input = e.target as HTMLInputElement
  const f = input.files?.[0]
  input.value = ''
  if (!f) return
  error.value = ''
  if (!PICTURE_TYPES.test(f.type)) { error.value = t('pm.bad_type'); return }
  close()
  Object.assign(crop, { open: true, src: URL.createObjectURL(f), busy: false, error: '' })
}
function close() {
  if (crop.src) URL.revokeObjectURL(crop.src)
  crop.open = false; crop.src = ''
}
onBeforeUnmount(close)

async function save(r: CropResult) {
  crop.busy = true; crop.error = ''
  try {
    emit('update:modelValue', await cropUpload(crop.src, r, props.base, props.ratio, props.width))
    close()
  } catch (e) { crop.error = e instanceof ApiError || e instanceof Error ? e.message : String(e) }
  crop.busy = false
}
</script>

<template>
  <div class="field imgfield" :data-testid="testid">
    <span>{{ label }}<span v-if="hint" class="muted small"> · {{ hint }}</span></span>
    <div class="row line">
      <button type="button" class="pic" :style="{ aspectRatio: String(ratio) }" :aria-label="t('im.upload')" @click="picker?.click()">
        <img v-if="modelValue" :src="modelValue" alt="" /><span v-else>＋</span>
      </button>
      <div class="side">
        <div class="row" style="gap:6px">
          <button type="button" class="btn small" data-role="upload" @click="picker?.click()">{{ t(modelValue ? 'im.change' : 'im.upload') }}</button>
          <button v-if="modelValue" type="button" class="btn small" data-role="remove" @click="emit('update:modelValue', '')">{{ t('im.remove') }}</button>
        </div>
        <input class="link" :value="modelValue" maxlength="500" :placeholder="t('im.or_link')" :aria-label="t('im.or_link')"
          @change="emit('update:modelValue', ($event.target as HTMLInputElement).value.trim())" />
      </div>
    </div>
    <div v-if="error" class="small" style="color:var(--bad, #c92a2a)">{{ error }}</div>
    <input ref="picker" type="file" hidden accept="image/jpeg,image/png,image/webp,image/gif" @change="picked" />
    <Teleport to="body">
      <MediaCropper v-if="crop.open" :key="crop.src" kind="image" plain :ratio="ratio" :src="crop.src" :busy="crop.busy" :error="crop.error"
        @save="save" @cancel="close" />
    </Teleport>
  </div>
</template>

<style scoped>
.imgfield { margin-bottom: 12px; }
.line { gap: 12px; align-items: stretch; }
.pic { flex: none; height: 76px; padding: 0; border: 1px dashed #c9ccd1; border-radius: 10px; background: #fafafa; overflow: hidden; cursor: pointer; display: grid; place-items: center; color: #9aa0a8; font-size: 26px; }
.pic img { width: 100%; height: 100%; object-fit: cover; display: block; }
.side { flex: 1; min-width: 0; display: grid; gap: 6px; align-content: center; }
.link { font-size: 13px; }
</style>
