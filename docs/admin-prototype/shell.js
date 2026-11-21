// 原型用：注入左侧导航，用示意数据画 SVG 图表。正式实现见 ADMIN_DESIGN.md 第 7 节。
(function () {
  var nav = [
    ['监控'], ['index.html', '总览'], ['api.html', 'API 监控'], ['worker.html', 'Worker 和任务', '3'],
    ['商家'], ['merchants.html', '商户'], ['items.html', '商品'],
    ['系统'], ['#', '操作记录']
  ];
  var here = document.body.dataset.page;
  var h = '<div class="brand">外卖点餐系统<small>平台管理后台</small></div>';
  nav.forEach(function (n) {
    if (n.length === 1) h += '<div class="grp">' + n[0] + '</div>';
    else h += '<a href="' + n[0] + '" class="' + (n[0] === here ? 'on' : '') + '">' + n[1] + (n[2] ? '<span class="badge">' + n[2] + '</span>' : '') + '</a>';
  });
  document.querySelector('.side').innerHTML = h;
  document.querySelector('.top').innerHTML = '<h1>' + document.body.dataset.title + '</h1><span class="grow"></span>' +
    '<span class="small muted">每 10 秒自动刷新 · 刚刚更新</span><span class="tag">中文 / EN</span><span>admin</span><a class="btn link">退出</a>';

  function rng(seed) { return function () { seed = (seed * 1664525 + 1013904223) % 4294967296; return seed / 4294967296; }; }
  function series(n, base, amp, seed, spike) {
    var r = rng(seed), out = [];
    for (var i = 0; i < n; i++) {
      var v = base + amp * Math.sin(i / n * Math.PI * 1.6 + 0.4) + (r() - .5) * amp * .5;
      if (spike && i >= spike[0] && i < spike[1]) v += spike[2] * (0.6 + r() * .6);
      out.push(Math.max(0, v));
    }
    return out;
  }
  var NS = 'http://www.w3.org/2000/svg';
  function el(tag, attrs, text) { var e = document.createElementNS(NS, tag); for (var k in attrs) e.setAttribute(k, attrs[k]); if (text != null) e.textContent = text; return e; }
  function frame(svg, W, H, max, fmt, xl) {
    var P = { l: 50, r: 52, t: 8, b: 22 };
    svg.setAttribute('viewBox', '0 0 ' + W + ' ' + H);
    for (var i = 0; i <= 3; i++) {
      var y = P.t + (H - P.t - P.b) * i / 3;
      svg.appendChild(el('line', { x1: P.l, x2: W - P.r, y1: y, y2: y, stroke: i === 3 ? '#cfd4da' : '#eef0f2' }));
      svg.appendChild(el('text', { x: P.l - 6, y: y + 4, 'text-anchor': 'end' }, fmt(max * (3 - i) / 3)));
    }
    xl.forEach(function (t, i) { svg.appendChild(el('text', { x: P.l + (W - P.l - P.r) * i / (xl.length - 1), y: H - 6, 'text-anchor': i === 0 ? 'start' : i === xl.length - 1 ? 'end' : 'middle' }, t)); });
    return P;
  }
  var XL = ['14:45', '15:00', '15:15', '15:30', '15:45'];
  window.lineChart = function (id, lines, max, fmt, H) {
    var svg = document.getElementById(id), W = +svg.dataset.w || 640; H = H || 170;
    var P = frame(svg, W, H, max, fmt, XL);
    lines.forEach(function (ln) {
      var n = ln.data.length, d = '', lx, ly;
      ln.data.forEach(function (v, i) {
        lx = P.l + (W - P.l - P.r) * i / (n - 1); ly = P.t + (H - P.t - P.b) * (1 - Math.min(v, max) / max);
        d += (i ? 'L' : 'M') + lx.toFixed(1) + ' ' + ly.toFixed(1);
      });
      svg.appendChild(el('path', { d: d, fill: 'none', stroke: ln.color, 'stroke-width': 2, 'stroke-linejoin': 'round' }));
      svg.appendChild(el('circle', { cx: lx, cy: ly, r: 4, fill: ln.color, stroke: '#fff', 'stroke-width': 2 }));
      svg.appendChild(el('text', { x: lx + 8, y: ly + 4, 'class': 'lbl' }, ln.label));
    });
  };
  window.stackChart = function (id, stacks, max, H) {
    var svg = document.getElementById(id), W = +svg.dataset.w || 640; H = H || 170;
    var P = frame(svg, W, H, max, function (v) { return Math.round(v); }, XL);
    var n = stacks[0].data.length, bw = (W - P.l - P.r) / n, ph = H - P.t - P.b;
    for (var i = 0; i < n; i++) {
      var y = H - P.b;
      stacks.forEach(function (s) {
        var hgt = ph * s.data[i] / max; if (hgt < .6) return;
        y -= hgt;
        svg.appendChild(el('rect', { x: P.l + i * bw + 1, y: y, width: Math.max(1, bw - 2), height: Math.max(0, hgt - 1), fill: s.color, rx: 1.5 }));
      });
    }
  };
  window.demo = { series: series };
})();
