import { createApp } from 'vue'
import { createPinia } from 'pinia'
import { createRouter, createWebHashHistory } from 'vue-router'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
import '@common-ui/styles/theme.css'
import { createAddpI18n } from '@common-ui/composables/useAddpI18n'
import App from '../src/App.vue'
import serviceRouter from '../src/router'
import zh from '../src/i18n/zh-cn.json'
import en from '../src/i18n/en.json'

// Reuse the production route table, App, Layout and pages. Hash history keeps
// reloads inside this isolated fixture; authentication has its own browser gate.
serviceRouter.options.history.destroy()
const router = createRouter({ history: createWebHashHistory(), routes: serviceRouter.options.routes })
const { i18n } = createAddpI18n({ moduleMessages: { 'zh-cn': zh, en }, listenToConsole: false })
createApp(App).use(createPinia()).use(router).use(i18n).use(ElementPlus).mount('#app')
