// 原型用：画一张示意底图（不是 Google 地图），再叠上店铺图钉和配送区。正式实现见 MAP_ZONES_DESIGN.md。
var NS = 'http://www.w3.org/2000/svg';
function el(tag, attrs, text) { var e = document.createElementNS(NS, tag); for (var k in attrs) e.setAttribute(k, attrs[k]); if (text != null) e.textContent = text; return e; }
function rng(seed) { return function () { seed = (seed * 1664525 + 1013904223) % 4294967296; return seed / 4294967296; }; }

function baseMap(svg, view) {
  svg.setAttribute('viewBox', view || '0 0 1000 720');
  svg.setAttribute('preserveAspectRatio', 'xMidYMid slice');
  svg.appendChild(el('rect', { x: -600, y: -600, width: 2200, height: 1920, fill: '#f1f3f4' }));
  var g = el('g', { transform: 'rotate(-20 500 360)' }); svg.appendChild(g);
  var r = rng(7), S = 62, x, y;
  for (x = -620; x < 1600; x += S) for (y = -620; y < 1400; y += S) {
    var v = r();
    if (v < 0.07) g.appendChild(el('rect', { x: x + 5, y: y + 5, width: S - 10, height: S - 10, rx: 4, fill: '#cfe8d0' }));
    else if (v < 0.5) g.appendChild(el('rect', { x: x + 8 + r() * 6, y: y + 8 + r() * 6, width: 18 + r() * 24, height: 16 + r() * 24, rx: 2, fill: '#e6e8eb' }));
  }
  g.appendChild(el('rect', { x: 314, y: 190, width: 114, height: 114, rx: 6, fill: '#cfe8d0' }));
  function road(d, casing, fill, w) {
    g.appendChild(el('path', { d: d, stroke: casing, 'stroke-width': w + 2.5, fill: 'none' }));
    return function () { g.appendChild(el('path', { d: d, stroke: fill, 'stroke-width': w, fill: 'none' })); };
  }
  var tops = [];
  for (x = -620; x < 1600; x += S) tops.push(road('M' + x + ' -700V1500', '#dadce0', '#fff', 6));
  for (y = -620; y < 1400; y += S) tops.push(road('M-700 ' + y + 'H1700', '#dadce0', '#fff', 6));
  tops.forEach(function (f) { f(); });
  tops = [];
  [124, 434, 744].forEach(function (y) { tops.push(road('M-700 ' + y + 'H1700', '#eebf4d', '#fde293', 12)); });
  [248, 558, 930].forEach(function (x) { tops.push(road('M' + x + ' -700V1500', '#eebf4d', '#fde293', 12)); });
  tops.forEach(function (f) { f(); });
  // 河和快速路不跟着街区转
  svg.appendChild(el('path', { d: 'M-80 52C160 6 380 92 600 40S900 18 1090 70', stroke: '#a9d3f5', 'stroke-width': 46, fill: 'none', 'stroke-linecap': 'round' }));
  svg.appendChild(el('path', { d: 'M690 -40C760 160 800 300 792 470S860 660 930 780', stroke: '#e9a13b', 'stroke-width': 17, fill: 'none' }));
  svg.appendChild(el('path', { d: 'M690 -40C760 160 800 300 792 470S860 660 930 780', stroke: '#f9cf7f', 'stroke-width': 13, fill: 'none' }));
  function label(x, y, t, o) {
    o = o || {};
    var e = el('text', { x: x, y: y, 'font-size': o.size || 11, fill: o.fill || '#5f6368', 'text-anchor': 'middle', stroke: '#fff', 'stroke-width': 3, 'paint-order': 'stroke', 'font-weight': o.bold ? 700 : 400, 'letter-spacing': o.sp || 0 }, t);
    if (o.rot != null) e.setAttribute('transform', 'rotate(' + o.rot + ' ' + x + ' ' + y + ')');
    svg.appendChild(e);
  }
  label(566, 378, 'Ayala Ave', { rot: -20 }); label(250, 480, 'Gil Puyat Ave', { rot: 70 }); label(533, 97, 'Makati Ave', { rot: 70 });
  label(300, 150, 'Kalayaan Ave', { rot: -20 }); label(806, 250, 'EDSA', { rot: 78, fill: '#8a5a00' });
  label(430, 345, 'MAKATI CBD', { size: 13, sp: 1.5, bold: 1, fill: '#70757a' }); label(540, 175, 'POBLACION', { size: 12, sp: 1.5, fill: '#70757a' });
  label(650, 105, 'ROCKWELL', { size: 12, sp: 1.5, fill: '#70757a' }); label(880, 420, 'BGC', { size: 13, sp: 1.5, bold: 1, fill: '#70757a' });
  label(450, 600, 'SAN LORENZO', { size: 12, sp: 1.5, fill: '#70757a' }); label(170, 330, 'BEL-AIR', { size: 12, sp: 1.5, fill: '#70757a' });
  label(700, 640, 'DASMARIÑAS', { size: 12, sp: 1.5, fill: '#70757a' }); label(120, 600, 'PIO DEL PILAR', { size: 12, sp: 1.5, fill: '#70757a' });
}

var STORE = [480, 390];
var ZONES = [
  { id: 'cbd', name: '马卡蒂 CBD', en: 'Makati CBD', fee: 49, min: 200, eta: 25, color: '#2a78d6', at: [470, 462], pts: [[360,300],[470,262],[590,290],[640,380],[600,480],[470,520],[350,470],[320,380]] },
  { id: 'pob', name: '波布拉西翁 / Rockwell', en: 'Poblacion / Rockwell', fee: 69, min: 300, eta: 35, color: '#18794e', pts: [[340,150],[520,108],[700,150],[725,270],[648,372],[592,286],[470,258],[358,296]] },
  { id: 'bgc', name: 'BGC', en: 'BGC', fee: 99, min: 500, eta: 45, color: '#7c3aed', pts: [[660,390],[735,284],[880,300],[915,450],[840,580],[680,560],[612,488]] }
];
var NEWZ = { id: 'new', name: '圣洛伦索', en: 'San Lorenzo', fee: 59, min: 250, color: '#c2255c', pts: [[300,500],[400,455],[520,470],[600,500],[668,570],[560,650],[380,640]] };

function centroid(p) { var x = 0, y = 0; p.forEach(function (q) { x += q[0]; y += q[1]; }); return [x / p.length, y / p.length]; }
function drawZone(svg, z, o) {
  o = o || {};
  svg.appendChild(el('polygon', { points: z.pts.join(' '), fill: z.color, 'fill-opacity': o.dim ? 0.08 : o.sel ? 0.3 : 0.18, stroke: z.color, 'stroke-width': o.sel ? 3 : 2, 'stroke-opacity': o.dim ? 0.45 : 1, 'stroke-linejoin': 'round' }));
  if (o.handles) {
    z.pts.forEach(function (p, i) {
      var q = z.pts[(i + 1) % z.pts.length];
      svg.appendChild(el('circle', { cx: (p[0] + q[0]) / 2, cy: (p[1] + q[1]) / 2, r: 4, fill: '#fff', 'fill-opacity': .7, stroke: z.color, 'stroke-width': 1.5, 'stroke-opacity': .7 }));
    });
    z.pts.forEach(function (p) { svg.appendChild(el('circle', { cx: p[0], cy: p[1], r: 6, fill: '#fff', stroke: z.color, 'stroke-width': 2.5 })); });
  }
  if (o.label === false) return;
  var c = o.at || z.at || centroid(z.pts), t1 = z.name, t2 = '₱' + z.fee + ' · 起送 ₱' + z.min, w = Math.max(t1.length * 12, t2.length * 7.6) + 22;
  var g = el('g', { opacity: o.dim ? .6 : 1 });
  g.appendChild(el('rect', { x: c[0] - w / 2, y: c[1] - 21, width: w, height: 40, rx: 8, fill: '#fff', stroke: z.color, 'stroke-width': 1.5 }));
  g.appendChild(el('text', { x: c[0], y: c[1] - 4, 'text-anchor': 'middle', 'font-size': 12.5, 'font-weight': 700, fill: z.color }, t1));
  g.appendChild(el('text', { x: c[0], y: c[1] + 12, 'text-anchor': 'middle', 'font-size': 11.5, fill: '#3f4650' }, t2));
  svg.appendChild(g);
}
function pin(svg, p, o) {
  o = o || {};
  var g = el('g', { transform: 'translate(' + p[0] + ' ' + p[1] + ')' });
  if (o.lift) g.appendChild(el('ellipse', { cx: 0, cy: 2, rx: 12, ry: 4, fill: '#000', opacity: .22 }));
  var b = el('g', { transform: 'translate(0 ' + (o.lift ? -12 : 0) + ')' });
  b.appendChild(el('path', { d: 'M0 0C-4 -12 -15 -17 -15 -29A15 15 0 1 1 15 -29C15 -17 4 -12 0 0Z', fill: o.color || '#d9480f', stroke: '#fff', 'stroke-width': 2 }));
  if (o.icon === 'dot') b.appendChild(el('circle', { cx: 0, cy: -29, r: 5.5, fill: '#fff' }));
  else { b.appendChild(el('path', { d: 'M-8 -31h16l-2 -6h-12zM-7 -30v8h14v-8M-2 -22v-5h4v5', fill: 'none', stroke: '#fff', 'stroke-width': 1.8, 'stroke-linejoin': 'round' })); }
  g.appendChild(b);
  if (o.text) g.appendChild(el('text', { x: 0, y: 18, 'text-anchor': 'middle', 'font-size': 12.5, 'font-weight': 700, fill: '#b93a08', stroke: '#fff', 'stroke-width': 3.5, 'paint-order': 'stroke' }, o.text));
  svg.appendChild(g);
}
