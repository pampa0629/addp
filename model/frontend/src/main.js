import { createApp } from 'vue'
import { createPinia } from 'pinia'
import 'element-plus/dist/index.css'
// 导入 Element Plus 深色模式 CSS
import 'element-plus/theme-chalk/dark/css-vars.css'
// 导入统一主题 CSS
import '@common-ui/styles/theme.css'
import App from './App.vue'
import router from './router'
// 导入主题管理
import { useTheme } from '@common-ui'
import { createAddpI18n } from '../../../common-frontend/basic/src/composables/useAddpI18n'
import zhCnMessages from './i18n/zh-cn.json'
import enMessages from './i18n/en.json'

const { i18n, init: initI18n } = createAddpI18n({
  moduleMessages: { 'zh-cn': zhCnMessages, 'en': enMessages },
  listenToConsole: true,
})

const app = createApp(App)

app.use(createPinia())
app.use(router)
app.use(i18n)

// 初始化主题系统
const { init: initTheme } = useTheme({
  listenToConsole: true,
  storageKey: 'theme-mode'
})
initTheme()
initI18n()

app.mount('#app')
