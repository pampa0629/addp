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
    component: () => import('@/views/TaskWizard.vue'),
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
    component: () => import('@/views/TaskWizard.vue'),
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
    path: '/local-resources',
    name: 'LocalResources',
    component: () => import('@/views/LocalResources.vue'),
    meta: { requiresAuth: true, title: '本地存储引擎-数据传输' }
  }
]

const router = createRouter({
  history: createWebHistory('/transfer/'),
  routes
})

// 路由守卫：检查登录状态
router.beforeEach(async (to, from, next) => {
  const authStore = useAuthStore()
  const queryToken = typeof to.query.token === 'string' ? to.query.token : null

  if (queryToken) {
    authStore.setToken(queryToken)

    try {
      await authStore.fetchUser()
    } catch (error) {
      console.error('获取用户信息失败:', error)
      authStore.logout()
      return next({
        name: 'Login',
        query: { redirect: normalizeRedirect(to.fullPath) }
      })
    }

    // 去掉 URL 中的 token 参数，避免泄漏及重复处理
    const { token: _removed, ...restQuery } = to.query
    next({ path: to.path, query: restQuery, replace: true })
    return
  }

  if (authStore.isAuthenticated && !authStore.user) {
    try {
      await authStore.fetchUser()
    } catch (error) {
      console.error('刷新用户信息失败:', error)
      authStore.logout()
      return next({
        name: 'Login',
        query: { redirect: normalizeRedirect(to.fullPath) }
      })
    }
  }

  const requiresAuth = !!to.meta?.requiresAuth
  const isPublic = !!to.meta?.public

  // 检测是否在 iframe 中
  const isInIframe = window.self !== window.top
  if (isInIframe && authStore.isAuthenticated) {
    return next()
  }

  if (!authStore.isAuthenticated && requiresAuth && !isPublic) {
    return next({
      name: 'Login',
      query: { redirect: normalizeRedirect(to.fullPath) }
    })
  }

  if (authStore.isAuthenticated && to.path === '/login') {
    // 已登录，访问登录页，重定向到首页
    return next({ name: 'TaskList' })
  }

  next()
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
