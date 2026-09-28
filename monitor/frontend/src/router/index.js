import { createRouter, createWebHistory } from 'vue-router'
import { createAuthGuard } from '@common-ui'
import { resolveModuleLandingRoute } from '../../../../common-frontend/basic/src/authorization/consoleRouteAccess.js'
import { useAuthStore } from '../store/auth'
import Dashboard from '../views/Dashboard.vue'
import ExecutionList from '../views/ExecutionList.vue'
import AlertList from '../views/AlertList.vue'
import NotificationList from '../views/NotificationList.vue'
import Login from '../views/Login.vue'

const routes = [
  {
    path: '/login',
    name: 'Login',
    component: Login,
    meta: { requiresAuth: false }
  },
  {
    path: '/',
    name: 'MonitorHome',
    meta: { handlesForbidden: true },
    beforeEnter: () => {
      const authStore = useAuthStore()
      return resolveModuleLandingRoute('/monitor', ['/dashboard', '/executions', '/alerts', '/notifications'], authStore.contextType, authStore.permissions)
    }
  },
  {
    path: '/dashboard',
    name: 'Dashboard',
    component: Dashboard
  },
  {
    path: '/executions',
    name: 'ExecutionList',
    component: ExecutionList
  },
  {
    path: '/alerts',
    name: 'AlertList',
    component: AlertList
  },
  {
    path: '/notifications',
    name: 'NotificationList',
    component: NotificationList
  }
]

const router = createRouter({
  history: createWebHistory(import.meta.env.BASE_URL),
  routes
})

// 使用标准认证守卫
router.beforeEach(createAuthGuard(useAuthStore, {
  router, moduleName: 'Monitor',
  loginRouteName: 'Login'
}))

export default router
