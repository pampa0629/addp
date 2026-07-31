import { createApp } from 'vue'
import { createPinia } from 'pinia'
import { ElLoading } from 'element-plus'
import 'element-plus/theme-chalk/base.css'
import 'element-plus/theme-chalk/el-loading.css'
import 'element-plus/theme-chalk/el-message.css'
import 'element-plus/theme-chalk/el-message-box.css'
import 'element-plus/theme-chalk/dark/css-vars.css'
import '../../../common-frontend/basic/src/styles/theme.css'
import App from './App.vue'
import router from './router'
import { useTheme } from '@common-ui'
import { createAddpI18n } from '../../../common-frontend/basic/src/composables/useAddpI18n'
import zhCnMessages from './i18n/zh-cn.json'
import enMessages from './i18n/en.json'
import { graphMessagesEn, graphMessagesZhCn } from '@addp/common-frontend/graph'

const app = createApp(App)
const pinia = createPinia()

const mergeGraphMessages = (moduleMessages, sharedMessages) => ({
  ...moduleMessages,
  graph: {
    ...sharedMessages.graph,
    ...moduleMessages.graph,
  },
})

const { i18n, init: initI18n } = createAddpI18n({
  moduleMessages: {
    'zh-cn': mergeGraphMessages(zhCnMessages, graphMessagesZhCn),
    'en': mergeGraphMessages(enMessages, graphMessagesEn),
  },
  listenToConsole: true,
})

app.directive('loading', ElLoading.directive)
app.use(pinia)
app.use(router)
app.use(i18n)

const { init: initTheme } = useTheme({
  listenToConsole: true,
  storageKey: 'theme-mode'
})
initTheme()
initI18n()

app.mount('#app')
