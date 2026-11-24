// Dine-in and delivery promotions, discounts at checkout, payment types and
// the reconciliation page (docs/CHECKOUT_DISCOUNT_DESIGN.md).
import { extend } from './i18n'

extend({
  'ck.tab_delivery': ['外卖活动', 'Delivery offers'],
  'ck.tab_dine': ['堂食活动', 'Dine-in offers'],
  'ck.tab_coupons': ['优惠券（外卖）', 'Coupons (delivery)'],
  'ck.new_dine': ['新建堂食活动', 'New dine-in promotion'],
  'ck.delivery_hint': ['外卖活动只用于外卖和自取订单，不会减堂食账单。', 'Delivery offers apply to delivery and pickup orders only, never to dine-in bills.'],
  'ck.dine_hint': ['堂食活动只减堂食账单（店员开台、顾客扫码点菜），不会出现在外卖店铺页，外卖活动也不会减堂食账单。一张账单最多用一个活动，自动挑省得最多的；名额在结账时才扣。',
    'Dine-in offers only reduce table bills (opened by staff or ordered by QR code) and never show on the delivery store page; delivery offers never reduce a table bill. One offer per bill, the one that saves the most; its quota is used when the bill is paid.'],
  'ck.dine_time_hint': ['每天时段按开台时间算：开台时在时段内，结账晚了也照样减。', 'Daily hours count from when the table was opened: a table opened inside them keeps the offer even if it pays later.'],
  'ck.dine_offers': ['堂食优惠', 'Dine-in offers'],
})
