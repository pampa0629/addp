import { createRouter, createWebHistory } from 'vue-router'
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
    redirect: '/tasks',
    meta: { requiresAuth: true }
  },
  {
    path: '/tasks',
    name: 'TaskList',
    component: () => import('@/views/TaskList.vue'),
    meta: { requiresAuth: true, title: '任务列表-数据传输' }
  },
  {
    path: '/tasks/create',
    name: 'TaskCreate',
    component: () => import('@/views/TaskWizard/TaskWizard.vue'),
    meta: { requiresAuth: true, title: '创建任务-数据传输' }
  },
  {
    path: '/tasks/create-simple',
    name: 'TaskCreateSimple',
    component: () => import('@/views/TaskForm.vue'),
    meta: { requiresAuth: true, title: '快速创建-数据传输' }
  },
  {
    path: '/tasks/:id/edit',
    name: 'TaskEdit',
    component: () => import('@/views/TaskWizard/TaskWizard.vue'),
    meta: { requiresAuth: true, title: '编辑任务-数据传输' }
  },
  {
    path: '/tasks/:id/detail',
    name: 'TaskDetail',
    component: () => import('@/views/TaskDetail.vue'),
    meta: { requiresAuth: true, title: '任务详情-数据传输' }
  },
  {
    path: '/executions',
    name: 'ExecutionList',
    component: () => import('@/views/ExecutionList.vue'),
    meta: { requiresAuth: true, title: '执行记录-数据传输' }
  },
  {
    path: '/executions/:id',
    name: 'ExecutionDetail',
    component: () => import('@/views/ExecutionDetail.vue'),
    meta: { requiresAuth: true, title: '执行详情-数据传输' }
  },
  {
    path: '/dashboard',
    name: 'Dashboard',
    component: () => import('@/views/Dashboard.vue'),
    meta: { requiresAuth: true, title: '监控面板-数据传输' }
  },
  {
    path: '/local-engines',
    name: 'LocalEngines',
    component: () => import('@/views/LocalEngines.vue'),
    meta: { requiresAuth: true, title: '本地存储引擎-数据传输' }
  }
]

const router = createRouter({
  history: createWebHistory(import.meta.env.DEV ? '/' : '/transfer/'),
  routes
})

// 路由守卫：检查登录状态
import { createAuthGuard } from '@common-ui'

router.beforeEach(createAuthGuard(useAuthStore, {
  moduleName: 'Transfer',
  loginRouteName: 'Login',
  normalizeRedirect  // 传入自定义规范化函数
}))

const DEFAULT_TITLE = '数据传输-ADDP'

router.afterEach((to) => {
  if (typeof document === 'undefined') {
    return
  }
  const pageTitle = typeof to.meta?.title === 'string' ? to.meta.title : DEFAULT_TITLE
  document.title = pageTitle
})

export default router
