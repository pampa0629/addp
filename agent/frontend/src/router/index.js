import { createRouter, createWebHistory } from 'vue-router'
import { createAuthGuard } from '@common-ui'
import { useAuthStore } from '../store/auth'

const routes = [
  {
    path: '/login',
    name: 'Login',
    component: () => import('../views/Login.vue'),
  },
  {
    path: '/',
    name: 'Chat',
    component: () => import('../views/ChatView.vue'),
    meta: { requiresAuth: true },
  },
  {
    path: '/sessions/:session_id',
    name: 'ChatSession',
    component: () => import('../views/ChatView.vue'),
    meta: { requiresAuth: true },
  },
]

const router = createRouter({
  history: createWebHistory(import.meta.env.DEV ? '/' : '/agent/'),
  routes,
})

router.beforeEach(createAuthGuard(useAuthStore, {
  moduleName: 'Agent',
  loginRouteName: 'Login'
}))

export default router
