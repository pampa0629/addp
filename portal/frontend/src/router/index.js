import { createRouter, createWebHistory } from 'vue-router'
import { createAuthGuard } from '@common-ui'
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
    name: 'Portal',
    component: () => import('../views/Portal.vue'),
    meta: { requiresAuth: true, title: '门户-addp' }
  },
  {
    path: '/meta',
    redirect: '/meta/scan'
  }
]

const router = createRouter({
  history: createWebHistory(),
  routes
})

// 路由守卫:使用统一的 createAuthGuard
router.beforeEach(createAuthGuard(useAuthStore, {
  moduleName: 'Portal',
  loginRouteName: 'Login'
}))

const DEFAULT_TITLE = '门户-addp'

router.afterEach((to) => {
  if (typeof document === 'undefined') {
    return
  }
  const pageTitle = typeof to.meta?.title === 'string' ? to.meta.title : DEFAULT_TITLE
  document.title = pageTitle
})

export default router
