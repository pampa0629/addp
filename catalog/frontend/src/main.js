import { createApp } from 'vue'
import { createPinia } from 'pinia'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
import 'element-plus/theme-chalk/dark/css-vars.css'
import '@common-ui/styles/theme.css'
import App from './App.vue'
import router from './router'
import { useTheme } from '@common-ui'
import { createAddpI18n } from '@common-ui/composables/useAddpI18n'
import graphMessagesZhCn from '@addp/common-frontend/graph/i18n/zh-cn.json'
import graphMessagesEn from '@addp/common-frontend/graph/i18n/en.json'
import zhCnMessages from './i18n/zh-cn.json'
import enMessages from './i18n/en.json'

const app = createApp(App)
const { i18n, init: initI18n } = createAddpI18n({
  moduleMessages: {
    'zh-cn': { ...zhCnMessages, ...graphMessagesZhCn },
    en: { ...enMessages, ...graphMessagesEn }
  },
  listenToConsole: true,
})

app.use(createPinia())
app.use(router)
app.use(ElementPlus)
app.use(i18n)

const { init: initTheme } = useTheme({ listenToConsole: true, storageKey: 'theme-mode' })
initTheme()
initI18n()

app.mount('#app')
