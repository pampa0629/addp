import { createApp } from 'vue'
import { createPinia } from 'pinia'
import 'element-plus/dist/index.css'
import 'element-plus/theme-chalk/dark/css-vars.css'
import '@common-ui/styles/theme.css'
import { useTheme } from '@common-ui'
import { createAddpI18n } from '@common-ui/composables/useAddpI18n'
import App from './App.vue'
import router from './router'
import zh from './i18n/zh-cn.json'
import en from './i18n/en.json'

const app = createApp(App)
const { i18n, init } = createAddpI18n({
  moduleMessages: { 'zh-cn': zh, en },
  listenToConsole: true
})
app.use(createPinia()).use(i18n).use(router)
useTheme({ listenToConsole: true, storageKey: 'theme-mode' }).init()
init()
app.mount('#app')
