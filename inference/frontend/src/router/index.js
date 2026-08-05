import { createRouter, createWebHistory } from 'vue-router'
import { createAuthGuard } from '@common-ui'
import { useAuthStore } from '../store/auth'

const router = createRouter({
  history: createWebHistory(import.meta.env.DEV ? '/' : '/inference/'),
  routes: [
    {
      path: '/login',
      name: 'Login',
      component: () => import('../views/Login.vue'),
      meta: { requiresAuth: false }
    },
    {
      path: '/',
      redirect: '/settings/models'
    },
    {
      path: '/settings/models',
      name: 'ModelSettings',
      component: () => import('../views/ModelSettings.vue'),
      meta: { requiresAuth: true }
    }
  ]
})

router.beforeEach(createAuthGuard(useAuthStore, {
  moduleName: 'Inference',
  loginRouteName: 'Login'
}))

export default router
