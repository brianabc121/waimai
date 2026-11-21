// The app is a shell around the workbench of ONE deployment. Set WORKBENCH_URL
// to that deployment's address when building, e.g. https://order.example.com
// (docs/DEPLOY.md, "安卓 App"). The pages, the sign-in cookie and every update
// come from the server; the shell adds push and background location.
const url = (process.env.WORKBENCH_URL || '').replace(/\/+$/, '')
// Push only works in a build that carries the Firebase file. The pages learn
// it from the browser name, and never ask Firebase for anything otherwise.
const firebase = require('node:fs').existsSync(require('node:path').join(__dirname, 'android/app/google-services.json'))

/** @type {import('@capacitor/cli').CapacitorConfig} */
const config = {
  appId: process.env.WORKBENCH_APP_ID || 'app.waimai.workbench',
  appName: process.env.WORKBENCH_APP_NAME || '员工工作台',
  webDir: 'www',
  appendUserAgent: 'WorkbenchApp/1' + (firebase ? ' fcm' : ''),
  // useLegacyBridge keeps position updates coming after five minutes in the background.
  android: { backgroundColor: '#ffffff', useLegacyBridge: true },
  plugins: {
    PushNotifications: { presentationOptions: ['badge', 'sound', 'alert'] },
  },
}
if (url) {
  config.server = {
    url: url + '/work',
    // Only http://10.0.2.2 style addresses for local testing need cleartext.
    cleartext: url.startsWith('http://'),
    errorPath: 'offline.html',
  }
}
module.exports = config
