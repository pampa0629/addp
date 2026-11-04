import { createRouter, createWebHistory } from 'vue-router'
import { createAuthGuard } from '@common-ui'
import { useAuthStore } from '../store/auth'
import Layout from '../components/Layout.vue'
import Login from '../views/Login.vue'

const isRuntimeEntry = window.location.pathname.startsWith('/data-apps/')
const routerBase = isRuntimeEntry ? '/' : import.meta.env.BASE_URL
const routes = [
  { path: isRuntimeEntry ? `${import.meta.env.BASE_URL}login` : '/login', name: 'Login', component: Login, meta: { requiresAuth: false } },
  { path: '/data-apps/:id', name: 'DataApplicationRuntime', component: () => import('../views/DataApplicationRuntime.vue'), meta: { requiresAuth: true, handlesForbidden: true } },
  { path: '/', component: Layout, redirect: '/applications', meta: { requiresAuth: true }, children: [
    { path: 'applications', name: 'DataApplicationList', component: () => import('../views/DataApplicationList.vue'), meta: { requiresAuth: true } },
    { path: 'applications/new', name: 'DataApplicationCreate', component: () => import('../views/DataApplicationEditor.vue'), meta: { requiresAuth: true } },
    { path: 'applications/:id', name: 'DataApplicationEdit', component: () => import('../views/DataApplicationEditor.vue'), meta: { requiresAuth: true } }
  ] }
]
const router = createRouter({ history: createWebHistory(routerBase), routes })
router.beforeEach(createAuthGuard(useAuthStore, { router, moduleName: 'Workbench', loginRouteName: 'Login' }))
export default router
