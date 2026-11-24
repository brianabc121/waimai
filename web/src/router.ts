import { createRouter, createWebHistory } from 'vue-router'
import { homeFor, loadSession, officeRoles, scopeFor, useScope } from './session'
import { adminScope } from './i18n'

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', component: () => import('./views/mall/MallHome.vue') },
    { path: '/s/:slug', component: () => import('./views/StoreView.vue'), props: true },
    { path: '/s/:slug/checkout', component: () => import('./views/CheckoutView.vue'), props: true },
    { path: '/orders', component: () => import('./views/CustomerOrders.vue') },
    { path: '/o/:token', component: () => import('./views/OrderView.vue'), props: true },
    { path: '/o/:token/append', component: () => import('./views/AddonCheckout.vue'), props: true },
    { path: '/t/:code', component: () => import('./views/TableView.vue'), props: true },
    { path: '/ipad', component: () => import('./views/TableView.vue') },
    { path: '/login', component: () => import('./views/LoginView.vue') },
    { path: '/login/pick', component: () => import('./views/LoginPick.vue'), meta: { staff: true } },
    { path: '/admin/login', component: () => import('./views/admin/AdminLogin.vue') },
    { path: '/account/login', component: () => import('./views/AccountLogin.vue') },
    { path: '/me', component: () => import('./views/me/MeHome.vue') },
    { path: '/me/profile', component: () => import('./views/me/MeProfile.vue') },
    { path: '/me/wallet/:account(balance|bonus)', component: () => import('./views/me/MeWallet.vue'), props: true },
    { path: '/me/coupons', component: () => import('./views/me/MeOffers.vue') },
    { path: '/me/addresses', component: () => import('./views/me/MeAddresses.vue') },
    { path: '/me/favorites', component: () => import('./views/me/MeFavorites.vue') },
    { path: '/me/support', component: () => import('./views/me/MeSupport.vue') },
    { path: '/me/support/chat', component: () => import('./views/chat/SupportChat.vue') },
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
        { path: 'orders/new', component: () => import('./views/ManualOrderView.vue') },
        { path: 'progress', component: () => import('./views/ProgressView.vue') },
        { path: 'voids', component: () => import('./views/VoidsView.vue') },
        { path: 'members', component: () => import('./views/MembersView.vue') },
        { path: 'addresses', component: () => import('./views/CustomerAddressesView.vue') },
        { path: 'tables', component: () => import('./views/TablesView.vue') },
        { path: 'messages', component: () => import('./views/MessagesView.vue') },
        { path: 'support', component: () => import('./views/SupportDesk.vue') },
        { path: 'menu', component: () => import('./views/MenuView.vue') },
        { path: 'templates', component: () => import('./views/TemplatesView.vue') },
        { path: 'media', component: () => import('./views/MediaView.vue') },
        { path: 'promos', component: () => import('./views/PromosView.vue') },
        { path: 'cash', component: () => import('./views/CashView.vue') },
        { path: 'bills', component: () => import('./views/BillsView.vue') },
        { path: 'reconcile', component: () => import('./views/ReconcileView.vue') },
        { path: 'delivery-map', component: () => import('./views/DeliveryMapView.vue') },
        { path: 'staff', component: () => import('./views/StaffView.vue') },
        { path: 'staff/roles', component: () => import('./views/staff/RolePermsView.vue') },
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
        { path: 'members', component: () => import('./views/admin/AdminMembers.vue'), meta: { title: 'ad.members' } },
        { path: 'addresses', component: () => import('./views/admin/AdminAddresses.vue'), meta: { title: 'ad.addresses' } },
        { path: 'delivery-addresses', component: () => import('./views/admin/AdminDeliveryAddresses.vue'), meta: { title: 'ad.delivery' } },
        { path: 'mall', component: () => import('./views/admin/AdminMall.vue'), meta: { title: 'ad.mall' } },
        { path: 'coupons', component: () => import('./views/admin/AdminCoupons.vue'), meta: { title: 'ad.coupons' } },
        { path: 'bills', component: () => import('./views/admin/AdminBills.vue'), meta: { title: 'bl.nav' } },
        { path: 'pay-methods', component: () => import('./views/admin/AdminPayMethods.vue'), meta: { title: 'ad.pay' } },
        { path: 'support', component: () => import('./views/admin/AdminSupport.vue'), meta: { title: 'ad.support' } },
        { path: 'chats', component: () => import('./views/admin/AdminChats.vue'), meta: { title: 'ad.chats' } },
        { path: 'worker', component: () => import('./views/admin/AdminWorker.vue'), meta: { title: 'ad.worker' } },
        { path: 'settings', component: () => import('./views/admin/AdminSettings.vue'), meta: { title: 'ad.settings' } },
        { path: 'i18n', component: () => import('./views/admin/AdminI18n.vue'), meta: { title: 'ad.i18n.nav' } },
        { path: 'audit', component: () => import('./views/admin/AdminAudit.vue'), meta: { title: 'ad.audit' } },
      ],
    },
    { path: '/ticket/:id', component: () => import('./views/TicketView.vue'), props: true, meta: { staff: true } },
    { path: '/:rest(.*)*', component: () => import('./views/NotFound.vue') },
  ],
})

// The store's back office needs a back-office sign-in: a workbench sign-in
// goes to the page that asks for the password again (docs/STAFF_ACCESS_DESIGN.md
// §5.4), and someone without a back-office role goes to the workbench. The
// kitchen screen and the printed ticket belong to the workbench too.
const officePage = (p: string) => /^\/merchant(\/|$)/.test(p)

router.beforeEach(async to => {
  const scope = scopeFor(to.path)
  if (to.meta.staff || to.path === '/login' || to.path === '/admin/login') useScope(scope)
  if (to.path === '/admin/login') {
    const u = await loadSession()
    return u?.entry === 'admin' ? '/admin' : true
  }
  if (to.meta.staff) {
    const u = await loadSession()
    if (scope === 'admin') {
      if (!u?.is_platform_admin) return { path: '/admin/login', query: { next: to.fullPath } }
      return true
    }
    if (!u) return { path: '/login', query: { next: to.fullPath } }
    if (officePage(to.path)) {
      if (!officeRoles(u).length) return '/work'
      if (u.entry === 'work') return { path: '/login/pick', query: { next: to.fullPath } }
    }
  }
  if (to.path === '/login') {
    const u = await loadSession()
    if (u) return homeFor(u)
  }
  return true
})

// The platform back office is in Chinese or English only (see i18n.ts).
router.afterEach(to => { adminScope.value = to.path === '/admin' || to.path.startsWith('/admin/') })
