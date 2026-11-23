<script setup lang="ts">
// The head of a page under "me": the red of the bar runs on under a card that
// says what the page is for, and ends in a curve. `icon` is a 24×24 path; a
// page may put its own drawing in the "icon" slot instead.
defineProps<{ title: string; sub?: string; icon?: string }>()
</script>

<template>
  <div class="me-intro">
    <div class="info">
      <span class="ic"><slot name="icon"><svg viewBox="0 0 24 24" width="32" height="32" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path :d="icon" /></svg></slot></span>
      <p><b>{{ title }}</b><span v-if="sub">{{ sub }}</span></p>
      <slot />
    </div>
  </div>
</template>

<style>
.me-intro { margin: -12px -12px 2px; padding: 6px 12px 0; position: relative; }
/* the bar's red and pattern run on (the bar is 52px tall) and end in a curve */
.me-intro::before { content: ""; position: absolute; left: 0; right: 0; top: 0; height: 84px; --motif-y: -52px; background: var(--red-bg); border-radius: 0 0 50% 50% / 0 0 22px 22px; }
.me-intro .info { position: relative; display: flex; align-items: center; gap: 16px; background: var(--paper); border-radius: 18px; padding: 14px 16px; box-shadow: 0 6px 18px rgba(90, 40, 15, .12); }
.me-intro .ic { flex: none; width: 62px; height: 62px; border-radius: 50%; background: #f9dcd6; color: var(--red); display: grid; place-items: center; }
.me-intro p { flex: 1; display: grid; gap: 4px; min-width: 0; padding-left: 16px; border-left: 1px solid var(--line); }
.me-intro b { font-size: 16px; line-height: 1.4; }
.me-intro p span { color: var(--muted); font-size: 14.5px; line-height: 1.45; }
@media (max-width: 380px) { .me-intro .info { gap: 10px; } .me-intro p { padding-left: 10px; } .me-intro .ic { width: 52px; height: 52px; } }
</style>
