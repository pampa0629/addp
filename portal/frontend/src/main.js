import { createApp } from 'vue'
import { createPinia } from 'pinia'
import router from './router'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
// 导入 Element Plus 深色模式 CSS
import 'element-plus/theme-chalk/dark/css-vars.css'
import zhCn from 'element-plus/es/locale/lang/zh-cn'
// 导入自定义主题变量
import './styles/theme.css'
// 按需引入实际使用的图标
import {
  Platform, User, Lock, Setting, ArrowDown, SwitchButton,
  Fold, Expand, HomeFilled, Upload, List, Timer, Connection,
  DataAnalysis, Search, Document, Box, Monitor, Operation,
  Edit, Link, Key, Loading, Sunny, Moon
} from '@element-plus/icons-vue'
import App from './App.vue'
import { useThemeStore } from './store/theme'

const app = createApp(App)

// 只注册实际使用的图标
const icons = {
  Platform, User, Lock, Setting, ArrowDown, SwitchButton,
  Fold, Expand, HomeFilled, Upload, List, Timer, Connection,
  DataAnalysis, Search, Document, Box, Monitor, Operation,
  Edit, Link, Key, Loading, Sunny, Moon
}
for (const [key, component] of Object.entries(icons)) {
  app.component(key, component)
}

const pinia = createPinia()
app.use(pinia)
app.use(router)
app.use(ElementPlus, { locale: zhCn })

// 初始化主题系统 (必须在 pinia 挂载后)
const themeStore = useThemeStore()
themeStore.initSystemThemeListener()
themeStore.applyTheme()

app.mount('#app')