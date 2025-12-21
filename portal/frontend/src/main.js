import { createApp } from 'vue'
import { createPinia } from 'pinia'
import router from './router'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
import zhCn from 'element-plus/es/locale/lang/zh-cn'
// 按需引入实际使用的图标
import {
  Platform, User, Lock, Setting, ArrowDown, SwitchButton,
  Fold, Expand, HomeFilled, Upload, List, Timer, Connection,
  DataAnalysis, Search, Document, Box, Monitor, Operation,
  Edit, Link, Key, Loading
} from '@element-plus/icons-vue'
import App from './App.vue'

const app = createApp(App)

// 只注册实际使用的图标
const icons = {
  Platform, User, Lock, Setting, ArrowDown, SwitchButton,
  Fold, Expand, HomeFilled, Upload, List, Timer, Connection,
  DataAnalysis, Search, Document, Box, Monitor, Operation,
  Edit, Link, Key, Loading
}
for (const [key, component] of Object.entries(icons)) {
  app.component(key, component)
}

app.use(createPinia())
app.use(router)
app.use(ElementPlus, { locale: zhCn })
app.mount('#app')