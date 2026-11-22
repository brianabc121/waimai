import { createRouter, createWebHistory } from 'vue-router'
import { homeFor, loadSession } from './session'

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', component: () => import('./views/mall/MallHome.vue') },
    { path: '/s/:slug', component: () => import('./views/StoreView.vue'), props: true },
    { path: '/s/:slug/checkout', component: () => import('./views/CheckoutView.vue'), props: true },
    { path: '/orders', component: () => import('./views/CustomerOrders.vue') },
    { path: '/o/:token', component: () => import('./views/OrderView.vue'), props: true },
    { path: '/t/:code', component: () => import('./views/TableView.vue'), props: true },
    { path: '/ipad', component: () => import('./views/TableView.vue') },
    { path: '/login', component: () => import('./views/LoginView.vue') },
    { path: '/account/login', component: () => import('./views/AccountLogin.vue') },
    { path: '/me', component: () => import('./views/me/MeHome.vue') },
    { path: '/me/profile', component: () => import('./views/me/MeProfile.vue') },
    { path: '/me/wallet/:account(balance|bonus)', component: () => import('./views/me/MeWallet.vue'), props: true },
    { path: '/me/coupons', component: () => import('./views/me/MeOffers.vue') },
    { path: '/me/addresses', component: () => import('./views/me/MeAddresses.vue') },
    { path: '/me/favorites', component: () => import('./views/me/MeFavorites.vue') },
    { path: '/me/support', component: () => import('./views/me/MeSupport.vue') },
    { path: '/me/reviews', component: () => import('./views/me/MeReviews.vue') },
    { path: '/me/messages', component: () => import('./views/me/MeMessages.vue') },
    { path: '/chat/:slug', component: () => import('./views/chat/ChatView.vue'), props: true },
    { path: '/me/apply', component: () => import('./views/me/MeApply.vue') },
    { path: '/me/about', component: () => import('./views/me/MeAbout.vue') },
    { path: '/me/feedback', component: () => import('./views/me/MeFeedback.vue') },
    { path: '/me/help', component: () => import('./views/me/MeHelp.vue') },
    {
      path: '/merchant', component: () => import('./views/StaffLayout.vue'), meta: { staff: true },
      children: [
        { path: '', redirect: '/merchant/orders' },
        { path: 'orders', component: () => import('./views/OrdersView.vue') },
        { path: 'tables', component: () => import('./views/TablesView.vue') },
        { path: 'messages', component: () => import('./views/MessagesView.vue') },
        { path: 'menu', component: () => import('./views/MenuView.vue') },
        { path: 'templates', component: () => import('./views/TemplatesView.vue') },
        { path: 'media', component: () => import('./views/MediaView.vue') },
        { path: 'promos', component: () => import('./views/PromosView.vue') },
        { path: 'cash', component: () => import('./views/CashView.vue') },
        { path: 'delivery-map', component: () => import('./views/DeliveryMapView.vue') },
        { path: 'staff', component: () => import('./views/StaffView.vue') },
        { path: 'staff/attendance', component: () => import('./views/staff/AttendanceView.vue') },
        { path: 'staff/kitchen', component: () => import('./views/staff/KitchenReportView.vue') },
        { path: 'staff/riders', component: () => import('./views/staff/RiderReportView.vue') },
        { path: 'staff/trips/:id', component: () => import('./views/staff/TripView.vue') },
        { path: 'riders', component: () => import('./views/RidersView.vue') },
        { path: 'settings', component: () => import('./views/SettingsView.vue') },
      ],
    },
    { path: '/kitchen', component: () => import('./views/StaffLayout.vue'), meta: { staff: true },
      children: [{ path: '', component: () => import('./views/KitchenView.vue') }] },
    { path: '/work', component: () => import('./views/WorkView.vue'), meta: { staff: true } },
    { path: '/rider', redirect: '/work' },
    {
      path: '/admin', component: () => import('./views/admin/AdminLayout.vue'), meta: { staff: true },
      children: [
        { path: '', redirect: '/admin/dashboard' },
        { path: 'dashboard', component: () => import('./views/admin/AdminDashboard.vue'), meta: { title: 'ad.dashboard' } },
        { path: 'api', component: () => import('./views/admin/AdminApi.vue'), meta: { title: 'ad.api' } },
        { path: 'merchants', component: () => import('./views/admin/AdminMerchants.vue'), meta: { title: 'ad.merchants' } },
        { path: 'merchants/:id', component: () => import('./views/admin/AdminMerchantDetail.vue'), props: true, meta: { title: 'ad.merchants' } },
        { path: 'items', component: () => import('./views/admin/AdminItems.vue'), meta: { title: 'ad.items' } },
        { path: 'customers', component: () => import('./views/admin/AdminCustomers.vue'), meta: { title: 'ad.customers' } },
        { path: 'mall', component: () => import('./views/admin/AdminMall.vue'), meta: { title: 'ad.mall' } },
        { path: 'chats', component: () => import('./views/admin/AdminChats.vue'), meta: { title: 'ad.chats' } },
        { path: 'worker', component: () => import('./views/admin/AdminWorker.vue'), meta: { title: 'ad.worker' } },
        { path: 'settings', component: () => import('./views/admin/AdminSettings.vue'), meta: { title: 'ad.settings' } },
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
