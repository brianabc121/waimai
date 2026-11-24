<script setup lang="ts">
import '../checkout'
import LangButton from './LangButton.vue'
import '../bills'
import './members/memberStrings'
import './addresses/deliveryStrings'
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { get, post } from '../api'
import { locale, setLocale, t } from '../i18n'
import { can, currentRole, logout, reloadUser, session, setAdminView } from '../session'
import './loginStrings'
import '../progress'
import '../voids'
import '../approve'
import ApprovePinDialog from './ApprovePinDialog.vue'
import { pagePerms } from '../perms'
import { useShell } from '../shell'
import { storeChats } from '../chat'

const router = useRouter(), route = useRoute()
const shell = useShell('store')
const role = computed(() => currentRole()?.role ?? '')
// a page the person's role may not open (docs/STAFF_ACCESS_DESIGN.md §6.4)
const blocked = computed(() => { const p = pagePerms(route.path); return !!p && !can(...p) })
const show = (path: string) => { const p = pagePerms(path); return !p || can(...p) }
const posts = computed(() => currentRole()?.posts ?? [])
const inStore = computed(() => posts.value.some(p => p.kind !== 'rider'))

// Clocking on and off from the back office: the same shift as in the workbench.
const shift = ref<{ on_duty: boolean } | null>(null)
onMounted(async () => { if (role.value) shift.value = await get<{ on_duty: boolean }>('/work/shift').catch(() => null) })
async function toggleShift() {
  if (!shift.value) return
  shift.value = await post<{ on_duty: boolean }>('/work/shift', { on_duty: !shift.value.on_duty, branch_id: session.branchId }).catch(() => shift.value)
}

// The number on "messages": customers' chat messages nobody at the desk has
// read. When it grows while another page is open, a line across the top says
// so (with a soft tone, unless the order sound is switched off).
const chatUnread = ref(0), chatNew = ref(false)
// and the platform's answers on "platform support"
const supportUnread = ref(0)
let chatTimer = 0, chatKnown = -1, chatSoon = 0, supportKnown = -1
function tone() {
  if (localStorage.getItem('order-sound') === 'off') return
  try {
    const ctx = new (window.AudioContext || (window as any).webkitAudioContext)()
    const o = ctx.createOscillator(), g = ctx.createGain()
    o.frequency.value = 660; o.connect(g); g.connect(ctx.destination)
    g.gain.setValueAtTime(0.2, ctx.currentTime); g.gain.exponentialRampToValueAtTime(0.001, ctx.currentTime + 0.35)
    o.start(); o.stop(ctx.currentTime + 0.4)
  } catch { /* no sound on this device */ }
}
async function countChats() {
  if (!can('chat.reply') || !session.branchId || document.visibilityState !== 'visible') return
  const r = await storeChats(session.branchId, { unread: true }).catch(() => ({ unread: chatUnread.value, support_unread: supportUnread.value }))
  const n = r.unread
  if (chatKnown >= 0 && n > chatKnown && route.path !== '/merchant/messages') { chatNew.value = true; tone() }
  if (!n) chatNew.value = false
  chatKnown = chatUnread.value = n
  if (supportKnown >= 0 && r.support_unread > supportKnown && route.path !== '/merchant/support') tone()
  supportKnown = supportUnread.value = r.support_unread
}
// a change of the store's role permissions or of this person's role comes as a
// refresh of the store: re-read what this person may do
let permSoon = 0
const countSoon = () => { clearTimeout(chatSoon); chatSoon = window.setTimeout(countChats, 400); clearTimeout(permSoon); permSoon = window.setTimeout(reloadUser, 1500) }
onMounted(() => { void countChats(); chatTimer = window.setInterval(countChats, 20000); window.addEventListener('live-refresh', countSoon) })
onBeforeUnmount(() => { clearInterval(chatTimer); clearTimeout(chatSoon); window.removeEventListener('live-refresh', countSoon) })
watch(() => route.path, p => { if (p === '/merchant/messages') chatNew.value = false })

async function out() { await logout(); session.loaded = true; router.replace('/login') }
const viewing = computed(() => !!session.user?.is_platform_admin)
// Managers and owners keep their own approval code (docs/STAFF_ACCESS_DESIGN.md §7).
const pinOpen = ref(false)
function backToAdmin() { setAdminView(null); location.href = '/admin/merchants' }
</script>

<template>
  <div class="shell" :class="{ drawer: shell.drawer.value, open: shell.open.value }">
    <header class="shell-top no-print">
      <button class="shell-menu" data-testid="nav-open" :aria-label="t('nav_menu')" @click="shell.show()"><i></i><i></i><i></i></button>
      <b class="grow shell-name">{{ currentRole()?.branch_name }}</b>
      <button v-if="shift" class="shiftbtn" :class="{ on: shift.on_duty }" :title="t(shift.on_duty ? 'shift_end' : 'shift_start')" @click="toggleShift">
        <i></i>{{ t(shift.on_duty ? 'shift_on' : 'shift_start') }}</button>
    </header>
    <div class="shell-mask no-print" @click="shell.open.value = false"></div>
    <nav class="staffbar no-print">
      <div class="shell-brand">
        <b class="grow">{{ currentRole()?.branch_name }}</b>
        <button v-if="!shell.narrow.value" class="shell-fold" data-testid="nav-fold" :title="t('nav_fold')" :aria-label="t('nav_fold')" @click="shell.fold()">«</button>
        <button v-else class="shell-fold" :aria-label="t('close')" @click="shell.open.value = false">×</button>
      </div>
      <div v-if="role && !viewing" class="shell-role" data-testid="nav-role"><span class="rtag" :class="role">{{ t('role.' + role) }}</span><span class="who">{{ session.user?.display_name }}</span></div>
      <div class="shell-links">
        <router-link v-if="show('/merchant/orders')" to="/merchant/orders">{{ t('orders') }}</router-link>
        <router-link v-if="show('/merchant/progress')" to="/merchant/progress" class="navsub" data-testid="nav-progress">{{ t('kp.nav') }}</router-link>
        <router-link v-if="show('/merchant/voids')" to="/merchant/voids" class="navsub" data-testid="nav-voids">{{ t('vd.log.title') }}</router-link>
        <router-link v-if="show('/merchant/members')" to="/merchant/members" data-testid="nav-members">{{ t('sm.title') }}</router-link>
        <router-link v-if="show('/merchant/addresses')" to="/merchant/addresses" data-testid="nav-addresses">{{ t('da.title') }}</router-link>
        <router-link v-if="show('/merchant/tables')" to="/merchant/tables">{{ t('tables') }}</router-link>
        <router-link v-if="show('/kitchen')" to="/kitchen">{{ t('kitchen') }}</router-link>
        <router-link v-if="show('/merchant/messages')" to="/merchant/messages" data-testid="nav-messages" @click="countChats">{{ t('ch.nav') }}<span v-if="chatUnread" class="navnum" data-testid="nav-chat-unread">{{ chatUnread > 99 ? '99+' : chatUnread }}</span></router-link>
        <router-link v-if="show('/merchant/menu')" to="/merchant/menu">{{ t('menu_admin') }}</router-link>
        <router-link v-if="show('/merchant/templates')" to="/merchant/templates">{{ t('templates') }}</router-link>
        <router-link v-if="show('/merchant/media')" to="/merchant/media">{{ t('promo_media') }}</router-link>
        <router-link v-if="show('/merchant/promos')" to="/merchant/promos">{{ t('promos') }}</router-link>
        <router-link v-if="show('/merchant/cash')" to="/merchant/cash">{{ t('cash') }}</router-link>
        <router-link v-if="show('/merchant/bills')" to="/merchant/bills" data-testid="nav-bills">{{ t('bl.nav') }}</router-link>
        <router-link v-if="show('/merchant/reconcile')" to="/merchant/reconcile" data-testid="nav-reconcile">{{ t('rc.nav') }}</router-link>
        <router-link v-if="show('/merchant/riders')" to="/merchant/riders">{{ t('riders') }}</router-link>
        <router-link v-if="show('/merchant/delivery-map')" to="/merchant/delivery-map">{{ t('delivery_map') }}</router-link>
        <router-link v-if="show('/merchant/staff')" to="/merchant/staff" data-testid="nav-staff">{{ t('staff') }}</router-link>
        <router-link v-if="show('/merchant/settings')" to="/merchant/settings">{{ t('settings') }}</router-link>
        <router-link v-if="show('/merchant/support')" to="/merchant/support" data-testid="nav-support" @click="countChats">{{ t('sp.title') }}<span v-if="supportUnread" class="navnum" data-testid="nav-support-unread">{{ supportUnread > 99 ? '99+' : supportUnread }}</span></router-link>
        <router-link v-if="posts.length" to="/work">{{ t('my_workbench') }}</router-link>
      </div>
      <div class="shell-foot">
        <button v-if="shift" class="shiftbtn" :class="{ on: shift.on_duty }" data-testid="office-shift" :data-on="shift.on_duty" :title="t(shift.on_duty ? 'shift_end' : 'shift_start')" @click="toggleShift">
          <i></i>{{ t(shift.on_duty ? 'shift_on' : 'shift_start') }}</button>
        <div class="row between">
          <span class="small grow who">{{ session.user?.display_name }}</span>
          <LangButton class="lang" />
        </div>
        <button v-if="!viewing && (role === 'owner' || role === 'manager')" class="btn link" data-testid="pin-open" @click="pinOpen = true">{{ t('ap.my') }}<span v-if="!session.user?.has_approve_pin" class="small muted"> · {{ t('ap.not_set') }}</span></button>
        <router-link v-if="!viewing" class="btn link" to="/login/pick" data-testid="switch-identity">{{ t('lg.switch') }}</router-link>
        <button class="btn link" @click="out">{{ t('sign_out') }}</button>
      </div>
    </nav>
    <main class="shell-main">
      <div v-if="chatNew" class="chat-toast" data-testid="chat-toast"><span>{{ t('ch.new_msg') }}</span>
        <router-link to="/merchant/messages" @click="chatNew = false">{{ t('ch.new_msg_go') }}</router-link>
        <button :aria-label="t('close')" @click="chatNew = false">×</button></div>
      <div v-if="viewing" class="banner info no-print" style="border-radius:0" data-testid="admin-viewing">
        {{ t('admin_viewing') }} <button class="btn small" style="margin-inline-start:8px" @click="backToAdmin">{{ t('admin_back') }}</button></div>
      <div v-if="!session.branchId" class="page"><div class="banner warn">{{ t('not_found') }}</div></div>
      <div v-else-if="blocked" class="page"><div class="banner warn" data-testid="no-perm">{{ t('rp.no_page') }}</div></div>
      <router-view v-else :key="session.branchId" />
    </main>
    <ApprovePinDialog v-if="pinOpen" @close="pinOpen = false" />
  </div>
</template>

<style>
.shiftbtn { background: none; border: 1px solid #8b949e; color: #fff; border-radius: 999px; padding: 4px 12px; font-size: 12.5px; cursor: pointer; white-space: nowrap; }
.shiftbtn i { display: inline-block; width: 7px; height: 7px; border-radius: 50%; background: #8b949e; margin-inline-end: 5px; }
.shiftbtn.on { border-color: #3fb950; } .shiftbtn.on i { background: #3fb950; }
</style>
