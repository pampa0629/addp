import { createApp } from 'vue'
import { createPinia } from 'pinia'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
// 导入 Element Plus 深色模式 CSS
import 'element-plus/theme-chalk/dark/css-vars.css'
// 导入统一主题 CSS
import '../../../common-frontend/basic/src/styles/theme.css'
// 按需引入实际使用的图标
import { Connection, Delete, DocumentDelete, Refresh } from '@element-plus/icons-vue'
import App from './App.vue'
import router from './router'
// 导入主题管理
import { useTheme, createAddpI18n } from '@common-ui'
import zhCnMessages from './i18n/zh-cn.json'
import enMessages from './i18n/en.json'

const app = createApp(App)
const pinia = createPinia()

// 只注册实际使用的图标
const icons = { Connection, Delete, DocumentDelete, Refresh }
for (const [key, component] of Object.entries(icons)) {
  app.component(key, component)
}

// 初始化 i18n
const { i18n, init: initI18n } = createAddpI18n({
  moduleMessages: { 'zh-cn': zhCnMessages, 'en': enMessages },
  listenToConsole: true,
})

app.use(ElementPlus)
app.use(pinia)
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
