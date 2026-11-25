// 原型用：每页共用的顶栏和页脚。正式版由 Astro 布局组件生成（docs/WEBSITE_DESIGN.md §7）。
const page = document.body.dataset.page || ''
const on = p => (p === page ? ' class="on"' : '')
document.body.insertAdjacentHTML('afterbegin', `
<header class="nav"><div class="wrap">
  <a class="logo" href="index.html"><i>食</i>Waimai <small>餐饮点餐系统</small></a>
  <nav class="menu">
    <a href="feature-dine-in.html"${on('features')}><span class="dd">功能</span></a>
    <a href="#"><span class="dd">解决方案</span></a>
    <a href="pricing.html"${on('pricing')}>价格</a>
    <a href="#">关于我们</a>
  </nav>
  <div class="right">
    <span class="lang">🌐 简体中文 ▾</span>
    <a class="login" href="#">登录</a>
    <a class="btn main sm" href="demo.html">预约演示</a>
    <span class="burger">☰</span>
  </div>
</div></header>`)
document.body.insertAdjacentHTML('beforeend', `
<footer><div class="wrap">
  <div class="cols">
    <div><a class="logo" href="index.html"><i>食</i>Waimai</a>
      <div>一套系统，管好外卖、自取、堂食和代客下单。</div>
      <a class="eater" href="#">我是顾客，去点餐 →</a></div>
    <div><h4>功能</h4><a>外卖商城</a><a>代客下单</a><a>堂食桌台</a><a>后厨进度</a><a>活动和优惠券</a><a>结账和对账</a></div>
    <div><h4>解决方案</h4><a>快餐小吃</a><a>中餐正餐</a><a>茶饮咖啡</a><a>多门店连锁</a></div>
    <div><h4>公司</h4><a>关于我们</a><a>价格</a><a>员工 App 下载</a><a>常见问题</a></div>
    <div><h4>联系我们</h4><a>WhatsApp　+63 9xx xxx xxxx</a><a>Telegram　@waimai</a><a>hello@brand.com</a></div>
  </div>
  <div class="base"><span>© 2026 【公司名】 · 隐私政策 · 服务条款</span><span>🌐 简体中文 · English · ไทย · Tiếng Việt · မြန်မာ · ខ្មែរ</span></div>
</div></footer>
${page === 'demo' ? '' : '<div class="sticky-cta"><a class="btn line" href="#">WhatsApp 咨询</a><a class="btn main" href="demo.html">预约演示</a></div>'}`)
