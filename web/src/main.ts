import { createApp } from 'vue'
import App from './App.vue'
import { router } from './router'
import './styles.css'
import { labelTables } from './tables'
import { watchStaleBuild } from './stale'

watchStaleBuild(router)

createApp(App).use(router).mount('#app')
labelTables(document.getElementById('app')!)
