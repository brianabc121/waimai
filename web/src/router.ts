import { createRouter, createWebHistory } from 'vue-router'
import { homeFor, loadSession } from './session'

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', redirect: '/login' },
    { path: '/s/:slug', component: () => import('./views/StoreView.vue'), props: true },
    { path: '/s/:slug/checkout', component: () => import('./views/CheckoutView.vue'), props: true },
    { path: '/o/:token', component: () => import('./views/OrderView.vue'), props: true },
    { path: '/login', component: () => import('./views/LoginView.vue') },
    {
      path: '/merchant', component: () => import('./views/StaffLayout.vue'), meta: { staff: true },
      children: [
        { path: '', redirect: '/merchant/orders' },
        { path: 'orders', component: () => import('./views/OrdersView.vue') },
        { path: 'menu', component: () => import('./views/MenuView.vue') },
        { path: 'promos', component: () => import('./views/PromosView.vue') },
        { path: 'cash', component: () => import('./views/CashView.vue') },
        { path: 'settings', component: () => import('./views/SettingsView.vue') },
      ],
    },
    { path: '/kitchen', component: () => import('./views/StaffLayout.vue'), meta: { staff: true },
      children: [{ path: '', component: () => import('./views/KitchenView.vue') }] },
    { path: '/rider', component: () => import('./views/RiderView.vue'), meta: { staff: true } },
    { path: '/admin', component: () => import('./views/AdminView.vue'), meta: { staff: true } },
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
