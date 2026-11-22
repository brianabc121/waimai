// A photo taken on a phone is several megabytes; it is shrunk to about
// 1280 px on the device before it is sent.
export function shrink(file: File): Promise<Blob> {
  return new Promise((resolve, reject) => {
    const img = new Image(), url = URL.createObjectURL(file)
    img.onload = () => {
      const k = Math.min(1, 1280 / Math.max(img.width, img.height))
      const c = document.createElement('canvas')
      c.width = Math.round(img.width * k); c.height = Math.round(img.height * k)
      c.getContext('2d')!.drawImage(img, 0, 0, c.width, c.height)
      URL.revokeObjectURL(url)
      c.toBlob(b => b ? resolve(b) : reject(new Error('photo')), 'image/jpeg', 0.7)
    }
    img.onerror = () => { URL.revokeObjectURL(url); reject(new Error('photo')) }
    img.src = url
  })
}
