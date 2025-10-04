import { createApp } from 'vue'
import { createPinia } from 'pinia'
import router from './router'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
import zhCn from 'element-plus/es/locale/lang/zh-cn'
import * as ElementPlusIconsVue from '@element-plus/icons-vue'
import App from './App.vue'
import { useAuthStore } from './store/auth'

const app = createApp(App)

// 注册所有图标
for (const [key, component] of Object.entries(ElementPlusIconsVue)) {
  app.component(key, component)
}

const pinia = createPinia()
app.use(pinia)

// 从 URL 参数中获取 token（Portal 通过 iframe 传递）
const urlParams = new URLSearchParams(window.location.search)
const tokenFromUrl = urlParams.get('token')
if (tokenFromUrl) {
  const authStore = useAuthStore()
  authStore.token = tokenFromUrl
  localStorage.setItem('token', tokenFromUrl)
  console.log('Manager: Token received from Portal URL')
}

app.use(router)
app.use(ElementPlus, { locale: zhCn })
app.mount('#app')