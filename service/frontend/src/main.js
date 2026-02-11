import { createApp } from 'vue'
import { createPinia } from 'pinia'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
// 导入 Element Plus 深色模式 CSS
import 'element-plus/theme-chalk/dark/css-vars.css'
// 导入统一主题 CSS
import '../../../common-frontend/basic/src/styles/theme.css'
// 按需引入实际使用的图标
import { Clock, Grid, Link, DocumentCopy } from '@element-plus/icons-vue'
import App from './App.vue'
import router from './router'
// 导入主题管理
import { useTheme } from '@common-ui'

const app = createApp(App)
const pinia = createPinia()

// 只注册实际使用的图标
const icons = { Clock, Grid, Link, DocumentCopy }
for (const [key, component] of Object.entries(icons)) {
  app.component(key, component)
}

app.use(ElementPlus)
app.use(pinia)
app.use(router)

// 初始化主题系统
const { init: initTheme } = useTheme({
  listenToPortal: true,
  storageKey: 'theme-mode'
})

initTheme()

app.mount('#app')
