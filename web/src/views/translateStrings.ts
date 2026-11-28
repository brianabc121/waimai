// Texts of the table of all translations (AllTranslations.vue) and what the
// language window shares with it, [zh, en].
import { extend } from '../i18n'

extend({
  'mtr.title': ['全部译文', 'All translations'],
  'mtr.menu_lang_confirm': ['改了原文语言，整份商品目录会按新语言重新翻译。继续吗？', 'Changing the catalog language translates the whole catalog again. Go on?'],
  'mtr.retranslate': ['全部重新机器翻译', 'Translate all again'],
  'mtr.retranslate_confirm': ['重新翻译所有机器翻译的内容？你自己改过的不会被覆盖。', 'Translate everything the machine translated again? What you changed yourself stays.'],
  'mtr.no_provider': ['平台还没设置翻译服务：繁体中文照样自动转换，其他语言暂时显示英文或原文。', 'The platform has no translation service yet: Traditional Chinese is still converted; other languages show English or the original for now.'],
  'mtr.retry': ['重试', 'Retry'],
  'mtr.pending': ['正在翻译，几秒后自动刷新…', 'Translating; this refreshes in a few seconds…'],
  'mtr.columns': ['语言：', 'Languages:'],
  'mtr.lang_on_hint': ['这种语言已关闭，顾客看英文。点击打开', 'Off: customers get English. Turn on'],
  'mtr.lang_off_hint': ['点击关闭这种语言，顾客改看英文', 'Turn off: customers get English instead'],
  'mtr.k_item': ['商品名', 'Item names'], 'mtr.k_category': ['分类', 'Categories'], 'mtr.k_option': ['规格', 'Options'], 'mtr.k_note': ['口味模板', 'Flavour notes'],
  'mtr.k_description': ['描述', 'Descriptions'], 'mtr.k_details': ['详情', 'Details'], 'mtr.k_promotion': ['优惠活动', 'Offers'], 'mtr.k_store': ['店铺信息', 'Store details'],
  'mtr.original': ['原文', 'Original'], 'mtr.save': ['保存', 'Save'], 'mtr.revert': ['恢复机器翻译', 'Back to machine translation'],
  'mtr.empty': ['没有内容', 'Nothing here'],
  'mtr.store_name': ['店名', 'Store name'],
})
