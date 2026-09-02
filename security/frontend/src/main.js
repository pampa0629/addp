import { createApp } from 'vue'
import { createPinia } from 'pinia'
import 'element-plus/dist/index.css'
import 'element-plus/theme-chalk/dark/css-vars.css'
import '@common-ui/styles/theme.css'
import { createAddpI18n } from '../../../common-frontend/basic/src/composables/useAddpI18n'
import { useTheme } from '@common-ui'
import App from './App.vue'
import router from './router'
import zhCn from './i18n/zh-cn.json'
import en from './i18n/en.json'

const { i18n, init: initI18n } = createAddpI18n({ moduleMessages: { 'zh-cn': zhCn, en }, listenToConsole: true })
const app = createApp(App)
app.use(createPinia()).use(router).use(i18n)
useTheme({ listenToConsole: true, storageKey: 'theme-mode' }).init()
initI18n()
app.mount('#app')
