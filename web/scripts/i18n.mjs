// Texts in languages other than Chinese and English (docs/I18N_DESIGN.md §8.3).
//
// The Chinese and English texts live in the code, as [zh, en] pairs in
// i18n.ts and the files that extend() it. Every other language has
// src/locales/<code>.json: key -> text. This script
//
//   node scripts/i18n.mjs source         prints every key with its Chinese and English (JSON)
//   node scripts/i18n.mjs zh-Hant        writes zh-Hant.json, converted from the Chinese (OpenCC)
//   node scripts/i18n.mjs check          lists missing keys, stray keys and broken {placeholders}
//
// The platform back office (src/views/admin) speaks Chinese and English only,
// so its texts are left out.
import { readFileSync, writeFileSync, readdirSync, statSync } from 'node:fs'
import { join, relative } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = join(fileURLToPath(import.meta.url), '../../src')
export const LANGS = ['zh-Hant', 'th', 'vi', 'fr', 'ar', 'es', 'ru', 'ja', 'my', 'km']

function files(dir) {
  return readdirSync(dir).flatMap(n => {
    const p = join(dir, n)
    return statSync(p).isDirectory() ? files(p) : /\.(ts|vue)$/.test(n) ? [p] : []
  })
}

const lit = String.raw`(?:'(?:\\.|[^'\\])*'|"(?:\\.|[^"\\])*")`
const entry = new RegExp(String.raw`(?:'([^']+)'|([A-Za-z_]\w*))\s*:\s*\[\s*(${lit})\s*,\s*(${lit})\s*\]`, 'g')
const str = s => Function(`"use strict"; return (${s})`)()

// source: key -> { zh, en, file }, from i18n.ts and every file that extend()s it.
export function source() {
  const out = {}
  for (const f of files(root)) {
    const rel = relative(root, f)
    if (rel.startsWith('views/admin') || rel.startsWith('locales')) continue
    const s = readFileSync(f, 'utf8')
    if (rel !== 'i18n.ts' && !s.includes('extend(')) continue
    const body = rel === 'i18n.ts' ? s.slice(s.indexOf('const dict'), s.indexOf('export function extend')) : s
    for (const m of body.matchAll(entry)) out[m[1] ?? m[2]] = { zh: str(m[3]), en: str(m[4]), file: rel }
  }
  return out
}

const holes = s => [...String(s).matchAll(/\{(\w+)(?:, plural,)?/g)].map(m => m[1]).filter((v, i, a) => a.indexOf(v) === i).sort().join(',')

export function check() {
  const src = source()
  const problems = []
  for (const l of LANGS) {
    let tr = {}
    try { tr = JSON.parse(readFileSync(join(root, 'locales', l + '.json'), 'utf8')) } catch { problems.push(`${l}: no file`); continue }
    for (const k in src) {
      if (!(k in tr)) { problems.push(`${l}: missing ${k}`); continue }
      const want = holes(src[k].en), got = holes(tr[k])
      if (want !== got) problems.push(`${l}: ${k} has {${got}} but the English has {${want}}`)
    }
    for (const k in tr) if (!(k in src)) problems.push(`${l}: stray key ${k}`)
  }
  return problems
}

async function zhHant() {
  const OpenCC = await import('opencc-js')
  const conv = OpenCC.Converter({ from: 'cn', to: 'twp' })
  const out = {}
  for (const [k, v] of Object.entries(source())) out[k] = conv(v.zh)
  writeFileSync(join(root, 'locales', 'zh-Hant.json'), JSON.stringify(out, null, 0).replace(/","/g, '",\n"').replace(/^\{/, '{\n').replace(/\}$/, '\n}\n'))
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const cmd = process.argv[2]
  if (cmd === 'source') console.log(JSON.stringify(source(), null, 1))
  else if (cmd === 'zh-Hant') await zhHant()
  else if (cmd === 'check') {
    const p = check()
    console.log(p.length ? p.join('\n') : 'all languages complete')
    process.exitCode = p.length ? 1 : 0
  } else console.log('usage: node scripts/i18n.mjs source|zh-Hant|check')
}
