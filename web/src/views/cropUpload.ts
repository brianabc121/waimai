// The picture fields of the back office crop in the browser and store the
// result: the chosen part is drawn at the stored size and sent as a JPEG.
import { upload } from '../api'
import { t } from '../i18n'
import type { CropResult } from './MediaCropper.vue'

// src is the picked file's object URL; base is the API prefix of the branch;
// ratio is width / height; width is the stored width in pixels. Answers the picture's link.
export async function cropUpload(src: string, r: CropResult, base: string, ratio: number, width: number): Promise<string> {
  const img = new Image()
  img.src = src
  await img.decode()
  const sw = r.crop.w * img.naturalWidth, sh = r.crop.h * img.naturalHeight
  const c = document.createElement('canvas')
  c.width = Math.round(Math.min(width, sw)); c.height = Math.round(c.width / ratio)
  const g = c.getContext('2d')!
  g.fillStyle = '#fff'; g.fillRect(0, 0, c.width, c.height)
  g.imageSmoothingQuality = 'high'
  g.drawImage(img, r.crop.x * img.naturalWidth, r.crop.y * img.naturalHeight, sw, sh, 0, 0, c.width, c.height)
  const blob = await new Promise<Blob | null>(done => c.toBlob(done, 'image/jpeg', 0.85))
  if (!blob) throw new Error(t('pm.bad_type'))
  const { url } = await upload<{ url: string }>(`${base}/images`, blob)
  return url
}

export const PICTURE_TYPES = /^image\/(jpeg|png|webp|gif)$/
