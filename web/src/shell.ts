import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'

// The back-office navigation on three kinds of screen. On a computer it is a
// side column that can be folded away; on a tablet held upright or a phone it
// is a drawer opened from the top bar. Folding is remembered per shell.
export function useShell(name: string) {
  const KEY = 'nav_folded:' + name
  const query = window.matchMedia('(max-width: 1023px)')
  const narrow = ref(query.matches)
  const folded = ref(localStorage.getItem(KEY) === '1')
  const open = ref(false)
  const drawer = computed(() => narrow.value || folded.value)
  const onChange = () => { narrow.value = query.matches; open.value = false }
  onMounted(() => query.addEventListener('change', onChange))
  onUnmounted(() => query.removeEventListener('change', onChange))
  // the button in the top bar: opens the drawer on a small screen, brings the column back on a large one
  function show() {
    if (narrow.value) open.value = true
    else { folded.value = false; localStorage.removeItem(KEY) }
  }
  function fold() { folded.value = true; open.value = false; localStorage.setItem(KEY, '1') }
  const route = useRoute()
  watch(() => route.fullPath, () => { open.value = false })
  return { narrow, drawer, open, show, fold }
}
