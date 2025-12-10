import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '../stores/auth'
import OrchestrationList from '../views/OrchestrationList.vue'
import OrchestrationForm from '../views/OrchestrationForm.vue'
import ExecutionList from '../views/ExecutionList.vue'

const normalizeRedirect = fullPath => {
  if (!fullPath) {
    return '/orchestrations'
  }
  if (fullPath === '/' || fullPath === '/') {
    return '/orchestrations'
  }
  return fullPath
}

const routes = [
  {
    path: '/login',
    name: 'Login',
    component: () => import('../views/Login.vue'),
    meta: { public: true, title: '登录-编排引擎' }
  },
  {
    path: '/',
    redirect: '/orchestrations'
  },
  {
    path: '/orchestrations',
    name: 'OrchestrationList',
    component: OrchestrationList,
    meta: { requiresAuth: true, title: '编排任务列表' }
  },
  {
    path: '/orchestrations/new',
    name: 'OrchestrationCreate',
    component: OrchestrationForm,
    meta: { requiresAuth: true, title: '创建编排任务' }
  },
  {
    path: '/orchestrations/:id/edit',
    name: 'OrchestrationEdit',
    component: OrchestrationForm,
    meta: { requiresAuth: true, title: '编辑编排任务' }
  },
  {
    path: '/orchestrations/:id/executions',
    name: 'ExecutionList',
    component: ExecutionList,
    meta: { requiresAuth: true, title: '执行历史' }
  }
]

const router = createRouter({
  history: createWebHistory(import.meta.env.DEV ? '/' : '/orchestrator/'),
  routes
})

// 路由守卫：支持两种运行模式
router.beforeEach(async (to, from, next) => {
  const authStore = useAuthStore()
  const queryToken = typeof to.query.token === 'string' ? to.query.token : null

  // 1️⃣ 如果 URL 中有 token（Portal 传递）
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

  // 2️⃣ 如果已认证但无用户信息，尝试刷新
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

  // 3️⃣ 检测 iframe 环境（Portal 嵌入模式）
  const isInIframe = window.self !== window.top
  if (isInIframe && authStore.isAuthenticated) {
    return next()
  }

  // 4️⃣ 独立运行时的认证检查
  if (!authStore.isAuthenticated && requiresAuth && !isPublic) {
    return next({
      name: 'Login',
      query: { redirect: normalizeRedirect(to.fullPath) }
    })
  }

  // 5️⃣ 已登录但访问登录页，重定向到首页
  if (authStore.isAuthenticated && to.path === '/login') {
    return next({ name: 'OrchestrationList' })
  }

  next()
})

const DEFAULT_TITLE = '编排引擎'

router.afterEach((to) => {
  if (typeof document === 'undefined') {
    return
  }
  const pageTitle = typeof to.meta?.title === 'string' ? to.meta.title : DEFAULT_TITLE
  document.title = pageTitle
})

export default router
