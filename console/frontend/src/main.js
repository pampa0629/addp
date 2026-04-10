import { createApp, watch } from 'vue'
import { createPinia } from 'pinia'
import router from './router'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
import 'element-plus/theme-chalk/dark/css-vars.css'
import '@common-ui/styles/theme.css'
import { createAddpI18n } from '@common-ui/composables/useAddpI18n'
import zhCnMessages from './i18n/zh-cn.json'
import enMessages from './i18n/en.json'
import zhCn from 'element-plus/es/locale/lang/zh-cn'
import {
  Platform, User, Lock, Setting, ArrowDown, SwitchButton,
  Fold, Expand, HomeFilled, Upload, List, Timer, Connection,
  DataAnalysis, Search, Document, Box, Monitor, Operation,
  Edit, Link, Key, Loading, Sunny, Moon, Warning, Memo
} from '@element-plus/icons-vue'
import App from './App.vue'
import { useThemeStore } from './store/theme'
import { useLangStore } from './store/lang'

const app = createApp(App)

const icons = {
  Platform, User, Lock, Setting, ArrowDown, SwitchButton,
  Fold, Expand, HomeFilled, Upload, List, Timer, Connection,
  DataAnalysis, Search, Document, Box, Monitor, Operation,
  Edit, Link, Key, Loading, Sunny, Moon, Warning, Memo
}
for (const [key, component] of Object.entries(icons)) {
  app.component(key, component)
}

const pinia = createPinia()
app.use(pinia)
app.use(router)
app.use(ElementPlus, { locale: zhCn })

// 初始化 vue-i18n（console 不需要监听自己，listenToConsole: false）
const { i18n, locale, init } = createAddpI18n({
  moduleMessages: {
    'zh-cn': zhCnMessages,
    'en': enMessages,
  },
  listenToConsole: false,
})
app.use(i18n)

// 初始化主题系统
const themeStore = useThemeStore()

// 将 langStore 的语言变化同步到 vue-i18n locale
const langStore = useLangStore()
watch(() => langStore.lang, (newLang) => {
  locale.value = newLang
}, { immediate: true })

init()

app.mount('#app')
