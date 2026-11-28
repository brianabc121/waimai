import { onScopeDispose, ref, type Ref } from 'vue'

// useNow is a clock for screens that show "x minutes ago": it ticks every
// `every` milliseconds and stops with the component, so that a screen opened
// and closed many times in a shift does not leave its timers running
// (docs/CODE_REVIEW_FIX_PLAN.md L02).
export function useNow(every: number): Ref<number> {
  const now = ref(Date.now())
  const timer = setInterval(() => { now.value = Date.now() }, every)
  onScopeDispose(() => clearInterval(timer))
  return now
}
