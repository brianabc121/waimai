// Everything about who we are that the pages print, in one place.
// docs/WEBSITE_DESIGN.md §12: brian chose the name, the domains and WeChat.
export const BRAND = {
  zh: '河马点餐系统',
  zhShort: '河马点餐',
  'zh-Hant': '河馬點餐系統',
  'zh-HantShort': '河馬點餐',
  // An English name has not been chosen yet; this one is a stand-in.
  en: 'Hippo Order',
  enShort: 'Hippo Order',
  wechat: 'abcfood123',
  // The ordering system itself (sign-in for stores and staff, the marketplace
  // for customers): the order. subdomain. Set ORDER_URL when building.
  orderUrl: (import.meta.env.ORDER_URL as string | undefined) || 'https://order.example.com',
}
