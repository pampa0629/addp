import { createApp } from 'vue'
import { createPinia } from 'pinia'
import router from './router'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
import 'element-plus/theme-chalk/dark/css-vars.css'
import '../../../common-frontend/basic/src/styles/theme.css'
import zhCn from 'element-plus/es/locale/lang/zh-cn'
import { useTheme, createAddpI18n } from '@common-ui'
import App from './App.vue'
import zhCnMessages from './i18n/zh-cn.json'
import enMessages from './i18n/en.json'

const app = createApp(App)

app.use(createPinia())
app.use(router)
app.use(ElementPlus, { locale: zhCn })

const { i18n, init: initI18n } = createAddpI18n({
  moduleMessages: { 'zh-cn': zhCnMessages, 'en': enMessages },
  listenToConsole: true,
})
app.use(i18n)
initI18n()

const { init: initTheme } = useTheme({
  listenToConsole: true,
  storageKey: 'theme-mode'
})
initTheme()

app.mount('#app')
