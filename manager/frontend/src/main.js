import * as Vue from 'vue'
import { createPinia } from 'pinia'
import router from './router'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
// 导入 Element Plus 深色模式 CSS
import 'element-plus/theme-chalk/dark/css-vars.css'
// 导入统一主题 CSS
import '../../../common-frontend/basic/src/styles/theme.css'
import 'ol/ol.css'
import { createAddpI18n } from '../../../common-frontend/basic/src/composables/useAddpI18n'
import zhCnMessages from './i18n/zh-cn.json'
import enMessages from './i18n/en.json'
// 按需引入实际使用的图标
import {
  ArrowDown, ArrowLeft, ArrowRight, Clock, Collection, Document, Download,
  Folder, Loading, Refresh, RefreshRight, Search, SwitchButton, User,
  VideoPlay, WarningFilled, ZoomIn, ZoomOut
} from '@element-plus/icons-vue'
import App from './App.vue'
import { loadRuntimePlugins } from '@/plugins/previews/manifestLoader'
import { ImagePreview } from '@common-ui'
import {
  ObjectCatalogPreview, JsonPreview, PdfPreview, ContainerPreview,
  DocxPreview, PptxPreview, TextPreview, MarkdownPreview, VideoPreview
} from '@common-ui/previews'
import {
  TablePreview, GeoJsonPreview,
  mapMessagesZhCn, mapMessagesEn, setMapConfigAPI
} from '@common-ui-map'
import configAPI from '@/api/config'
// 注入真实的地图配置 API，使 common-frontend/map 组件能获取到地图密钥
setMapConfigAPI(configAPI)
// 导入主题管理
import { useTheme } from '@common-ui'

const { createApp, h, resolveComponent } = Vue

const app = createApp(App)

// 只注册实际使用的图标
const icons = {
  ArrowDown, ArrowLeft, ArrowRight, Clock, Collection, Document, Download,
  Folder, Loading, Refresh, RefreshRight, Search, SwitchButton, User,
  VideoPlay, WarningFilled, ZoomIn, ZoomOut
}
for (const [key, component] of Object.entries(icons)) {
  app.component(key, component)
}

const { i18n, init: initI18n } = createAddpI18n({
  moduleMessages: {
    'zh-cn': { ...zhCnMessages, ...mapMessagesZhCn },
    'en': { ...enMessages, ...mapMessagesEn }
  },
  listenToConsole: true,
})

const pinia = createPinia()
app.use(pinia)

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

const mountApp = () => {
  app.mount('#app')
}

if (typeof window !== 'undefined') {
  window.Vue = Object.assign({}, window.Vue, Vue)

  window.DataExplorerPluginComponents = {
    TablePreview,
    ContainerPreview,
    ObjectCatalogPreview,
    ImagePreview,
    GeoJsonPreview,
    JsonPreview,
    PdfPreview,
    DocxPreview,
    PptxPreview,
    TextPreview,
    MarkdownPreview,
    VideoPreview
  }
}

loadRuntimePlugins()
  .catch((error) => {
    console.warn('DataExplorer: 自定义插件加载失败', error)
  })
  .finally(() => {
    mountApp()
  })
