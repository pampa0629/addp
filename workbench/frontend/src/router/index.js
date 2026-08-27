import { createRouter, createWebHistory } from 'vue-router'
import { createAuthGuard } from '@common-ui'
import { useAuthStore } from '../store/auth'
import Layout from '../components/Layout.vue'
import Login from '../views/Login.vue'

const routes = [
  { path: '/login', name: 'Login', component: Login, meta: { requiresAuth: false } },
  { path: '/', component: Layout, redirect: '/views', meta: { requiresAuth: true }, children: [
    { path: 'views', name: 'ViewList', component: () => import('../views/ViewList.vue'), meta: { requiresAuth: true } },
    { path: 'views/new', name: 'ViewCreate', component: () => import('../views/ViewEditor.vue'), meta: { requiresAuth: true } },
    { path: 'views/:id', name: 'ViewEdit', component: () => import('../views/ViewEditor.vue'), meta: { requiresAuth: true } }
  ] }
]
const router = createRouter({ history: createWebHistory(import.meta.env.DEV ? '/' : '/workbench/'), routes })
router.beforeEach(createAuthGuard(useAuthStore, { moduleName: 'Workbench', loginRouteName: 'Login' }))
export default router
