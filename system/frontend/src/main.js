import { createApp } from 'vue'
import { createPinia } from 'pinia'
import router from './router'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
import zhCn from 'element-plus/es/locale/lang/zh-cn'
// 按需引入实际使用的图标
import {
  Checked, Connection, Delete, Document, DocumentCopy, Edit,
  Files, Grid, HomeFilled, Key, Monitor, Plus, Setting,
  SwitchButton, User
} from '@element-plus/icons-vue'
import App from './App.vue'

const app = createApp(App)

// 只注册实际使用的图标
const icons = {
  Checked, Connection, Delete, Document, DocumentCopy, Edit,
  Files, Grid, HomeFilled, Key, Monitor, Plus, Setting,
  SwitchButton, User
}
for (const [key, component] of Object.entries(icons)) {
  app.component(key, component)
}

app.use(createPinia())
app.use(router)
app.use(ElementPlus, { locale: zhCn })
app.mount('#app')