// Writes src/copy/zh-Hant.ts from src/copy/zh.ts with OpenCC (Simplified ->
// Traditional characters), like the system's zh-Hant texts (web/scripts/i18n.mjs).
// Character by character on purpose: the phrase tables turn 菜单 (a restaurant's
// menu) into 選單 (a software menu), even inside 点菜单 and 加菜单独. A few
// words are then set by hand in FIX. Run after changing zh.ts: npm run hant
import { writeFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import * as OpenCC from 'opencc-js'
import zh from '../src/copy/zh.ts'

const chars = OpenCC.Converter({ from: 'cn', to: 'tw' })
const FIX = [['登錄', '登入'], ['短信', '簡訊']]
const conv = s => FIX.reduce((t, [a, b]) => t.replaceAll(a, b), chars(s))
const walk = v => typeof v === 'string' ? conv(v) : Array.isArray(v) ? v.map(walk) : v && typeof v === 'object'
  ? Object.fromEntries(Object.entries(v).map(([k, x]) => [k, k === 'img' ? x : walk(x)])) : v
const out = { ...walk(zh), langName: '繁體中文' }
const file = fileURLToPath(new URL('../src/copy/zh-Hant.ts', import.meta.url))
writeFileSync(file, `// 繁體中文。由 scripts/hant.mjs 從 zh.ts 自動轉換，不要手改：改 zh.ts 後執行 npm run hant。
import type { Copy } from '../i18n'

const zhHant: Copy = ${JSON.stringify(out, null, 2)}
export default zhHant
`)
console.log('wrote', file)
