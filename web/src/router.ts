import { createRouter, createWebHistory } from 'vue-router'
import { homeFor, loadSession } from './session'

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', redirect: '/login' },
    { path: '/s/:slug', component: () => import('./views/StoreView.vue'), props: true },
    { path: '/s/:slug/checkout', component: () => import('./views/CheckoutView.vue'), props: true },
    { path: '/o/:token', component: () => import('./views/OrderView.vue'), props: true },
    { path: '/t/:code', component: () => import('./views/TableView.vue'), props: true },
    { path: '/ipad', component: () => import('./views/TableView.vue') },
    { path: '/login', component: () => import('./views/LoginView.vue') },
    {
      path: '/merchant', component: () => import('./views/StaffLayout.vue'), meta: { staff: true },
      children: [
        { path: '', redirect: '/merchant/orders' },
        { path: 'orders', component: () => import('./views/OrdersView.vue') },
        { path: 'tables', component: () => import('./views/TablesView.vue') },
        { path: 'menu', component: () => import('./views/MenuView.vue') },
        { path: 'templates', component: () => import('./views/TemplatesView.vue') },
        { path: 'promos', component: () => import('./views/PromosView.vue') },
        { path: 'cash', component: () => import('./views/CashView.vue') },
        { path: 'delivery-map', component: () => import('./views/DeliveryMapView.vue') },
        { path: 'settings', component: () => import('./views/SettingsView.vue') },
      ],
    },
    { path: '/kitchen', component: () => import('./views/StaffLayout.vue'), meta: { staff: true },
      children: [{ path: '', component: () => import('./views/KitchenView.vue') }] },
    { path: '/rider', component: () => import('./views/RiderView.vue'), meta: { staff: true } },
    {
      path: '/admin', component: () => import('./views/admin/AdminLayout.vue'), meta: { staff: true },
      children: [
        { path: '', redirect: '/admin/dashboard' },
        { path: 'dashboard', component: () => import('./views/admin/AdminDashboard.vue'), meta: { title: 'ad.dashboard' } },
        { path: 'api', component: () => import('./views/admin/AdminApi.vue'), meta: { title: 'ad.api' } },
        { path: 'merchants', component: () => import('./views/admin/AdminMerchants.vue'), meta: { title: 'ad.merchants' } },
        { path: 'merchants/:id', component: () => import('./views/admin/AdminMerchantDetail.vue'), props: true, meta: { title: 'ad.merchants' } },
        { path: 'items', component: () => import('./views/admin/AdminItems.vue'), meta: { title: 'ad.items' } },
        { path: 'worker', component: () => import('./views/admin/AdminWorker.vue'), meta: { title: 'ad.worker' } },
        { path: 'audit', component: () => import('./views/admin/AdminAudit.vue'), meta: { title: 'ad.audit' } },
      ],
    },
    { path: '/ticket/:id', component: () => import('./views/TicketView.vue'), props: true, meta: { staff: true } },
    { path: '/:rest(.*)*', component: () => import('./views/NotFound.vue') },
  ],
})

router.beforeEach(async to => {
  if (to.meta.staff) {
    const u = await loadSession()
    if (!u) return { path: '/login', query: { next: to.fullPath } }
  }
  if (to.path === '/login') {
    const u = await loadSession()
    if (u) return homeFor(u)
  }
  return true
})
