import { createApp } from 'vue'
import App from './App.vue'
import { router } from './router'
import './styles.css'
import { labelTables } from './tables'
import { watchStaleBuild } from './stale'
import { initLocale } from './i18n'

watchStaleBuild(router)

// a language other than Chinese or English loads its texts before the first paint
void initLocale().finally(() => {
  createApp(App).use(router).mount('#app')
  labelTables(document.getElementById('app')!)
})
