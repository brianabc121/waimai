// Flavour note templates (docs/NOTE_TEMPLATE_DESIGN.md): helpers to name the
// chosen tags of a line, and the texts of every page that shows them, [zh, en].
import { extend, mn } from './i18n'
import type { Item, LineNoteTag, NoteGroup } from './types'

extend({
  // ordering
  'nt.title': ['口味备注', 'Flavour notes'], 'nt.free': ['不加钱，可以不选', 'No extra charge, all optional'],
  'nt.single': ['单选', 'Pick one'], 'nt.multi': ['可多选', 'Pick any'],
  'nt.choose': ['选口味', 'Choose'], 'nt.other_ph': ['其他要求，如：打包分开装', 'Anything else, e.g. pack separately'],
  // the store's template page
  'nt.tab_items': ['商品', 'Items'], 'nt.tab_templates': ['口味模板', 'Flavour notes'],
  'nt.new': ['新建模板', 'New template'], 'nt.starters': ['从常用模板添加', 'Add common templates'],
  'nt.empty': ['还没有口味模板', 'No flavour templates yet'],
  'nt.empty_hint': ['先从常用模板添加，再按店里的习惯改。', 'Start from the common ones and adjust them to your store.'],
  'nt.groups_tags': ['{g} 组 · {n} 个标签', '{g} groups · {n} tags'], 'nt.used_on': ['用在 {n} 件商品', 'On {n} items'],
  'nt.why_title': ['口味模板和规格有什么不同？', 'Flavour notes or options?'],
  'nt.why': ['要加钱或必须选的（大杯 / 中杯、加珍珠），在商品里设“规格”。不加钱、可选可不选的口味要求（少糖、去冰、不要葱），用口味模板：写一次，很多商品共用。',
    'Use options on the item for anything that costs extra or must be chosen (size, toppings). Use flavour notes for free, optional requests (less sugar, no ice, no onion): write them once and share them across items.'],
  'nt.name': ['模板名称', 'Template name'], 'nt.name_hint': ['只有店里看得到这个名字', 'Only your store sees this name'],
  'nt.group_ph': ['组名，可留空', 'Group name (optional)'],
  'nt.single_hint': ['顾客只能选其中一个', 'Customers pick one'], 'nt.multi_hint': ['顾客可以选几个', 'Customers pick any'],
  'nt.tag_ph': ['输入标签，回车添加', 'Type a tag, press Enter'],
  'nt.tag_tip': ['点标签可以停用或恢复；划线的是停用的，顾客暂时看不到。', 'Tap a tag to pause or resume it; struck-through tags are hidden from customers.'],
  'nt.add_group': ['加一组', 'Add group'], 'nt.preview': ['顾客看到的样子', 'What customers see'],
  'nt.apply': ['套用到商品（{n}）', 'Apply to items ({n})'],
  'nt.save_hint': ['保存后，{n} 道套用的商品马上跟着变；已下的订单不变。', 'Saving updates the {n} items using it at once; past orders stay as they were.'],
  'nt.delete_confirm': ['删除“{name}”？它用在 {n} 件商品，删除后这些商品不再显示它的口味；已下的订单不受影响。',
    'Delete "{name}"? It is on {n} items, which will stop showing its notes. Past orders are not affected.'],
  'nt.apply_title': ['把“{name}”套用到商品', 'Apply "{name}" to items'],
  'nt.apply_hint': ['勾上的商品会显示这个模板的口味；取消勾选就不再显示。', 'Ticked items show these notes; untick to remove them.'],
  'nt.search': ['搜商品名或编号', 'Search name or code'],
  'nt.cat_count': ['{k} / {n} 件', '{k} of {n}'], 'nt.using': ['已套用：{names}', 'Using: {names}'],
  'nt.total': ['共 {n} 件商品', '{n} items'],
  'nt.full': ['这些商品已经有 3 个口味模板：{names}', 'These items already have 3 templates: {names}'],
  'nt.starters_hint': ['勾上的模板加进店里，加进来以后可以随便改。', 'Ticked templates are added to your store; you can change them afterwards.'],
  'nt.add_n': ['添加 {n} 个模板', 'Add {n} templates'], 'nt.added': ['已添加', 'Added'],
  'nt.form_hint': ['不加钱、可选可不选的口味要求，最多选 3 个。要加钱或必须选的，用下面的“规格”。',
    'Free, optional requests; up to 3. Use the options below for anything that costs extra or must be chosen.'],
  'nt.manage': ['管理口味模板', 'Manage templates'], 'nt.none_yet': ['店里还没有口味模板。', 'Your store has no flavour templates yet.'],
  'nt.on': ['口味：{names}', 'Notes: {names}'], 'nt.taken': ['已经有同名的模板', 'A template with this name exists'],
  'nt.tag_twice': ['同一组里有重复的标签：{name}', 'The tag "{name}" is twice in a group'],
  'nt.need_tag': ['每组至少要有一个标签', 'Every group needs at least one tag'],
  // ordering for a customer
  'nt.done': ['完成', 'Done'], 'nt.enter_note': ['按回车，把“{s}”记成备注', 'Press Enter to keep "{s}" as a note'],
})

// The chosen tags of an order line, for the customer (their language) or the
// store (the menu's own words).
export function tagNames(tags: LineNoteTag[] | undefined, customer = false): string[] {
  return (tags ?? []).map(x => customer ? (x.name_tr || x.name) : x.name)
}

// noteLine is "少糖 · 去冰 · 珍珠多一点": the tags first, the customer's own words after.
export function noteLine(it: { note?: string; note_tags?: LineNoteTag[] }, customer = false): string {
  return [...tagNames(it.note_tags, customer), it.note?.trim() ?? ''].filter(Boolean).join(' · ')
}

// cartTags names the tags of a cart line from the dish as it is on the menu now.
export function cartTags(item: Item | undefined, ids: number[] | undefined): string[] {
  if (!item || !ids?.length) return []
  const out: string[] = []
  for (const g of item.notes ?? []) for (const x of g.tags) if (ids.includes(x.id)) out.push(mn(x))
  return out
}

export const hasNotes = (it: Item) => (it.notes?.length ?? 0) > 0

// toggleTag picks or drops a tag; in a pick-one group it replaces the other.
export function toggleTag(chosen: number[], g: NoteGroup, id: number): number[] {
  if (chosen.includes(id)) return chosen.filter(x => x !== id)
  const others = g.multi ? chosen : chosen.filter(x => !g.tags.some(t => t.id === x))
  return [...others, id]
}
