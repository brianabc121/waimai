// Texts of the menu's languages page (MenuTranslations.vue), [zh, en].
import { extend } from '../i18n'

extend({
  'mtr.title': ['菜单多语言', 'Menu languages'], 'mtr.title_item': ['多语言 · {name}', 'Languages · {name}'],
  'mtr.open': ['多语言', 'Languages'], 'mtr.open_hint': ['查看和修改菜单的各种语言译文', 'See and correct the menu in other languages'],
  'mtr.menu_lang': ['菜单原文语言', 'Menu written in'],
  'mtr.menu_lang_hint': ['菜品、分类和规格用这种语言录入，系统自动翻译成其他语言。关掉的语言，顾客看英文。',
    'Write dishes, categories and options in this language; the system translates them. Customers reading a language that is off get English.'],
  'mtr.menu_lang_confirm': ['改了原文语言，整份菜单会按新语言重新翻译。继续吗？', 'Changing the menu language translates the whole menu again. Go on?'],
  'mtr.stats': ['机器翻译 {machine} · 商家已改 {human} · 待核对 {stale} · 缺 {missing}', 'Machine {machine} · Yours {human} · To check {stale} · Missing {missing}'],
  'mtr.retranslate': ['全部重新机器翻译', 'Translate all again'],
  'mtr.retranslate_confirm': ['重新翻译所有机器翻译的内容？你自己改过的不会被覆盖。', 'Translate everything the machine translated again? What you changed yourself stays.'],
  'mtr.no_provider': ['平台还没设置翻译服务：繁体中文照样自动转换，其他语言暂时显示英文或原文。', 'The platform has no translation service yet: Traditional Chinese is still converted; other languages show English or the original for now.'],
  'mtr.failed': ['翻译失败（{at}），点这里重试', 'Translation failed ({at}). Try again'],
  'mtr.retry': ['重试', 'Retry'],
  'mtr.pending': ['正在翻译，几秒后自动刷新…', 'Translating; this refreshes in a few seconds…'],
  'mtr.columns': ['语言：', 'Languages:'],
  'mtr.lang_on': ['开', 'On'], 'mtr.lang_off': ['关', 'Off'],
  'mtr.lang_on_hint': ['这种语言已关闭，顾客看英文。点击打开', 'Off: customers get English. Turn on'],
  'mtr.lang_off_hint': ['点击关闭这种语言，顾客改看英文', 'Turn off: customers get English instead'],
  'mtr.k_item': ['菜名', 'Dish names'], 'mtr.k_category': ['分类', 'Categories'], 'mtr.k_option': ['规格', 'Options'],
  'mtr.k_description': ['描述', 'Descriptions'], 'mtr.k_details': ['详情', 'Details'], 'mtr.k_promotion': ['优惠活动', 'Offers'],
  'mtr.original': ['原文', 'Original'],
  'mtr.st_machine': ['机器翻译', 'Machine'], 'mtr.st_converted': ['简繁转换', 'Converted'], 'mtr.st_human': ['商家已改', 'Yours'],
  'mtr.st_stale': ['原文改过，待核对', 'Original changed, check'], 'mtr.st_missing': ['未翻译', 'Not translated'],
  'mtr.keep': ['保持不变', 'Keep'], 'mtr.save': ['保存', 'Save'], 'mtr.revert': ['恢复机器翻译', 'Back to machine translation'],
  'mtr.empty': ['没有内容', 'Nothing here'],
})
