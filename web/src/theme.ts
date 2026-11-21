import type { Template } from './types'

// themeVars turns a template's colour into the CSS variables the customer
// pages are painted with. Set on a page's root element, it themes everything
// inside, including sheets.
export function themeVars(tpl?: Template | null): Record<string, string> {
  const hex = tpl?.theme_color ?? ''
  if (!/^#[0-9a-f]{6}$/i.test(hex)) return {}
  const [r, g, b] = [1, 3, 5].map(i => parseInt(hex.slice(i, i + 2), 16))
  const mix = (c: number, to: number, w: number) => Math.round(c + (to - c) * w)
  const rgb = (to: number, w: number) => `rgb(${mix(r, to, w)}, ${mix(g, to, w)}, ${mix(b, to, w)})`
  return { '--brand': hex, '--brand-dark': rgb(0, 0.18), '--brand-soft': rgb(255, 0.9) }
}

export const defaultTemplate = (channel: string): Template =>
  ({ layout: channel === 'ipad' ? 'grid' : 'list', theme_color: '#d9480f', logo_url: '', cover_url: '' })
