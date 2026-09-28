import { createRouter, createWebHistory } from 'vue-router'
import Layout from '../components/Layout.vue'
import { useAuthStore } from '../store/auth'

const normalizeRedirect = fullPath => {
  if (!fullPath) {
    return '/tasks'
  }
  if (fullPath === '/transfer' || fullPath === '/transfer/') {
    return '/tasks'
  }
  if (fullPath.startsWith('/transfer/')) {
    return fullPath.replace('/transfer', '') || '/tasks'
  }
  return fullPath
}

const routes = [
  {
    path: '/login',
    name: 'Login',
    component: () => import('../views/Login.vue'),
    meta: { public: true, title: '登录-数据传输' }
  },
  {
    path: '/',
    component: Layout,
    redirect: '/tasks',
    meta: { requiresAuth: true },
    children: [
      {
        path: 'tasks',
        name: 'TaskList',
        component: () => import('@/views/TaskList.vue'),
        meta: { requiresAuth: true, title: '任务列表-数据传输', requiredPermissions: ['transfer.task.read'] }
      },
      {
        path: 'tasks/create',
        name: 'TaskCreate',
        component: () => import('@/views/TaskWizard/TaskWizard.vue'),
        meta: { requiresAuth: true, title: '创建任务-数据传输', requiredPermissions: ['transfer.task.create', 'meta.catalog.read'] }
      },
      {
        path: 'tasks/:id/edit',
        name: 'TaskEdit',
        component: () => import('@/views/TaskWizard/TaskWizard.vue'),
        meta: { requiresAuth: true, title: '编辑任务-数据传输', requiredPermissions: ['transfer.task.read', 'transfer.task.update', 'meta.catalog.read'] }
      },
      {
        path: 'tasks/:id/detail',
        name: 'TaskDetail',
        component: () => import('@/views/TaskDetail.vue'),
        meta: { requiresAuth: true, title: '任务详情-数据传输', requiredPermissions: ['transfer.task.read'] }
      },
      {
        path: 'executions/:execution_id',
        name: 'ExecutionDetail',
        component: () => import('@/views/ExecutionDetail.vue'),
        meta: { requiresAuth: true, title: '执行详情-数据传输', requiredPermissions: ['transfer.task.read'] }
      },
      {
        path: 'forbidden',
        name: 'AccessDenied',
        component: () => import('@/views/AccessDenied.vue'),
        meta: { requiresAuth: true }
      },
    ]
  }
]

const router = createRouter({
  history: createWebHistory(import.meta.env.BASE_URL),
  routes
})

// 路由守卫：检查登录状态
import { createAuthGuard } from '@common-ui'

router.beforeEach(createAuthGuard(useAuthStore, {
  moduleName: 'Transfer',
  loginRouteName: 'Login',
  normalizeRedirect
}))

router.beforeEach((to) => {
  const authStore = useAuthStore()
  if (!authStore.isAuthenticated || to.name === 'AccessDenied') return true
  if (to.name === 'TaskList' && to.redirectedFrom?.path === '/' &&
      !authStore.hasPermission('transfer.task.read') &&
      authStore.hasPermission('transfer.task.create') &&
      authStore.hasPermission('meta.catalog.read')) {
    return { name: 'TaskCreate', replace: true }
  }
  const required = to.meta?.requiredPermissions || []
  if (required.every(permission => authStore.hasPermission(permission))) return true
  return { name: 'AccessDenied', replace: true }
})

const DEFAULT_TITLE = '数据传输-ADDP'

router.afterEach((to) => {
  if (typeof document === 'undefined') {
    return
  }
  const pageTitle = typeof to.meta?.title === 'string' ? to.meta.title : DEFAULT_TITLE
  document.title = pageTitle
})

export default router
