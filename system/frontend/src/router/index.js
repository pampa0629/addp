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

router.beforeEach((to, from, next) => {
  console.log('System Router: beforeEach triggered')
  console.log('  to.path:', to.path)
  console.log('  from.path:', from.path)

  const authStore = useAuthStore()
  console.log('  isAuthenticated:', authStore.isAuthenticated)
  console.log('  requiresAuth:', to.meta.requiresAuth)

  // 检测是否在 iframe 中
  const isInIframe = window.self !== window.top
  console.log('  isInIframe:', isInIframe)

  // 如果在 iframe 中,跳过认证检查(开发模式)
  // 生产环境应该通过 URL 参数或 postMessage 传递 token
  if (isInIframe) {
    console.log('System Router: In iframe, bypassing auth check')
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