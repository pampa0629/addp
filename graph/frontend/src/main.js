import { createApp } from 'vue'
import { createPinia } from 'pinia'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
import 'element-plus/theme-chalk/dark/css-vars.css'
import '../../../common-frontend/basic/src/styles/theme.css'
import App from './App.vue'
import router from './router'
import { useTheme } from '@common-ui'

const app = createApp(App)
const pinia = createPinia()

app.use(ElementPlus)
app.use(pinia)
app.use(router)

const { init: initTheme } = useTheme({
  listenToConsole: true,
  storageKey: 'theme-mode'
})
initTheme()

app.mount('#app')
