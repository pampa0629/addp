import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '../store/auth'

const normalizeRedirect = fullPath => {
  if (!fullPath) {
    return '/scan'
  }
  if (fullPath === '/meta' || fullPath === '/meta/') {
    return '/scan'
  }
  if (fullPath.startsWith('/meta/')) {
    return fullPath.replace('/meta', '') || '/scan'
  }
  return fullPath
}

const routes = [
  {
    path: '/login',
    name: 'Login',
    component: () => import('../views/Login.vue'),
    meta: { public: true, title: '登录-addp' }
  },
  {
    path: '/',
    component: () => import('../components/Layout.vue'),
    meta: { requiresAuth: true, title: '元数据-addp' },
    children: [
      {
        path: '',
        redirect: { name: 'MetadataScan' },
        meta: { requiresAuth: true }
      },
      {
        path: 'scan',
        name: 'MetadataScan',
        component: () => import('../views/MetadataScan.vue'),
        meta: { requiresAuth: true, title: '元数据-addp' }
      },
      {
        path: 'tasks',
        name: 'TaskMonitor',
        component: () => import('../views/TaskMonitor.vue'),
        meta: { requiresAuth: true, title: '元数据-addp' }
      }
    ]
  }
]

const router = createRouter({
  history: createWebHistory(import.meta.env.DEV ? '/' : '/meta/'),
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
    return next({ name: 'MetadataScan' })
  }

  next()
})

const DEFAULT_TITLE = '元数据-addp'

router.afterEach((to) => {
  if (typeof document === 'undefined') {
    return
  }
  const pageTitle = typeof to.meta?.title === 'string' ? to.meta.title : DEFAULT_TITLE
  document.title = pageTitle
})

export default router
