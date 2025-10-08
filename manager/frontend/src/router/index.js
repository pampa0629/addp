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
    component: () => import('../components/Layout.vue'),
    meta: { requiresAuth: true },
    children: [
      {
        path: '',
        redirect: 'data-explorer'
      },
      {
        path: 'data-explorer',
        name: 'DataExplorer',
        component: () => import('../views/DataExplorer.vue')
      }
    ]
  }
]

const router = createRouter({
  history: createWebHistory('/manager/'),
  routes
})

router.beforeEach((to, from, next) => {
  const authStore = useAuthStore()

  // 检测是否在 iframe 中
  const isInIframe = window.self !== window.top

  // 如果在 iframe 中,跳过认证检查(开发模式)
  // 生产环境应该通过 URL 参数或 postMessage 传递 token
  if (isInIframe) {
    console.log('Manager Router: In iframe, bypassing auth check')
    next()
    return
  }

  if (to.meta.requiresAuth && !authStore.isAuthenticated) {
    next('/login')
  } else {
    next()
  }
})

export default router
