import { createApp } from 'vue'
import { createPinia } from 'pinia'

import App from './App.vue'
import { router } from './router'
import { apply, read } from './shared/theme'
import './styles/base.css'

apply(read())

/*
 * Mounted once the first route has resolved, so the loader index.html draws
 * inside #app stays until there is a page to replace it with (FEAT-074 AC-1).
 */
const app = createApp(App).use(createPinia()).use(router)
const mount = () => void app.mount('#app')
void router.isReady().then(mount, mount)
