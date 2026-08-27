import { createApp } from 'vue'
import { createPinia } from 'pinia'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
import 'element-plus/theme-chalk/dark/css-vars.css'
import 'ol/ol.css'
import '@common-ui/styles/theme.css'
import App from './App.vue'
import router from './router'
import { useTheme } from '@common-ui'
import { createAddpI18n } from '@common-ui/composables/useAddpI18n'
import zhCnMessages from './i18n/zh-cn.json'
import enMessages from './i18n/en.json'
import { mapMessagesEn, mapMessagesZhCn } from '@common-ui-map'

const app = createApp(App)
const { i18n, init: initI18n } = createAddpI18n({ moduleMessages: { 'zh-cn': { ...mapMessagesZhCn, ...zhCnMessages }, en: { ...mapMessagesEn, ...enMessages } }, listenToConsole: true })
app.use(createPinia()).use(router).use(ElementPlus).use(i18n)
useTheme({ listenToConsole: true, storageKey: 'theme-mode' }).init()
initI18n()
app.mount('#app')
