<script setup lang="ts">
// More pictures of a dish, for its dish page: upload and crop each one like
// the main picture, move it earlier or later, or take it out.
import { onBeforeUnmount, reactive, ref } from 'vue'
import { ApiError } from '../api'
import { t } from '../i18n'
import '../media'
import MediaCropper, { type CropResult } from './MediaCropper.vue'
import { cropUpload, PICTURE_TYPES } from './cropUpload'

const props = defineProps<{ modelValue: string[]; base: string; ratio: number; width: number; max: number; label: string; hint?: string; testid?: string }>()
const emit = defineEmits<{ 'update:modelValue': [v: string[]] }>()

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
    const url = await cropUpload(crop.src, r, props.base, props.ratio, props.width)
    emit('update:modelValue', [...props.modelValue, url])
    close()
  } catch (e) { crop.error = e instanceof ApiError || e instanceof Error ? e.message : String(e) }
  crop.busy = false
}
function remove(i: number) { emit('update:modelValue', props.modelValue.filter((_, j) => j !== i)) }
function move(i: number, by: number) {
  const l = [...props.modelValue]
  const j = i + by
  if (j < 0 || j >= l.length) return
  ;[l[i], l[j]] = [l[j], l[i]]
  emit('update:modelValue', l)
}
</script>

<template>
  <div class="field photos" :data-testid="testid">
    <span>{{ label }} <span class="muted small">{{ modelValue.length }}/{{ max }}<template v-if="hint"> · {{ hint }}</template></span></span>
    <div class="tiles">
      <div v-for="(p, i) in modelValue" :key="i + p" class="tile" :style="{ aspectRatio: String(ratio) }" data-role="photo">
        <img :src="p" alt="" />
        <button type="button" class="rm" :aria-label="t('im.remove')" data-role="remove" @click="remove(i)">×</button>
        <div v-if="modelValue.length > 1" class="mv">
          <button type="button" :disabled="i === 0" :aria-label="t('im.earlier')" @click="move(i, -1)"><span class="rtl-flip">‹</span></button>
          <button type="button" :disabled="i === modelValue.length - 1" :aria-label="t('im.later')" @click="move(i, 1)"><span class="rtl-flip">›</span></button>
        </div>
      </div>
      <button v-if="modelValue.length < max" type="button" class="tile add" :style="{ aspectRatio: String(ratio) }" data-role="add" :aria-label="t('im.upload')" @click="picker?.click()">＋</button>
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
.photos { margin-bottom: 12px; }
.tiles { display: flex; flex-wrap: wrap; gap: 8px; }
.tile { position: relative; width: 76px; border-radius: 10px; overflow: hidden; background: #fafafa; }
.tile img { width: 100%; height: 100%; object-fit: cover; display: block; }
.tile.add { border: 1px dashed #c9ccd1; color: #9aa0a8; font-size: 26px; cursor: pointer; padding: 0; display: grid; place-items: center; }
.rm { position: absolute; top: 3px; inset-inline-end: 3px; width: 22px; height: 22px; border: 0; border-radius: 50%; background: rgba(0, 0, 0, .55); color: #fff; font-size: 15px; line-height: 1; cursor: pointer; padding: 0; }
.mv { position: absolute; inset-inline-start: 0; inset-inline-end: 0; bottom: 0; display: flex; justify-content: space-between; }
.mv button { width: 26px; height: 22px; border: 0; background: rgba(0, 0, 0, .45); color: #fff; font-size: 16px; line-height: 1; cursor: pointer; padding: 0; }
.mv button:disabled { visibility: hidden; }
</style>
