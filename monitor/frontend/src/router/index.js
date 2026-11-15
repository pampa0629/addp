import { createRouter, createWebHistory } from 'vue-router'
import { createAuthGuard } from '@common-ui'
import { resolveModuleLandingRoute } from '../../../../common-frontend/basic/src/authorization/consoleRouteAccess.js'
import { useAuthStore } from '../store/auth'

const routes = [
  {
    path: '/login',
    name: 'Login',
    component: () => import('../views/Login.vue'),
    meta: { requiresAuth: false }
  },
  {
    path: '/',
    name: 'MonitorHome',
    meta: { handlesForbidden: true },
    beforeEnter: () => {
      const authStore = useAuthStore()
      return resolveModuleLandingRoute('/monitor', ['/node-resources', '/service-resources', '/engine-resources', '/http-quality', '/traces', '/monitoring-targets', '/dashboard', '/executions', '/platform-alerts', '/alerts', '/notifications', '/cdc-resources'], authStore.contextType, authStore.permissions)
    }
  },
  { path: '/platform-alerts', name: 'PlatformAlerts', component: () => import('../views/PlatformAlerts.vue') },
  { path: '/traces', name: 'Traces', component: () => import('../views/Traces.vue') },
  { path: '/http-quality', name: 'HTTPQuality', component: () => import('../views/HTTPQuality.vue') },
  { path: '/cdc-resources', name: 'CDCResources', component: () => import('../views/CDCResources.vue') },
  { path: '/resource-overview', name: 'ResourceOverview', component: () => import('../views/ResourceOverview.vue') },
  { path: '/engine-resources', name: 'EngineResources', component: () => import('../views/EngineResources.vue') },
  {
    path: '/service-resources', name: 'ProcessResources', component: () => import('../views/ProcessResources.vue')
  },
  {
    path: '/node-resources', name: 'NodeResources', component: () => import('../views/NodeResources.vue')
  },
  {
    path: '/node-resources/:node_id', name: 'NodeResourceDetail', component: () => import('../views/NodeResources.vue')
  },
  {
    path: '/monitoring-targets', name: 'MonitoringTargets', component: () => import('../views/MonitoringTargets.vue')
  },
  {
    path: '/monitoring-targets/new', name: 'MonitoringTargetNew', component: () => import('../views/MonitoringTargets.vue')
  },
  {
    path: '/monitoring-targets/:id', name: 'MonitoringTargetDetail', component: () => import('../views/MonitoringTargets.vue')
  },
  {
    path: '/dashboard',
    name: 'Dashboard',
    component: () => import('../views/Dashboard.vue')
  },
  {
    path: '/executions',
    name: 'ExecutionList',
    component: () => import('../views/ExecutionList.vue')
  },
  {
    path: '/alerts',
    name: 'AlertList',
    component: () => import('../views/AlertList.vue')
  },
  {
    path: '/notifications',
    name: 'NotificationList',
    component: () => import('../views/NotificationList.vue')
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
