// On a phone a data table is shown as a stack of cards, one per row (see
// "tables on a phone" in styles.css). A card has no header row, so each cell
// carries its column's heading in data-label. This keeps those labels in step
// with whatever tables are on the page, in either language.
function label(table: HTMLTableElement) {
  const heads = Array.from(table.tHead?.rows[0]?.cells ?? [])
  if (!heads.length || heads.some(h => h.colSpan > 1)) return
  const names = heads.map(h => (h.textContent ?? '').trim())
  for (const body of Array.from(table.tBodies)) {
    for (const row of Array.from(body.rows)) {
      if (row.cells.length !== names.length) continue
      for (let i = 0; i < names.length; i++) {
        if (row.cells[i].dataset.label !== names[i]) row.cells[i].dataset.label = names[i]
      }
    }
  }
}

export function labelTables(root: HTMLElement) {
  let queued = false
  const run = () => { queued = false; root.querySelectorAll<HTMLTableElement>('table.data').forEach(label) }
  // Attribute changes are not watched, so writing the labels does not call this again.
  new MutationObserver(() => { if (!queued) { queued = true; requestAnimationFrame(run) } })
    .observe(root, { childList: true, subtree: true, characterData: true })
  run()
}
