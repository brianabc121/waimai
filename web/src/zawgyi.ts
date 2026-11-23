// Burmese typed in the old Zawgyi font encoding (docs/I18N_DESIGN.md §12).
// The system keeps Burmese in Unicode only; Zawgyi text looks right on the
// writer's phone but turns into nonsense for everyone else. These are spots
// Unicode Burmese never has but Zawgyi typing always does.
const burmese = /[က-႟]/
// Unicode puts the vowel sign E (ေ) and the medials after their consonant;
// Zawgyi types ေ and the ya-yit (U+103B there) first, at the start of a syllable.
const leading = /(^|[^က-ဿ၌-႟])[ေျ]/
// Zawgyi's own letters for stacked and reshaped forms (U+1060 to U+1097 are
// Mon, Shan and Karen letters in Unicode, not used in Burmese).
const shapes = /[ၠ-႗]/
// Zawgyi's killer is U+1039, which in Unicode only stacks a consonant on another.
const killer = /္(?![က-အ])/

export function looksZawgyi(s: string): boolean {
  if (!burmese.test(s)) return false
  // U+103E (ha-htoe) is Unicode only; Shan written in Unicode uses the "shapes" range
  return leading.test(s) || killer.test(s) || (shapes.test(s) && !/\u103E/.test(s))
}

// zawgyiIn lists the texts of a request body that look like Zawgyi.
export function zawgyiIn(body: unknown, out: string[] = []): string[] {
  if (typeof body === 'string') { if (looksZawgyi(body)) out.push(body) }
  else if (Array.isArray(body)) body.forEach(x => zawgyiIn(x, out))
  else if (body && typeof body === 'object') Object.values(body).forEach(x => zawgyiIn(x, out))
  return out
}

// checkZawgyi asks before a merchant or the platform saves Burmese that looks
// like Zawgyi. It returns false to stop the save.
export function checkZawgyi(path: string, body: unknown, ask: (found: string) => boolean): boolean {
  if (!/^\/(merchant|admin)\//.test(path)) return true
  const found = zawgyiIn(body)
  return !found.length || ask(found[0])
}
