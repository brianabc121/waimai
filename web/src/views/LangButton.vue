<script setup lang="ts">
// The language button (top right of the customer pages, and in staff
// settings): shows the current language in its own script and opens the
// picker. Each language is listed in its own script with its English name, so
// someone who picked a language they cannot read can still find their way
// back. "Same as browser" undoes a choice.
import { computed, ref } from 'vue'
import { chosen, locale, setLocale, t } from '../i18n'
import { LANGS, detect, langInfo, type Locale } from '../langs'

const props = defineProps<{ label?: 'short' | 'icon' }>()
const open = ref(false)
const browser = computed(() => langInfo(detect(navigator.languages ?? [navigator.language])).name)
const short = computed(() => ({ zh: '中文', 'zh-Hant': '繁中', en: 'EN' } as Record<string, string>)[locale.value] ?? langInfo(locale.value).name)

async function choose(l: Locale | null) {
  open.value = false
  await setLocale(l)
}
</script>

<template>
  <button type="button" class="lang-btn" data-testid="lang-btn" :aria-label="t('lang')" @click="open = true">
    <svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">
      <circle cx="12" cy="12" r="9" /><path d="M3 12h18M12 3c2.5 2.7 3.8 5.7 3.8 9s-1.3 6.3-3.8 9c-2.5-2.7-3.8-5.7-3.8-9S9.5 5.7 12 3z" />
    </svg>
    <span v-if="props.label !== 'icon'">{{ short }}</span>
  </button>
  <Teleport to="body">
    <div v-if="open" class="lang-dim" @click.self="open = false">
      <div class="lang-sheet" role="dialog" :aria-label="t('lang')" data-testid="lang-sheet">
        <div class="lang-grab"></div>
        <h3>{{ t('lang') }}<span v-if="locale !== 'en'"> · Language</span></h3>
        <p>{{ t('lang_remember') }}</p>
        <button type="button" class="lang-item auto" :class="{ on: !chosen }" data-testid="lang-auto" @click="choose(null)">
          {{ t('lang_auto') }}<span class="en">{{ browser }}</span>
        </button>
        <button v-for="l in LANGS" :key="l.code" type="button" class="lang-item" :class="{ on: chosen && locale === l.code }"
          :lang="l.tag" :data-testid="'lang-' + l.code" @click="choose(l.code)">
          <bdi>{{ l.name }}</bdi><span class="en" dir="ltr">{{ l.en }}</span>
        </button>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.lang-btn { display: inline-flex; align-items: center; gap: 5px; white-space: nowrap; }
.lang-dim { position: fixed; inset: 0; z-index: 1000; background: rgba(30, 18, 12, .5); display: flex; align-items: flex-end; justify-content: center; }
.lang-sheet { width: 100%; max-width: 440px; max-height: 88vh; overflow: auto; background: var(--paper, #fffaf2); color: #2b1d17;
  border-radius: 18px 18px 0 0; padding: 8px 16px calc(14px + env(safe-area-inset-bottom)); }
@media (min-width: 700px) { .lang-dim { align-items: center; } .lang-sheet { border-radius: 18px; } }
.lang-grab { width: 40px; height: 4px; border-radius: 2px; background: #d8c8b4; margin: 0 auto 10px; }
.lang-sheet h3 { margin: 0; font-size: 17px; }
.lang-sheet p { margin: 2px 0 10px; font-size: 12px; color: #7a6a5f; }
.lang-item { display: flex; width: 100%; align-items: center; gap: 10px; padding: 10px 6px; border: 0; border-bottom: 1px solid #f1e6d6;
  background: none; font: inherit; font-size: 15px; color: inherit; text-align: start; cursor: pointer; }
.lang-item .en { margin-inline-start: auto; color: #7a6a5f; font-size: 12.5px; }
.lang-item.on { color: var(--red, #a02c20); font-weight: 700; }
.lang-item.on::after { content: "✓"; margin-inline-start: 8px; }
.lang-item.auto { background: var(--gold-soft, #fbe7b3); border-radius: 10px; border: 0; padding: 10px; margin-bottom: 4px; }
</style>
