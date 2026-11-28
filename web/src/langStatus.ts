// Whether a store's translations keep failing, for the red dot on the
// "Languages" buttons and the settings card. Asked once per page and store,
// and again after anything that may have changed it.
import { reactive } from 'vue'
import { get } from './api'

export interface LangsMeta {
  menu_lang: string; menu_langs_off: string[]; provider: string; pending: boolean
  error: string; error_at: string | null; default_lang: string; updated_at: string | null
}
interface Status { failed: boolean; meta: LangsMeta | null; at: number }

const cache = new Map<string, Status>()

export function langStatus(base: string): Status {
  let s = cache.get(base)
  if (!s) {
    s = reactive({ failed: false, meta: null, at: 0 })
    cache.set(base, s)
  }
  if (Date.now() - s.at > 60_000) {
    s.at = Date.now()
    void refreshLangStatus(base)
  }
  return s
}

export async function refreshLangStatus(base: string): Promise<LangsMeta | null> {
  const s = cache.get(base) ?? langStatus(base)
  try {
    const m = await get<LangsMeta>(`${base}/translations?scope=none`)
    noteLangStatus(base, m)
    return m
  } catch { return s.meta }
}

// noteLangStatus takes what a translations answer says about the store.
export function noteLangStatus(base: string, m: LangsMeta) {
  const s = cache.get(base) ?? langStatus(base)
  s.meta = m
  s.failed = !!m.error
  s.at = Date.now()
}
