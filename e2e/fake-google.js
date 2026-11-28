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

  // What was asked is kept in window.__fakeAsked, for tests to look at.
  //   - a point (reverse geocoding) is always somewhere in Makati;
  //   - an address is found only when window.__fakeGeocodes is set, as one
  //     result in the country asked for. A building asked for by name
  //     ("... tower") comes back the way Google answers: the street it stands
  //     on, without its name;
  //   - the Places library exists only when window.__fakePlaces is set. A
  //     chosen place has a full street address, in the language asked for.
  const asked = window.__fakeAsked = { geocode: [], suggest: [], text: [], details: [] }
  const comp = (long_name, short_name, ...types) => ({ long_name, short_name, types })
  class Geocoder {
    geocode(req) {
      asked.geocode.push(req)
      if (req.location) return Promise.resolve({ results: [{
        formatted_address: '123 Ayala Ave, Makati, Metro Manila, Philippines',
        address_components: [comp('Bel-Air', 'Bel-Air', 'sublocality_level_1', 'sublocality'), comp('Makati', 'Makati', 'locality'),
          comp('Metro Manila', 'NCR', 'administrative_area_level_1'), comp('Philippines', 'PH', 'country'), comp('1209', '1209', 'postal_code')],
      }] })
      if (!window.__fakeGeocodes) return Promise.reject(new Error('ZERO_RESULTS'))
      const where = req.componentRestrictions?.country ?? 'anywhere'
      const street = /tower/i.test(req.address) ? 'Makati Ave, Makati City, Metro Manila' : req.address
      return Promise.resolve({ results: [{ formatted_address: `${street}, ${where}`, place_id: 'geo-1', geometry: { location: new LatLng(14.5601, 121.0253) } }] })
    }
  }
  class Place {
    constructor(o) { this.id = o.id; this.requestedLanguage = o.requestedLanguage }
    fetchFields(req) {
      asked.details.push({ id: this.id, language: this.requestedLanguage, fields: [...req.fields] })
      this.location = new LatLng(14.5601, 121.0253)
      this.formattedAddress = `1 Kalayaan Ave, Poblacion, Makati (${this.requestedLanguage ?? 'browser'})`
      this.types = ['premise', 'point_of_interest']
      return Promise.resolve({ place: this })
    }
    static searchNearby() { return Promise.resolve({ places: [] }) }
    static searchByText(req) {
      asked.text.push(req)
      const p = new Place({ id: 'text-1' })
      p.displayName = req.textQuery; p.formattedAddress = '1 Kalayaan Ave, Poblacion, Makati'; p.location = new LatLng(14.5601, 121.0253)
      return Promise.resolve({ places: [p] })
    }
  }
  const places = {
    AutocompleteSessionToken: class {},
    AutocompleteSuggestion: { fetchAutocompleteSuggestions(req) {
      asked.suggest.push(req)
      const where = (req.includedRegionCodes ?? ['anywhere']).join()
      return Promise.resolve({ suggestions: [{ placePrediction: { placeId: 'place-1', types: ['premise'], mainText: { text: req.input }, secondaryText: { text: `Places, ${where}` },
        toPlace: () => new Place({ id: 'place-1' }) } }] })
    } },
    Place,
  }
  const importLibrary = name => name === 'places' && window.__fakePlaces ? Promise.resolve(places) : Promise.reject(new Error('no library'))
  window.google = { maps: { Map, OverlayView, LatLng, Point, Geocoder, importLibrary } }
})()
