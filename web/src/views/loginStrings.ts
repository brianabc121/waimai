// Texts of the sign-in pages (docs/STAFF_ACCESS_DESIGN.md §5).
import { extend } from '../i18n'

extend({
  'lg.office': ['商家后台', 'Back office'],
  'lg.work': ['员工工作台', 'Staff workbench'],
  'lg.hint': ['店主、经理、接单员用商家后台；后厨、打包、骑手用员工工作台', 'Owners, managers and order takers use the back office; kitchen, packing and riders use the workbench.'],
  'lg.staff_only': ['你的账号是普通员工，只能使用员工工作台', 'Your account is a staff account and can only use the staff workbench.'],
  'lg.go_work': ['进入员工工作台', 'Open the staff workbench'],
  'lg.pick': ['选择身份', 'Choose where to work'],
  'lg.pick_hint': ['选一家门店和角色进入，之后可以在侧栏的“切换身份”里换', 'Pick a store and role. You can change it later with “Switch identity” in the menu.'],
  'lg.work_hint': ['后厨、打包、配送', 'Kitchen, packing and deliveries'],
  'lg.last': ['上次使用', 'Last used'],
  'lg.again': ['从员工工作台进入商家后台，请再输一次密码', 'To open the back office from the workbench, enter your password again.'],
  'lg.enter': ['进入', 'Open'],
  'lg.bad_password': ['密码不对', 'Wrong password'],
  'lg.switch': ['切换身份', 'Switch identity'],
  'lg.admin_title': ['平台管理员后台', 'Platform admin'],
  'lg.admin_hint': ['仅限平台管理员登录', 'For platform administrators only'],
})
