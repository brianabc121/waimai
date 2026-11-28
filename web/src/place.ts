// A delivery address that is a place known by name (docs/ADDRESS_PLACE_DESIGN.md).
// address_line is the whole address, "name, street address"; place_name says
// which leading part of it is the name. Every screen shows it the same way:
// the name and the unit first (what the rider looks for), the street after.
export interface Addressed { address_line: string; address_unit?: string; place_name?: string }

const leads = (line: string, name: string) => !!name && line.trim().toLowerCase().startsWith(name.trim().toLowerCase())

export function addrParts(a: Addressed): { name: string; unit: string; street: string } {
  const line = (a.address_line ?? '').trim(), unit = (a.address_unit ?? '').trim(), name = (a.place_name ?? '').trim()
  if (!leads(line, name)) return { name: line, unit, street: '' }
  return { name: line.slice(0, name.length), unit, street: line.slice(name.length).replace(/^[\s,，、;；]+/, '') }
}
// addrHead is "name · unit"; addrText is the whole address on one line.
export function addrHead(a: Addressed): string {
  const p = addrParts(a)
  return [p.name, p.unit].filter(Boolean).join(' · ')
}
export function addrText(a: Addressed, sep = ', '): string {
  return [addrHead(a), addrParts(a).street].filter(Boolean).join(sep)
}
// keepsName: the typed line still starts with the place's name.
export const keepsName = leads
