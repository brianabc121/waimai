// The languages the site speaks. index.html runs the same detection inline
// before the page paints (see the script there); keep the two in step.
export const LANGS = [
  { code: 'zh', tag: 'zh-CN', name: '简体中文', en: 'Chinese (Simplified)' },
  { code: 'en', tag: 'en', name: 'English', en: 'English' },
  { code: 'zh-Hant', tag: 'zh-Hant', name: '繁體中文', en: 'Chinese (Traditional)' },
  { code: 'th', tag: 'th', name: 'ไทย', en: 'Thai' },
  { code: 'vi', tag: 'vi', name: 'Tiếng Việt', en: 'Vietnamese' },
  { code: 'fr', tag: 'fr', name: 'Français', en: 'French' },
  { code: 'ar', tag: 'ar', name: 'العربية', en: 'Arabic', rtl: true },
  { code: 'es', tag: 'es', name: 'Español', en: 'Spanish' },
  { code: 'ru', tag: 'ru', name: 'Русский', en: 'Russian' },
  { code: 'ja', tag: 'ja', name: '日本語', en: 'Japanese' },
  { code: 'my', tag: 'my', name: 'မြန်မာ', en: 'Burmese' },
  { code: 'km', tag: 'km', name: 'ខ្មែរ', en: 'Khmer' },
] as const

export type Locale = typeof LANGS[number]['code']
export const CODES = LANGS.map(l => l.code) as readonly Locale[]
export const isLocale = (s: unknown): s is Locale => typeof s === 'string' && (CODES as readonly string[]).includes(s)
export const langInfo = (l: Locale) => LANGS.find(x => x.code === l)!

// fromBrowser maps a browser language ("zh-TW", "th-TH", "fr-CA") to ours.
export function fromBrowser(tag: string): Locale | null {
  const t = tag.toLowerCase()
  if (t.startsWith('zh')) return /hant|tw|hk|mo/.test(t) ? 'zh-Hant' : 'zh'
  const p = t.split('-')[0]
  return isLocale(p) ? p : null
}

export function detect(list: readonly string[]): Locale {
  for (const tag of list) { const l = fromBrowser(tag); if (l) return l }
  return 'en'
}

const COOKIE = 'lang'
export function readCookie(): Locale | null {
  const m = document.cookie.match(/(?:^|;\s*)lang=([^;]+)/)
  const v = m ? decodeURIComponent(m[1]) : null
  return isLocale(v) ? v : null
}
export function writeCookie(l: Locale | null) {
  document.cookie = l ? `${COOKIE}=${encodeURIComponent(l)}; path=/; max-age=31536000; samesite=lax`
    : `${COOKIE}=; path=/; max-age=0; samesite=lax`
}
