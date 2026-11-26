import { createApp } from 'vue'
import App from './App.vue'
import { router } from './router'
import './styles.css'
import './office.css'
import { labelTables } from './tables'
import { fitTexts } from './fit'
import { watchStaleBuild } from './stale'
import { initLocale } from './i18n'
import { loadSite } from './site'

watchStaleBuild(router)

// a language other than Chinese or English loads its texts before the first paint
// and the site (the mall or a store's own page) is known before the first route
void Promise.all([initLocale(), loadSite()]).finally(() => {
  createApp(App).use(router).mount('#app')
  labelTables(document.getElementById('app')!)
  fitTexts(document.getElementById('app')!)
})
