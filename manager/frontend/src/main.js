import * as Vue from 'vue'
import { createPinia } from 'pinia'
import router from './router'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
import zhCn from 'element-plus/es/locale/lang/zh-cn'
import * as ElementPlusIconsVue from '@element-plus/icons-vue'
import App from './App.vue'
import { useAuthStore } from './store/auth'
import { loadRuntimePlugins } from '@/plugins/previews/manifestLoader'
import TablePreview from '@/components/previews/TablePreview.vue'
import ObjectStoragePreview from '@/components/previews/ObjectStoragePreview.vue'
import ImagePreview from '@/components/previews/ImagePreview.vue'
import GeoJsonPreview from '@/components/previews/GeoJsonPreview.vue'
import ShapefilePreview from '@/components/previews/ShapefilePreview.vue'
import JsonPreview from '@/components/previews/JsonPreview.vue'
import PdfPreview from '@/components/previews/PdfPreview.vue'
import DocxPreview from '@/components/previews/DocxPreview.vue'
import PptxPreview from '@/components/previews/PptxPreview.vue'
import TextPreview from '@/components/previews/TextPreview.vue'
import MarkdownPreview from '@/components/previews/MarkdownPreview.vue'
import VideoPreview from '@/components/previews/VideoPreview.vue'
import ExcelPreview from '@/components/previews/ExcelPreview.vue'

const { createApp, h, resolveComponent } = Vue

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

const mountApp = () => {
  app.mount('#app')
}

if (typeof window !== 'undefined') {
  window.Vue = Object.assign({}, window.Vue, Vue)

  window.DataExplorerPluginComponents = {
    TablePreview,
    ObjectStoragePreview,
    ImagePreview,
    GeoJsonPreview,
    ShapefilePreview,
    JsonPreview,
    PdfPreview,
    DocxPreview,
    PptxPreview,
    TextPreview,
    MarkdownPreview,
    VideoPreview,
    ExcelPreview
  }
}

loadRuntimePlugins()
  .catch((error) => {
    console.warn('DataExplorer: 自定义插件加载失败', error)
  })
  .finally(() => {
    mountApp()
  })
