// Manager approval (docs/STAFF_ACCESS_DESIGN.md §7): texts shared by the
// approval dialog and the "approval code" setting.
import { extend } from './i18n'

extend({
  'ap.ask': ['请经理授权', 'Ask a manager'],
  'ap.title': ['经理授权', 'Manager approval'],
  'ap.for': ['要做的事：{what}', 'For: {what}'],
  'ap.who': ['授权人', 'Approved by'],
  'ap.pin': ['6 位授权码', '6-digit approval code'],
  'ap.go': ['授权并执行', 'Approve and go ahead'],
  'ap.none': ['这家店还没有经理或店主设置授权码。请他们在商家后台侧栏底部的“授权码”里设置。', 'No manager or owner of this store has set an approval code yet. They can set one under “Approval code” at the bottom of the back-office menu.'],
  'ap.bad': ['授权码不对，还能试 {n} 次', 'Wrong code. {n} tries left.'],
  'ap.locked': ['输错太多次，{at} 以后再试，或换一个人授权', 'Too many wrong codes. Try again after {at}, or ask someone else.'],
  'ap.done': ['{name}已授权', 'Approved by {name}'],
  'ap.used': ['授权已用过或过期，请重新授权', 'The approval was used or has expired. Ask again.'],
  'ap.my': ['授权码', 'Approval code'],
  'ap.my_hint': ['接单员遇到没权限的操作（比如取消已接的单）时，你在他的电脑上输这个码就能替他授权，只对那一次有效。和登录密码分开，输错 5 次锁 15 分钟。', 'When an order taker needs something their role may not do (such as cancelling an accepted order), you enter this code on their screen to approve that one action. It is separate from your password; 5 wrong tries lock it for 15 minutes.'],
  'ap.is_set': ['已设置', 'Set'],
  'ap.not_set': ['未设置', 'Not set'],
  'ap.new_pin': ['新的 6 位授权码', 'New 6-digit code'],
  'ap.password': ['登录密码（确认是你本人）', 'Your sign-in password (to confirm it is you)'],
  'ap.save': ['保存授权码', 'Save code'],
  'ap.saved': ['授权码已保存', 'Approval code saved'],
  'ap.cancel': ['取消', 'Cancel'],
})
