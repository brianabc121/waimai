// A stand-in for the Google Maps JavaScript API, for e2e/mapgoogle.cjs. It
// imitates the parts web/src/maps.ts relies on, the way Google documents them:
//   - custom overlays live in panes that the map itself moves while panning,
//     without calling draw();
//   - draw() is called when a zoom changes where positions fall, on every
//     frame of the zoom animation;
//   - the pointer over the map is set with the draggableCursor option;
//   - map clicks also fire for clicks that bubble up from an overlay pane.
// It shows no tiles and talks to no server. It proves that our code drives
// this interface correctly, not what the real map looks like.
(() => {
  const TILE = 256
  const world = (lat, lng, zoom) => {
    const s = TILE * 2 ** zoom, sin = Math.min(0.9999, Math.max(-0.9999, Math.sin(lat * Math.PI / 180)))
    return { x: (lng + 180) / 360 * s, y: (0.5 - Math.log((1 + sin) / (1 - sin)) / (4 * Math.PI)) * s }
  }
  const unworld = (x, y, zoom) => {
    const s = TILE * 2 ** zoom, n = Math.PI - 2 * Math.PI * y / s
    return new LatLng(180 / Math.PI * Math.atan(0.5 * (Math.exp(n) - Math.exp(-n))), x / s * 360 - 180)
  }
  class LatLng { constructor(lat, lng) { this._lat = lat; this._lng = lng } lat() { return this._lat } lng() { return this._lng } }
  class Point { constructor(x, y) { this.x = x; this.y = y } }

  class Map {
    constructor(el, opts) {
      this.el = el; this.opts = { ...opts }; this.listeners = {}; this.overlays = []
      this.center = new LatLng(opts.center.lat, opts.center.lng)
      this.zoom = opts.zoom; this.shown = opts.zoom // shown is fractional during a zoom
      el.style.background = '#e8eaed'; el.style.overflow = 'hidden'
      this.panes = document.createElement('div')
      this.panes.className = 'fake-panes'
      this.panes.style.cssText = 'position:absolute;left:0;top:0;width:0;height:0'
      this.mouseTarget = document.createElement('div')
      this.mouseTarget.className = 'fake-pane'
      this.mouseTarget.style.cssText = 'position:absolute;left:0;top:0'
      this.panes.appendChild(this.mouseTarget); el.appendChild(this.panes)
      this.reset()
      let drag = null, panned = false
      el.addEventListener('pointerdown', e => { panned = false; if (e.button === 0 && this.opts.gestureHandling !== 'none') drag = { x: e.clientX, y: e.clientY } })
      window.addEventListener('pointermove', e => {
        if (drag && (panned || Math.hypot(e.clientX - drag.x, e.clientY - drag.y) > 3)) {
          const c = world(this.center.lat(), this.center.lng(), this.shown)
          this.center = unworld(c.x - (e.clientX - drag.x), c.y - (e.clientY - drag.y), this.shown)
          drag = { x: e.clientX, y: e.clientY }; panned = true
          this.place(); this.fire('bounds_changed') // panes move; draw() is not called
        }
      })
      window.addEventListener('pointerup', () => { if (drag && panned) this.fire('idle'); drag = null })
      const mouse = name => e => {
        if (name === 'click' && panned) { panned = false; return }
        const r = el.getBoundingClientRect()
        this.fire(name, { latLng: this.fromContainer(e.clientX - r.left, e.clientY - r.top), domEvent: e })
      }
      el.addEventListener('click', mouse('click')); el.addEventListener('dblclick', mouse('dblclick')); el.addEventListener('mousemove', mouse('mousemove'))
      el.addEventListener('wheel', e => { e.preventDefault(); this.setZoom(this.zoom + (e.deltaY < 0 ? 1 : -1)) }, { passive: false })
      window.__fakeMaps = [...(window.__fakeMaps || []), this]
    }
    size() { return { w: this.el.clientWidth, h: this.el.clientHeight } }
    // reset puts the panes back at the box's corner; div pixels are counted from there.
    reset() {
      const c = world(this.center.lat(), this.center.lng(), this.shown), s = this.size()
      this.origin = { x: c.x - s.w / 2, y: c.y - s.h / 2 }
      this.place()
    }
    place() {
      const c = world(this.center.lat(), this.center.lng(), this.shown), s = this.size()
      this.panes.style.transform = `translate(${this.origin.x - (c.x - s.w / 2)}px, ${this.origin.y - (c.y - s.h / 2)}px)`
    }
    toDiv(l) { const p = world(l.lat(), l.lng(), this.shown); return new Point(p.x - this.origin.x, p.y - this.origin.y) }
    toContainer(lat, lng) {
      const p = world(lat, lng, this.shown), c = world(this.center.lat(), this.center.lng(), this.shown), s = this.size()
      return { x: p.x - c.x + s.w / 2, y: p.y - c.y + s.h / 2 }
    }
    fromContainer(x, y) {
      const c = world(this.center.lat(), this.center.lng(), this.shown), s = this.size()
      return unworld(x + c.x - s.w / 2, y + c.y - s.h / 2, this.shown)
    }
    redraw() { this.reset(); for (const o of this.overlays) o.draw() }
    fire(name, arg) { for (const f of this.listeners[name] || []) f(arg) }
    addListener(name, f) { (this.listeners[name] ||= []).push(f); return { remove: () => { this.listeners[name] = this.listeners[name].filter(x => x !== f) } } }
    getCenter() { return this.center }
    getZoom() { return this.zoom }
    getDiv() { return this.el }
    getBounds() { return null }
    setCenter(c) { this.center = new LatLng(c.lat, c.lng); this.redraw(); this.fire('bounds_changed'); this.fire('idle') }
    setZoom(z) {
      z = Math.max(3, Math.min(21, Math.round(z)))
      if (z === this.zoom) return
      const from = this.shown, t0 = performance.now(), run = ++this.anim || (this.anim = 1)
      this.zoom = z; this.fire('zoom_changed')
      const step = now => {
        if (run !== this.anim) return
        const k = Math.min(1, (now - t0) / 240)
        this.shown = from + (z - from) * k
        this.redraw(); this.fire('bounds_changed')
        if (k < 1) requestAnimationFrame(step); else this.fire('idle')
      }
      requestAnimationFrame(step)
    }
    setMapTypeId() {}
    setOptions(o) {
      Object.assign(this.opts, o)
      if ('draggableCursor' in o) this.el.style.cursor = o.draggableCursor || ''
    }
  }

  class OverlayView {
    setMap(map) {
      if (!map) { if (this.map) { this.map.overlays = this.map.overlays.filter(o => o !== this); this.onRemove && this.onRemove(); this.map = null } return }
      this.map = map
      Promise.resolve().then(() => { map.overlays.push(this); this.onAdd && this.onAdd(); this.draw && this.draw() })
    }
    getPanes() { return { overlayMouseTarget: this.map.mouseTarget, overlayLayer: this.map.mouseTarget, floatPane: this.map.mouseTarget } }
    getProjection() {
      const m = this.map
      return m && m.overlays.includes(this) ? {
        fromLatLngToDivPixel: l => m.toDiv(l),
        fromContainerPixelToLatLng: p => m.fromContainer(p.x, p.y),
      } : undefined
    }
  }

  class Geocoder { geocode() { return Promise.reject(new Error('ZERO_RESULTS')) } }
  window.google = { maps: { Map, OverlayView, LatLng, Point, Geocoder, importLibrary: () => Promise.reject(new Error('no library')) } }
})()
