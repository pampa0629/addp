import { createApp } from 'vue'
import { createPinia } from 'pinia'
import router from './router'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
// 导入 Element Plus 深色模式 CSS
import 'element-plus/theme-chalk/dark/css-vars.css'
// 导入统一主题 CSS
import '../../../common-frontend/basic/src/styles/theme.css'
import zhCn from 'element-plus/es/locale/lang/zh-cn'
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
app.use(ElementPlus, { locale: zhCn })

// 初始化主题系统
const { init: initTheme } = useTheme({
  listenToConsole: true,
  storageKey: 'theme-mode'
})

initTheme()

app.mount('#app')