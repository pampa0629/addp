import { createRouter, createWebHistory } from 'vue-router'
import { createAuthGuard } from '@common-ui'
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
    redirect: '/dashboard'
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
  history: createWebHistory(import.meta.env.DEV ? '/' : '/monitor/'),
  routes
})

// 使用标准认证守卫
router.beforeEach(createAuthGuard(useAuthStore, {
  moduleName: 'Monitor',
  loginRouteName: 'Login'
}))

export default router
