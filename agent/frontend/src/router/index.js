import { createRouter, createWebHistory } from 'vue-router'
import { createAuthGuard } from '@common-ui'
import { useAuthStore } from '../store/auth'

const routes = [
  {
    path: '/agent/login',
    name: 'Login',
    component: () => import('../views/Login.vue'),
  },
  {
    path: '/agent',
    name: 'Chat',
    component: () => import('../views/ChatView.vue'),
    meta: { requiresAuth: true },
  },
  {
    path: '/',
    redirect: '/agent',
  },
]

const router = createRouter({
  history: createWebHistory(),
  routes,
})

router.beforeEach(createAuthGuard(useAuthStore, {
  moduleName: 'Agent',
  loginRouteName: 'Login'
}))

export default router
