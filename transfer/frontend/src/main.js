import { createApp } from 'vue'
import { createPinia } from 'pinia'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
import zhCn from 'element-plus/es/locale/lang/zh-cn'
// 按需引入实际使用的图标
import {
  ArrowLeft, Check, Delete, MagicStick, Plus, Refresh, Right,
  SwitchButton, UserFilled
} from '@element-plus/icons-vue'

import App from './App.vue'
import router from './router'

const app = createApp(App)

// 只注册实际使用的图标
const icons = {
  ArrowLeft, Check, Delete, MagicStick, Plus, Refresh, Right,
  SwitchButton, UserFilled
}
for (const [key, component] of Object.entries(icons)) {
  app.component(key, component)
}

app.use(createPinia())
app.use(router)
app.use(ElementPlus, { locale: zhCn })

app.mount('#app')
