// Texts of the back-office page kit (office.css) and the pages redrawn with
// it: subtitles under the page titles, number tiles, column headings.
import { extend } from './i18n'

extend({
  'ox.ops': ['操作', 'Actions'],
  'ox.range': ['第 {a}–{b} 条，共 {n} 条', '{a}–{b} of {n}'],
  // members
  'ox.sm.sub': ['管理本店会员资料、收货地址和消费记录', 'Your members: their details, addresses and orders'],
  'ox.sm.total': ['会员总数', 'Members'],
  'ox.sm.temp_s': ['1 小时内不补资料会自动回收', 'Freed after an hour without details'],
  'ox.sm.page_orders': ['本页消费次数', 'Orders (this page)'],
  'ox.sm.page_spent': ['本页消费金额', 'Spent (this page)'],
  'ox.sm.page_note': ['按当前筛选和排序', 'With the filters and order above'],
  // customer addresses
  'ox.da.sub': ['顾客收货地址、送餐排行和地图', 'Where your customers get their food: list, ranking and map'],
  // staff
  'ox.st.sub': ['员工账号、岗位、考勤和工作汇总', 'Staff accounts, posts, attendance and work summaries'],
  'ox.st.shown': ['显示 {n} 人', '{n} shown'],
  // riders
  'ox.rd.sub': ['骑手位置、在岗状态和手上的单', 'Where the riders are, who is on duty and what they carry'],
  'ox.rd.all': ['骑手', 'Riders'],
  'ox.rd.carrying': ['送餐中', 'Delivering'],
  'ox.rd.lost': ['位置异常', 'No position'],
})
