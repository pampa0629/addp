import { createRouter, createWebHistory } from 'vue-router'
import Layout from '../views/Layout.vue'
import Login from '../views/Login.vue'
import { useAuthStore } from '../stores/auth'

const routes = [
  {
    path: '/login',
    name: 'Login',
    component: Login,
    meta: { requiresAuth: false }
  },
  {
    path: '/',
    component: Layout,
    redirect: '/sql',
    meta: { requiresAuth: true },
    children: [
      {
        path: 'sql',
        name: 'SQLEditor',
        component: () => import('../views/SQLEditor.vue'),
        meta: { requiresAuth: true, title: 'SQL 工作台' }
      },
      {
        path: 'gis-workflow',
        name: 'GISWorkflowEditor',
        component: () => import('../views/GISWorkflowEditor.vue'),
        meta: { requiresAuth: true, title: 'GIS 工作流编辑器' }
      },
      {
        path: 'gis-tasks',
        name: 'GISTasks',
        component: () => import('../views/GISTasks.vue'),
        meta: { requiresAuth: true, title: 'GIS 任务管理' }
      },
      {
        path: 'gis-executions',
        name: 'GISExecutions',
        component: () => import('../views/GISExecutions.vue'),
        meta: { requiresAuth: true, title: '执行历史' }
      },
      {
        path: 'gis-executions/:id',
        name: 'GISExecutionDetail',
        component: () => import('../views/GISExecutionDetail.vue'),
        meta: { requiresAuth: true, title: '执行详情' }
      },
      // 保留旧路由以兼容
      {
        path: 'spatial',
        redirect: '/gis-tasks'
      }
    ]
  }
]

const router = createRouter({
  history: createWebHistory(import.meta.env.DEV ? '/' : '/develop/'),
  routes
})

// 路由守卫：支持两种运行模式（Portal 嵌入 + 独立访问）
router.beforeEach(async (to, from, next) => {
  const authStore = useAuthStore()
  const queryToken = typeof to.query.token === 'string' ? to.query.token : null

  // 0️⃣ 如果正在加载用户信息，直接放行（避免重复处理）
  if (authStore.isLoadingUser) {
    console.log('[Router] User is loading, allowing navigation')
    return next()
  }

  // 1️⃣ 如果 URL 中有 token（Portal 传递）
  if (queryToken) {
    console.log('[Router] Found token in URL, processing...')
    authStore.setToken(queryToken)

    try {
      await authStore.fetchUser()
      console.log('[Router] User fetched successfully, user:', authStore.user?.username)
    } catch (error) {
      console.error('[Router] 获取用户信息失败:', error)
      authStore.logout()
      return next({ name: 'Login' })
    }

    // 确保用户信息已加载后，再清除 URL token
    console.log('[Router] Removing token from URL')
    const { token: _removed, ...restQuery } = to.query
    next({ path: to.path, query: restQuery, replace: true })
    return
  }

  // 2️⃣ 如果已认证但无用户信息，尝试刷新
  if (authStore.isAuthenticated && !authStore.user) {
    console.log('[Router] Authenticated but no user, fetching...')
    try {
      await authStore.fetchUser()
      console.log('[Router] User refreshed successfully')
    } catch (error) {
      console.error('[Router] 刷新用户信息失败:', error)
      authStore.logout()
      return next({ name: 'Login' })
    }
  }

  // 3️⃣ 检测是否在 iframe 中（跳过登录页检查）
  const isInIframe = window.self !== window.top
  if (isInIframe && authStore.isAuthenticated) {
    // 额外检查：如果在 iframe 中但 user 仍未加载，等待
    if (!authStore.user && !authStore.isLoadingUser) {
      console.log('[Router] In iframe but no user, fetching...')
      try {
        await authStore.fetchUser()
      } catch (error) {
        console.error('[Router] Failed to fetch user in iframe:', error)
        // 在 iframe 中失败时不跳转登录页，避免嵌套登录
        return next()
      }
    }
    return next()
  }

  // 4️⃣ 正常的路由守卫逻辑
  if (!to.meta.requiresAuth) {
    next()
    return
  }

  if (authStore.isAuthenticated) {
    next()
  } else {
    next('/login')
  }
})

export default router
