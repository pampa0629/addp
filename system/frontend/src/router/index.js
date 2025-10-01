import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '../store/auth'

const routes = [
  {
    path: '/login',
    name: 'Login',
    component: () => import('../views/Login.vue')
  },
  {
    path: '/',
    component: () => import('../views/SystemLayout.vue'),
    meta: { requiresAuth: true },
    children: [
      {
        path: '',
        name: 'Home',
        component: () => import('../views/Home.vue')
      },
      {
        path: 'users',
        name: 'Users',
        component: () => import('../views/Users.vue')
      },
      {
        path: 'logs',
        name: 'Logs',
        component: () => import('../views/Logs.vue')
      },
      {
        path: 'resources',
        name: 'Resources',
        component: () => import('../views/Resources.vue')
      },
      {
        path: 'developer',
        name: 'Developer',
        component: () => import('../views/Developer.vue')
      },
      {
        path: 'dev',
        redirect: '/developer'
      }
    ]
  }
]

const router = createRouter({
  history: createWebHistory(),
  routes
})

router.beforeEach(async (to, from, next) => {
  console.log('System Router: beforeEach triggered')
  console.log('  to.path:', to.path)
  console.log('  from.path:', from.path)

  const authStore = useAuthStore()
  console.log('  isAuthenticated:', authStore.isAuthenticated)
  console.log('  requiresAuth:', to.meta.requiresAuth)

  // 检测是否在 iframe 中
  const isInIframe = window.self !== window.top
  console.log('  isInIframe:', isInIframe)

  // 检查URL参数中是否有token（从portal传递过来）
  const urlToken = to.query.token
  if (urlToken) {
    // 如果URL中有token,始终使用URL中的token(可能是切换了用户)
    console.log('System Router: Found token in URL params, saving to auth store')
    authStore.setToken(urlToken)
    // 获取用户信息
    try {
      await authStore.fetchUser()
      console.log('System Router: User fetched successfully from token')
    } catch (error) {
      console.error('System Router: Failed to fetch user from token:', error)
      authStore.logout()
    }
  }

  // 如果在 iframe 中且已认证,跳过认证检查
  if (isInIframe && authStore.isAuthenticated) {
    console.log('System Router: In iframe with auth, allowing navigation')
    next()
    return
  }

  if (to.meta.requiresAuth && !authStore.isAuthenticated) {
    console.log('System Router: Redirecting to /login')
    next('/login')
  } else {
    console.log('System Router: Allowing navigation')
    next()
  }
})

export default router