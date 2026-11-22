// Inside the Android or iPhone app the same pages run in the app's own
// browser, and the app lends them what a browser tab cannot do: push through
// the phone's own service and the position while the screen is off.
// In an ordinary browser none of this is loaded.
const cap = (window as any).Capacitor
export const native: boolean = !!cap?.isNativePlatform?.()
export const platform: 'android' | 'ios' | 'web' = native ? cap.getPlatform() : 'web'
// Whether this build of the app was made with the Firebase file. Without it
// the phone's push service must not be touched at all.
export const appPush: boolean = native && /WorkbenchApp\/\d+ fcm/.test(navigator.userAgent)

const plugins: Record<string, Promise<any>> = {}
export function plugin<T = any>(name: string): Promise<T> {
  // A plugin answers any property, "then" included, so a promise handed the
  // plugin itself would try to wait on it and fail ("then() is not
  // implemented"). The cover below hides "then" and passes the rest through.
  return (plugins[name] ??= import('@capacitor/core').then(c => {
    const p = c.registerPlugin<any>(name)
    return new Proxy({}, { get: (_, k) => k === 'then' ? undefined : p[k] })
  }))
}

// nativePost sends a request through the phone instead of the page. Android
// slows a page's own requests down after a few minutes in the background;
// this keeps positions flowing. It answers false when the caller should fall
// back to an ordinary request.
export async function nativePost(path: string, body: unknown): Promise<boolean> {
  if (!native) return false
  try {
    const http = await plugin('CapacitorHttp')
    const r = await http.request({ url: location.origin + '/api/v1' + path, method: 'POST', data: body,
      headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'waimai-app' }, connectTimeout: 15000, readTimeout: 15000 })
    return r.status >= 200 && r.status < 300
  } catch { return false }
}
