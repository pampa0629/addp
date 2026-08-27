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
    path: '/invitations/accept',
    name: 'TenantInvitationAccept',
    component: () => import('../views/TenantInvitationAccept.vue'),
    meta: { requiresAuth: false, title: 'ADDP' }
  },
  {
    path: '/',
    name: 'Console',
    component: () => import('../views/Portal.vue'),
    meta: { requiresAuth: true, title: '控制台-addp' }
  },
  {
    path: '/oauth/authorize',
    name: 'OAuthAuthorize',
    component: () => import('../views/OAuthAuthorize.vue'),
    meta: { requiresAuth: true, title: 'OAuth-addp' }
  },
  {
    path: '/oauth/device',
    name: 'OAuthDevice',
    component: () => import('../views/OAuthDevice.vue'),
    meta: { requiresAuth: true, title: 'OAuth-addp' }
  },
  {
    path: '/meta',
    redirect: '/meta/scan'
  },
  {
    path: '/:pathMatch(.*)*',
    name: 'ConsoleRoute',
    component: () => import('../views/Portal.vue'),
    meta: { requiresAuth: true, title: '控制台-addp' }
  }
]

const router = createRouter({
  history: createWebHistory(),
  routes
})

// 路由守卫:使用统一的 createAuthGuard
router.beforeEach(createAuthGuard(useAuthStore, {
  moduleName: 'Console',
  loginRouteName: 'Login'
}))

const DEFAULT_TITLE = '控制台-addp'

router.afterEach((to) => {
  if (typeof document === 'undefined') {
    return
  }
  const pageTitle = typeof to.meta?.title === 'string' ? to.meta.title : DEFAULT_TITLE
  document.title = pageTitle
})

export default router
