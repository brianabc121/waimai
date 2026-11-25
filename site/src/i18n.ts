// The website's languages (docs/WEBSITE_DESIGN.md §7.2). brian's market is
// Chinese restaurants abroad, so Simplified Chinese comes first and is the
// default; Traditional Chinese is converted from it (npm run hant); English
// is for search engines and partners. More of the system's 12 languages
// (web/src/langs.ts) can be added later with a copy file each.
import { BRAND } from './brand'
import zh from './copy/zh'
import zhHant from './copy/zh-Hant'
import en from './copy/en'

export type Copy = typeof zh
export const LANGS = ['zh', 'zh-Hant', 'en'] as const
export type Lang = typeof LANGS[number]
export const DEFAULT_LANG: Lang = 'zh'
// BCP 47 tags for <html lang> and hreflang
export const TAG: Record<Lang, string> = { zh: 'zh-Hans', 'zh-Hant': 'zh-Hant', en: 'en' }

const COPY: Record<Lang, Copy> = { zh, 'zh-Hant': zhHant, en }

export const brandName = (l: Lang) => BRAND[l]
export const brandShort = (l: Lang) => BRAND[`${l}Short` as const]

// fill puts the brand and WeChat ID into a text.
export const fill = (l: Lang, s: string) =>
  s.replaceAll('{brand}', brandName(l)).replaceAll('{short}', brandShort(l)).replaceAll('{wechat}', BRAND.wechat)

export function copy(l: Lang): Copy {
  return COPY[l]
}

export const langPaths = () => LANGS.map(lang => ({ params: { lang } }))

// href builds a link inside the site: href('en', 'pricing') -> /en/pricing/
export const href = (l: Lang, path = '') => `/${l}/${path ? path.replace(/^\/|\/$/g, '') + '/' : ''}`
