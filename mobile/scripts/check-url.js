// A release build is what staff install and keep: it must point at the real
// server over https. A debug build may point anywhere, or nowhere.
//   node scripts/check-url.js release|debug   (reads WORKBENCH_URL)
function problem(kind, raw) {
  if (kind !== 'release') return ''
  const value = (raw || '').trim()
  if (!value) return 'WORKBENCH_URL is not set: the app would open to an empty page'
  let u
  try {
    u = new URL(value)
  } catch {
    return 'WORKBENCH_URL is not an address: ' + value
  }
  if (u.protocol !== 'https:') return 'WORKBENCH_URL must start with https:// for a release build, got ' + u.protocol + '//'
  if (!u.hostname || u.username || u.password) return 'WORKBENCH_URL must be a plain https address: ' + value
  return ''
}
module.exports = { problem }
if (require.main === module) {
  const msg = problem(process.argv[2] || 'debug', process.env.WORKBENCH_URL)
  if (msg) {
    console.error('::error::' + msg)
    process.exit(1)
  }
}
