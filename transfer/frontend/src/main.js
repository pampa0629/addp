import { createApp } from 'vue'
import { createPinia } from 'pinia'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
// 导入 Element Plus 深色模式 CSS
import 'element-plus/theme-chalk/dark/css-vars.css'
// 导入统一主题 CSS
import '../../../common-frontend/basic/src/styles/theme.css'
// 按需引入实际使用的图标
import {
  ArrowLeft, Check, Delete, MagicStick, Plus, Refresh, Right,
  SwitchButton, UserFilled
} from '@element-plus/icons-vue'

import App from './App.vue'
import router from './router'
// 导入主题管理和 i18n
import { useTheme } from '@common-ui'
import { createAddpI18n } from '../../../common-frontend/basic/src/composables/useAddpI18n'
import zhCnMessages from './i18n/zh-cn.json'
import enMessages from './i18n/en.json'

const app = createApp(App)

// 只注册实际使用的图标
const icons = {
  ArrowLeft, Check, Delete, MagicStick, Plus, Refresh, Right,
  SwitchButton, UserFilled
}
for (const [key, component] of Object.entries(icons)) {
  app.component(key, component)
}

const { i18n, init: initI18n } = createAddpI18n({
  moduleMessages: { 'zh-cn': zhCnMessages, 'en': enMessages },
  listenToConsole: true,
})

app.use(createPinia())
app.use(router)
app.use(i18n)
app.use(ElementPlus)

// 初始化主题系统
const { init: initTheme } = useTheme({
  listenToConsole: true,
  storageKey: 'theme-mode'
})

initTheme()
initI18n()

app.mount('#app')
