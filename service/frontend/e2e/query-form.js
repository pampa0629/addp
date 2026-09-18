import { createApp, h } from 'vue'
import { createPinia } from 'pinia'
import { createRouter, createWebHashHistory, RouterView } from 'vue-router'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
import '@common-ui/styles/theme.css'
import { createAddpI18n } from '@common-ui/composables/useAddpI18n'
import QueryServiceForm from '../src/views/QueryServiceForm.vue'
import { navigateServiceRoute } from '../src/utils/moduleNavigation'
import zh from '../src/i18n/zh-cn.json'
import en from '../src/i18n/en.json'

// The real editor and HTTP clients run here; Playwright supplies deterministic API responses.
const router = createRouter({ history: createWebHashHistory(), routes: [
  { path: '/query-services/create', name: 'QueryServiceCreate', component: QueryServiceForm },
  { path: '/query-services/:id/edit', name: 'QueryServiceEdit', component: QueryServiceForm },
  { path: '/query-services/:id', component: { render: () => h('h2', 'Query service detail') } },
  { path: '/query-services', component: { render: () => h('h2', 'Query service list') } }
] })
const { i18n } = createAddpI18n({ moduleMessages: { 'zh-cn': zh, en }, listenToConsole: false })
createApp({ render: () => h('div', [
  // Keep RouterView unkeyed so identity-switch tests exercise the reused editor.
  ...[41, 42].map(id => h('button', {
    onClick: () => navigateServiceRoute(router, `/query-services/${id}/edit`)
  }, `Edit service ${id}`)),
  h(RouterView)
]) }).use(createPinia()).use(router).use(i18n).use(ElementPlus).mount('#app')
