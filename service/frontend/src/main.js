import { createApp } from 'vue'
import { createPinia } from 'pinia'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
// 按需引入实际使用的图标
import { Clock, Layers, Link } from '@element-plus/icons-vue'
import App from './App.vue'
import router from './router'

const app = createApp(App)
const pinia = createPinia()

// 只注册实际使用的图标
const icons = { Clock, Layers, Link }
for (const [key, component] of Object.entries(icons)) {
  app.component(key, component)
}

app.use(ElementPlus)
app.use(pinia)
app.use(router)

app.mount('#app')
