// Every page in every language, each listing its other languages (hreflang).
import type { APIRoute } from 'astro'
import { LANGS, TAG, DEFAULT_LANG, href } from '../i18n'

export const PAGES = ['', 'features', 'features/online-ordering', 'features/dine-in', 'pricing', 'demo', 'about', 'privacy', 'terms']

export const GET: APIRoute = ({ site }) => {
  const u = (l: string, p: string) => new URL(href(l as never, p), site).href
  const urls = PAGES.flatMap(p => LANGS.map(l => `  <url><loc>${u(l, p)}</loc>
${LANGS.map(x => `    <xhtml:link rel="alternate" hreflang="${TAG[x]}" href="${u(x, p)}"/>`).join('\n')}
    <xhtml:link rel="alternate" hreflang="x-default" href="${u(DEFAULT_LANG, p)}"/>
  </url>`))
  return new Response(`<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9" xmlns:xhtml="http://www.w3.org/1999/xhtml">
${urls.join('\n')}
</urlset>
`, { headers: { 'Content-Type': 'application/xml' } })
}
