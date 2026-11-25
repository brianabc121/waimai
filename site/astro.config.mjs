// The company website (docs/WEBSITE_DESIGN.md): plain static pages, one folder
// per language. SITE_URL is the website's own address (the main domain), used
// for canonical links, hreflang and the sitemap.
import { defineConfig } from 'astro/config'

export default defineConfig({
  site: process.env.SITE_URL || 'https://example.com',
  trailingSlash: 'always',
  build: { format: 'directory', inlineStylesheets: 'auto' },
  compressHTML: true,
  server: { port: 4321 },
  vite: {
    // the form posts to the API; in development pass it on to a running system
    server: { proxy: { '/api': process.env.API_URL || 'http://localhost:8080' }, fs: { allow: ['..'] } },
  },
})
