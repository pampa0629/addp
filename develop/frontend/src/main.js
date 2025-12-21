import { createApp } from 'vue'
import { createPinia } from 'pinia'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
// 按需引入实际使用的图标
import {
  ArrowLeft, CircleCloseFilled, Close, Connection, CopyDocument, Delete,
  Document, DocumentAdd, DocumentDelete, Download, Edit, FolderAdd,
  FolderOpened, List, MagicStick, Monitor, Plus, Refresh, RefreshRight,
  Search, SuccessFilled, SwitchButton, Upload, User, VideoPlay, View
} from '@element-plus/icons-vue'
import router from './router'
import App from './App.vue'

const app = createApp(App)
const pinia = createPinia()

// 只注册实际使用的图标
const icons = {
  ArrowLeft, CircleCloseFilled, Close, Connection, CopyDocument, Delete,
  Document, DocumentAdd, DocumentDelete, Download, Edit, FolderAdd,
  FolderOpened, List, MagicStick, Monitor, Plus, Refresh, RefreshRight,
  Search, SuccessFilled, SwitchButton, Upload, User, VideoPlay, View
}
for (const [key, component] of Object.entries(icons)) {
  app.component(key, component)
}

app.use(pinia)
app.use(router)
app.use(ElementPlus)

app.mount('#app')
