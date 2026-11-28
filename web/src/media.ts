// Promotional media of the store page: how a crop is shown, and the texts.
import { extend } from './i18n'
import type { Crop } from './types'

// Every medium is shown in a frame of this shape (width / height).
export const MEDIA_RATIO = 16 / 9

// cropStyle places a picture or video inside its frame so that only the
// cropped part shows. The file itself is never cut.
export function cropStyle(c: Crop) {
  return { width: 100 / c.w + '%', height: 100 / c.h + '%', left: -c.x / c.w * 100 + '%', top: -c.y / c.h * 100 + '%' }
}

// largestCrop is the biggest part of a w × h picture that has the frame's shape, centred.
export function largestCrop(w: number, h: number, ratio = MEDIA_RATIO): Crop {
  const a = w / h
  const cw = a > ratio ? ratio / a : 1, ch = a > ratio ? 1 : a / ratio
  return { x: (1 - cw) / 2, y: (1 - ch) / 2, w: cw, h: ch }
}

export const clock = (s: number) => `${Math.floor(s / 60)}:${String(Math.floor(s % 60)).padStart(2, '0')}`

extend({
  'pm.video': ['门店视频', 'Video'], 'pm.image': ['图片', 'Picture'], 'pm.gif': ['动图', 'GIF'],
  'pm.play': ['播放', 'Play'], 'pm.pause': ['暂停', 'Pause'], 'pm.prev': ['上一个', 'Previous'], 'pm.next': ['下一个', 'Next'],
  'pm.sound_on': ['打开声音', 'Sound on'], 'pm.sound_off': ['静音', 'Mute'],
  'pm.hint': ['显示在外卖点餐页的最上方，可以放图片、动图和短视频，顾客左右滑动查看。一个都不放时，点餐页不显示这一块。',
    'Shown at the top of the ordering page: pictures, GIFs and short videos that customers swipe through. With none, the page has no such area.'],
  'pm.upload': ['上传图片或视频', 'Upload a picture or video'], 'pm.limits': ['图片 JPG / PNG / WebP，动图 GIF 最大 8 MB，视频 MP4 / MOV / WebM 最大 20 MB。最多 {n} 个。',
    'Pictures: JPG, PNG, WebP. GIFs up to 8 MB. Videos: MP4, MOV, WebM up to 20 MB. At most {n}.'],
  'pm.none': ['还没有宣传物料，点餐页现在不显示宣传区。', 'No promo media yet; the ordering page shows no promo area.'],
  'pm.shown': ['显示中', 'Shown'], 'pm.hidden': ['已隐藏', 'Hidden'], 'pm.show': ['显示', 'Show'], 'pm.hide': ['隐藏', 'Hide'],
  'pm.crop': ['裁剪', 'Crop'], 'pm.up': ['上移', 'Move up'], 'pm.down': ['下移', 'Move down'],
  'pm.delete_confirm': ['确定删除这个宣传物料？', 'Delete this promo medium?'],
  'pm.preview': ['看点餐页效果', 'See the ordering page'],
  'pm.crop_title': ['裁剪成点餐页的尺寸（16:9）', 'Crop to the ordering page (16:9)'],
  'pm.crop_hint': ['拖动画面调整位置，用滑块或滚轮缩放。框里的部分就是顾客看到的。', 'Drag to move, zoom with the slider or the wheel. Customers see what is in the frame.'],
  'pm.zoom': ['缩放', 'Zoom'], 'pm.title': ['标题（可不填）', 'Title (optional)'], 'pm.title_ph': ['例如：招牌盐酥鸡', 'For example: our popcorn chicken'],
  'pm.trim': ['播放片段', 'Part to play'], 'pm.from_here': ['从这里开始', 'Start here'], 'pm.to_here': ['到这里结束', 'End here'],
  'pm.whole': ['播放整段', 'Play it all'], 'pm.cover_here': ['用这一帧做封面', 'Use this frame as the cover'], 'pm.cover_at': ['封面 {t}', 'Cover {t}'],
  'pm.gif_note': ['动图按原文件播放，只调整显示范围。', 'A GIF plays as it is; only the visible part changes.'],
  'pm.too_big': ['文件太大：{kind}最大 {n} MB。', 'The file is too large: {kind} up to {n} MB.'],
  'pm.bad_type': ['不支持这种文件，请上传 JPG、PNG、WebP、GIF 图片或 MP4、MOV、WebM 视频。', 'This kind of file is not supported. Upload JPG, PNG, WebP or GIF pictures, or MP4, MOV or WebM videos.'],
  'pm.cant_play': ['这个浏览器播放不了这个视频，请先转成 MP4（H.264）再上传。', 'This browser cannot play the video. Convert it to MP4 (H.264) first.'],
  'pm.full': ['最多 {n} 个，请先删除一个。', 'At most {n}. Delete one first.'],
  'pm.uploading': ['正在上传…', 'Uploading…'],
  'im.upload': ['上传图片', 'Upload a picture'], 'im.change': ['换一张', 'Replace'], 'im.remove': ['移除', 'Remove'],
  'im.crop_title': ['裁剪图片', 'Crop the picture'], 'im.or_link': ['或者填图片链接', 'or paste a picture link'],
  'im.dish': ['商品图片', 'Item picture'], 'im.dish_hint': ['正方形，上传时裁剪', 'Square; cropped when uploaded'],
  'im.more': ['更多商品图', 'More pictures'], 'im.more_hint': ['顾客点开商品时左右滑动查看', 'Customers swipe through them on the item page'],
  'it.channels': ['供应方式', 'Offered for'], 'it.delivery': ['外卖配送', 'Delivery'], 'it.pickup': ['到店自取', 'Pickup'],
  'it.no_channel': ['都不勾选时，顾客在哪里都点不到这件商品', 'With none ticked, customers cannot order this item anywhere'],
  'im.earlier': ['往前移', 'Move earlier'], 'im.later': ['往后移', 'Move later'],
  'im.details': ['商品详情', 'Details'], 'im.details_ph': ['如主料、份量、做法、过敏原，顾客点开商品时看到', 'Ingredients, portion, how it is made, allergens: shown on the item page'],
  'im.logo': ['店铺头像', 'Store logo'], 'im.cover': ['封面图', 'Cover picture'], 'im.cover_hint': ['16:9，没有宣传物料时显示在点餐页顶部', '16:9; shown at the top of the ordering page when there is no promo media'],
})
