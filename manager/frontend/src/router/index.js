import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '../store/auth'

const routes = [
  {
    path: '/login',
    name: 'Login',
    component: () => import('../views/Login.vue'),
    meta: { title: '登录-addp' }
  },
  {
    path: '/',
    component: () => import('../components/Layout.vue'),
    meta: { requiresAuth: true, title: '数据管理-addp' },
    children: [
      {
        path: '',
        redirect: 'data-explorer'
      },
      {
        path: 'data-explorer',
        name: 'DataExplorer',
        component: () => import('../views/DataExplorer.vue'),
        meta: { requiresAuth: true, title: '数据管理-addp' }
      },
      {
        path: 'fulltext-search',
        name: 'FullTextSearch',
        component: () => import('../views/FullTextSearch.vue'),
        meta: { requiresAuth: true, title: '全文检索-addp' }
      }
      ,
      {
        path: 'spatial-preview',
        name: 'SpatialPreview',
        component: () => import('../views/SpatialPreview.vue'),
        meta: { requiresAuth: true, title: '空间预览-addp' }
      }
    ]
  }
]

const router = createRouter({
  history: createWebHistory(import.meta.env.DEV ? '/' : '/manager/'),
  routes
})

// 路由守卫：支持两种运行模式（Portal 嵌入 + 独立访问）
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
      return next({ name: 'Login' })
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
      return next({ name: 'Login' })
    }
  }

  // 3️⃣ 检测是否在 iframe 中（跳过登录页检查）
  const isInIframe = window.self !== window.top
  if (isInIframe && authStore.isAuthenticated) {
    return next()
  }

  // 4️⃣ 正常的路由守卫逻辑
  if (to.meta.requiresAuth && !authStore.isAuthenticated) {
    next('/login')
  } else {
    next()
  }
})

const DEFAULT_TITLE = '数据管理-addp'

router.afterEach((to) => {
  if (typeof document === 'undefined') {
    return
  }
  const pageTitle = typeof to.meta?.title === 'string' ? to.meta.title : DEFAULT_TITLE
  document.title = pageTitle
})

export default router
