import { createApp } from 'vue'
import { createPinia } from 'pinia'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
import 'element-plus/theme-chalk/dark/css-vars.css'
import '@common-ui/styles/theme.css'
import * as ElementPlusIconsVue from '@element-plus/icons-vue'
import { createAddpI18n, useTheme } from '@common-ui'
import App from './App.vue'
import router from './router'
import zhCnMessages from './i18n/zh-cn.json'
import enMessages from './i18n/en.json'

const app = createApp(App)
for (const [name, component] of Object.entries(ElementPlusIconsVue)) {
  app.component(name, component)
}

const { i18n, init: initI18n } = createAddpI18n({
  moduleMessages: { 'zh-cn': zhCnMessages, en: enMessages },
  listenToConsole: true
})

app.use(createPinia())
app.use(router)
app.use(i18n)
app.use(ElementPlus)

useTheme({ listenToConsole: true, storageKey: 'theme-mode' }).init()
initI18n()
app.mount('#app')
