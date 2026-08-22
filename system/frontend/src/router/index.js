import { createRouter, createWebHistory } from 'vue-router'
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
    component: () => import('../views/SystemLayout.vue'),
    meta: { requiresAuth: true, title: '系统管理-addp' },
    children: [
      {
        path: '',
        name: 'Home',
        component: () => import('../views/Home.vue'),
        meta: { requiresAuth: true, title: '系统管理-addp' }
      },
      {
        path: 'iam',
        name: 'IAMWorkbench',
        component: () => import('../views/IAMWorkbench.vue'),
        meta: {
          requiresAuth: true,
          title: '身份与访问管理-addp'
        }
      },
      {
        path: 'settings/security-policy',
        name: 'SecurityPolicy',
        component: () => import('../views/SecurityPolicy.vue'),
        meta: {
          requiresAuth: true,
          title: 'IAM 安全策略-addp',
          requiredPermissions: ['iam.security_policy.read']
        }
      },
      {
        path: 'modules',
        name: 'Modules',
        component: () => import('../views/Modules.vue'),
        meta: { requiresAuth: true, title: '模块管理-addp', requiredPermissions: ['platform.module.read'] }
      },
      {
        path: 'engines',
        name: 'Engines',
        component: () => import('../views/Engines.vue'),
        meta: { requiresAuth: true, title: '系统管理-addp', requiredPermissions: ['system.engine.read'] }
      },
      {
        path: 'engines/:id',
        name: 'EngineDetail',
        component: () => import('../views/Engines.vue'),
        meta: { requiresAuth: true, title: '系统管理-addp', requiredPermissions: ['system.engine.read'] }
      },
      {
        path: 'applications',
        name: 'Applications',
        component: () => import('../views/Applications.vue'),
        meta: { requiresAuth: true, title: '应用管理-addp', requiredPermissions: ['system.application.read'] }
      },
      {
        path: 'cleanup',
        name: 'CleanupManager',
        component: () => import('../views/CleanupManager.vue'),
        meta: { requiresAuth: true, title: '资源回收-addp', requiredPermissions: ['system.cleanup.read'] }
      }
    ]
  }
]

const router = createRouter({
  history: createWebHistory(import.meta.env.DEV ? '/' : '/system/'),
  routes
})

import { createAuthGuard } from '@common-ui'

router.beforeEach(createAuthGuard(useAuthStore, {
  moduleName: 'System',
  loginRouteName: 'Login'
}))

router.beforeEach((to) => {
  const authStore = useAuthStore()
  if (!authStore.isAuthenticated) return true
  const required = to.meta?.requiredPermissions || []
  const any = to.meta?.anyPermissions || []
  if (required.some((permission) => !authStore.hasPermission(permission))) return { name: 'Home' }
  if (any.length && !authStore.hasAnyPermission(any)) return { name: 'Home' }
  return true
})

const DEFAULT_TITLE = '系统管理-addp'

router.afterEach((to) => {
  if (typeof document === 'undefined') {
    return
  }
  const pageTitle = typeof to.meta?.title === 'string' ? to.meta.title : DEFAULT_TITLE
  document.title = pageTitle
})

export default router
