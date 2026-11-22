<script setup lang="ts">
// One promotional picture, GIF or video, cropped into a 16:9 frame.
import { ref } from 'vue'
import { cropStyle } from '../media'
import type { Crop } from '../types'

defineProps<{ kind: 'image' | 'gif' | 'video'; src: string; crop: Crop; poster?: string; lazy?: boolean; still?: boolean }>()
const video = ref<HTMLVideoElement | null>(null)
defineExpose({ video })
</script>

<template>
  <div class="mframe">
    <!-- a video shows its cover (already cropped) until it plays -->
    <img v-if="kind === 'video' && still" :src="poster" alt="" class="whole" :loading="lazy ? 'lazy' : 'eager'" draggable="false" />
    <video v-else-if="kind === 'video'" ref="video" :src="src" :style="cropStyle(crop)" playsinline preload="metadata"></video>
    <img v-else :src="src" alt="" :style="cropStyle(crop)" :loading="lazy ? 'lazy' : 'eager'" draggable="false" />
  </div>
</template>

<style scoped>
.mframe { position: relative; width: 100%; aspect-ratio: 16 / 9; overflow: hidden; background: #1c1c1c; }
.mframe > * { position: absolute; max-width: none; object-fit: fill; display: block; user-select: none; }
.mframe > .whole { inset: 0; width: 100%; height: 100%; object-fit: cover; }
</style>
