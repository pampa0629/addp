import { createApp } from 'vue'
import { createPinia } from 'pinia'
import router from './router'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
// 导入 Element Plus 深色模式 CSS
import 'element-plus/theme-chalk/dark/css-vars.css'
// 导入统一主题 CSS
import '../../../common-frontend/basic/src/styles/theme.css'
// 按需引入实际使用的图标
import {
  Checked, Connection, Delete, Document, DocumentCopy, Edit,
  Files, Grid, HomeFilled, Key, Monitor, Plus, Refresh, Setting,
  SwitchButton, User
} from '@element-plus/icons-vue'
// ECharts 配置（新增）
import ECharts from 'vue-echarts'
import { use } from 'echarts/core'
import { CanvasRenderer } from 'echarts/renderers'
import { BarChart, LineChart, PieChart } from 'echarts/charts'
import {
  GridComponent,
  TooltipComponent,
  LegendComponent,
  TitleComponent
} from 'echarts/components'
// 导入主题管理
import { useTheme } from '@common-ui'
// 导入 i18n
import { createAddpI18n } from '../../../common-frontend/basic/src/composables/useAddpI18n'
import zhCnMessages from './i18n/zh-cn.json'
import enMessages from './i18n/en.json'

use([
  CanvasRenderer,
  BarChart,
  LineChart,
  PieChart,
  GridComponent,
  TooltipComponent,
  LegendComponent,
  TitleComponent
])

import App from './App.vue'

// 初始化 i18n（listenToConsole=true，接收 Console 的语言切换消息）
const { i18n, init: initI18n } = createAddpI18n({
  moduleMessages: { 'zh-cn': zhCnMessages, 'en': enMessages },
  listenToConsole: true,
})

const app = createApp(App)

// 全局注册 ECharts 组件
app.component('v-chart', ECharts)

// 只注册实际使用的图标
const icons = {
  Checked, Connection, Delete, Document, DocumentCopy, Edit,
  Files, Grid, HomeFilled, Key, Monitor, Plus, Refresh, Setting,
  SwitchButton, User
}
for (const [key, component] of Object.entries(icons)) {
  app.component(key, component)
}

app.use(createPinia())
app.use(router)
app.use(ElementPlus)
app.use(i18n)

// 初始化主题系统
const { init: initTheme } = useTheme({
  listenToConsole: true,
  storageKey: 'theme-mode'
})

initTheme()
initI18n()

app.mount('#app')