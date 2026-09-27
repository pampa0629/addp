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
import { useAuthStore } from '../src/store/auth'

// Reuse the production route table, App, Layout and pages. Hash history keeps
// reloads inside this isolated fixture; authentication has its own browser gate.
serviceRouter.options.history.destroy()
const router = createRouter({ history: createWebHashHistory(), routes: serviceRouter.options.routes })
const { i18n } = createAddpI18n({ moduleMessages: { 'zh-cn': zh, en }, listenToConsole: false })
const pinia = createPinia()
useAuthStore(pinia).authContext = {
  context: { type: 'tenant', tenant_id: '7' },
  authorization: { role_assignments: [{
    scope: { type: 'tenant', tenant_id: '7' },
    permissions: [
      'service.definition.read', 'service.definition.create', 'service.definition.update',
      'meta.catalog.read', 'system.engine_catalog.read',
      'service.external_registration.read', 'service.external_registration.create',
      'service.external_registration.update', 'service.external_registration.delete'
    ]
  }] }
}
createApp(App).use(pinia).use(router).use(i18n).use(ElementPlus).mount('#app')
