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
  // tables
  'ox.tb.sub': ['桌台状态、桌码和最近结账的单', 'Tables at a glance, their codes and the latest bills'],
  'ox.tb.all': ['桌台', 'Tables'],
  'ox.tb.open_total': ['在用餐的消费', 'Open bills'],
  'ox.tb.all_areas': ['全部区域', 'All areas'],
  'ox.tb.no_area': ['未分区', 'No area'],
  'ox.tb.all_states': ['全部状态', 'Any state'],
  'ox.tb.none': ['这里没有符合条件的桌台', 'No table matches'],
  'ox.tb.tip': ['点空桌开台点菜，点用餐中的桌看账单；“桌码”里可打印二维码、绑定平板。', 'Tap a free table to seat guests, a busy one to see its bill; “Table code” prints the QR code and pairs tablets.'],
  // kitchen
  'ox.kt.sub': ['按工位做菜、打包出餐、堂食上菜', 'Cook by station, pack and hand over, serve to the tables'],
  'kt.never': ['订单没取到，不是没有单。请检查网络后重试', 'Could not load the orders. This does not mean there are none: check the network and retry'],
  'kt.stale': ['下面是 {t} 的旧内容，可能有新单没显示', 'Showing what was loaded at {t}; new orders may be missing'],
  'kt.failing': ['刚才刷新失败，正在重试', 'The last refresh failed, trying again'],
  'kt.retry': ['重试', 'Retry'],
  'ox.kt.stations': ['制作台', 'Stations'],
  'ox.kt.tip': ['只看自己的工位；菜可以改到别的工位。', 'Show your own station; a dish can be moved to another one.'],
  // messages
  'ox.ch.sub': ['和顾客的聊天，店里所有人共用一个收件箱', 'Chats with customers, one inbox for the whole store'],
  'ox.ch.unread': ['{n} 条未读', '{n} unread'],
  // platform support
  'ox.sp.sub': ['有问题直接问平台，店长和前台共用这个会话', 'Ask the platform; managers and the desk share this conversation'],
  'ox.sp.shared': ['店长和前台共用', 'Shared by managers and the desk'],
  'ox.sp.can': ['平台客服可以帮你', 'Support can help with'],
  'ox.sp.k.account': ['账号、员工登录和权限', 'Accounts, staff sign-in and permissions'],
  'ox.sp.k.money': ['结算、对账和账单', 'Payouts, reconciling and bills'],
  'ox.sp.k.menu': ['菜品上架、店铺资料', 'Listings and store details'],
  'ox.sp.k.other': ['投诉和其他问题', 'Complaints and anything else'],
  'ox.sp.tip_h': ['小提示', 'Tip'],
  'ox.sp.tip': ['问订单的事时，点输入框旁的订单按钮把订单发过来，客服能直接看到。', 'Asking about an order? Send it with the order button by the input box so support sees it right away.'],
  // promo media
  'ox.pm.sub': ['点餐页最上方的图片、动图和短视频', 'Pictures, GIFs and short videos at the top of the ordering page'],
  'ox.pm.total': ['已上传', 'Uploaded'],
  'ox.pm.left': ['还能上传', 'Room for'],
  'ox.pm.kinds': ['类型', 'Kinds'],
  'ox.pm.all': ['全部物料', 'All media'],
  'ox.pm.order': ['按顾客看到的顺序排列', 'In the order customers see them'],
  'ox.pm.no_match': ['没有符合条件的物料', 'No media match'],
  // promotions
  'ox.pr.sub': ['外卖和堂食活动、优惠券，发布前可以试算', 'Delivery and dine-in deals and coupons; try them before publishing'],
  'ox.pr.state': ['状态', 'State'],
  'ox.pr.all': ['全部活动', 'All deals'],
  // settings
  'ox.set.sub': ['门店资料、费用、接单出餐、付款方式和商城展示', 'Store details, fees, order timing, payments and the marketplace'],
  'ox.set.open_store': ['打开顾客点餐页', 'Open the ordering page'],
  'ox.set.sections': ['设置项', 'Sections'],
  'ox.set.basic': ['基本信息', 'Basics'],
  'ox.set.fees': ['税费和起送', 'Tax, fees and minimum'],
  'ox.set.timing': ['接单和出餐', 'Accepting and cooking'],
  'ox.set.switches': ['服务开关', 'Services'],
  'ox.set.pay': ['付款方式', 'Payments'],
  'ox.set.texts': ['店铺多语言', 'Store texts'],
  // rider cash (核款)
  'ox.cs.sub': ['骑手代收的现金，交回店里后在这里核款', 'Cash riders collected; check it in here when they hand it back'],
  'ox.cs.all': ['全部记录', 'All'],
  'ox.cs.pending_due': ['骑手手上 {m}', 'Riders hold {m}'],
  'ox.cs.listed': ['最近 {n} 笔', 'Last {n}'],
  'ox.cs.tip': ['填骑手实际交回的金额，再点“确认核款”；和代收不一致会记为有差异。', 'Enter what the rider handed back and confirm; a different amount is marked as a discrepancy.'],
  // bills, reconciling
  'ox.bl.sub': ['外卖和自取订单的金额明细、优惠分摊和平台结算', 'Delivery and pickup orders: amounts, who paid for each discount, platform payouts'],
  'ox.rc.sub': ['按付款方式核对收款，核对在线支付，查看打折和抹零', 'Check takings by payment method, tick off online payments, review discounts'],
})
