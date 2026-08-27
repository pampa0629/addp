import { createRouter, createWebHistory } from 'vue-router'
import { createAuthGuard } from '@common-ui'
import { useAuthStore } from '../store/auth'
import Layout from '../components/Layout.vue'
import Login from '../views/Login.vue'

const routes = [
  { path: '/login', name: 'Login', component: Login, meta: { requiresAuth: false } },
  { path: '/data-apps/:id', name: 'DataApplicationRuntime', component: () => import('../views/DataApplicationRuntime.vue'), meta: { requiresAuth: true } },
  { path: '/', component: Layout, redirect: '/views', meta: { requiresAuth: true }, children: [
    { path: 'views', name: 'ViewList', component: () => import('../views/ViewList.vue'), meta: { requiresAuth: true } },
    { path: 'views/new', name: 'ViewCreate', component: () => import('../views/ViewEditor.vue'), meta: { requiresAuth: true } },
    { path: 'views/:id', name: 'ViewEdit', component: () => import('../views/ViewEditor.vue'), meta: { requiresAuth: true } },
    { path: 'applications', name: 'DataApplicationList', component: () => import('../views/DataApplicationList.vue'), meta: { requiresAuth: true } },
    { path: 'applications/:id', name: 'DataApplicationEdit', component: () => import('../views/DataApplicationEditor.vue'), meta: { requiresAuth: true } }
  ] }
]
const routerBase = window.location.pathname.startsWith('/workbench/') ? '/workbench/' : '/'
const router = createRouter({ history: createWebHistory(routerBase), routes })
router.beforeEach(createAuthGuard(useAuthStore, { moduleName: 'Workbench', loginRouteName: 'Login' }))
export default router
