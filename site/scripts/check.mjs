// After the build: every internal link points at a page that exists, and every
// page has a title and a description. Fails the build otherwise.
import { readFileSync, readdirSync, statSync, existsSync } from 'node:fs'
import { join, relative } from 'node:path'
import { fileURLToPath } from 'node:url'

const dist = fileURLToPath(new URL('../dist/', import.meta.url))
const pages = []
const walk = d => readdirSync(d).forEach(n => { const p = join(d, n); statSync(p).isDirectory() ? walk(p) : n.endsWith('.html') && pages.push(p) })
walk(dist)
const bad = []
for (const p of pages) {
  const html = readFileSync(p, 'utf8'), at = '/' + relative(dist, p)
  if (!/<title>[^<]+<\/title>/.test(html)) bad.push(`${at}: no title`)
  if (!at.endsWith('/index.html') || at === '/index.html' || at.startsWith('/404')) { /* root redirect and 404 */ }
  else if (!/<meta name="description" content="[^"]+"/.test(html)) bad.push(`${at}: no description`)
  for (const [, link] of html.matchAll(/(?:href|src)="(\/[^"#?]*)"/g)) {
    if (link.startsWith('/api/') || link.startsWith('//')) continue
    const file = link.endsWith('/') ? join(dist, link, 'index.html') : join(dist, link)
    if (!existsSync(file)) bad.push(`${at}: broken link ${link}`)
  }
}
if (bad.length) { console.error(bad.join('\n')); process.exit(1) }
console.log(`checked ${pages.length} pages: titles, descriptions and links are fine`)
