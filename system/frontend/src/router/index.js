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
        meta: { requiresAuth: true, title: '身份与访问管理-addp' }
      },
      {
        path: 'engines',
        name: 'Engines',
        component: () => import('../views/Engines.vue'),
        meta: { requiresAuth: true, title: '系统管理-addp' }
      },
      {
        path: 'applications',
        name: 'Applications',
        component: () => import('../views/Applications.vue'),
        meta: { requiresAuth: true, title: '应用管理-addp' }
      },
      {
        path: 'cleanup',
        name: 'CleanupManager',
        component: () => import('../views/CleanupManager.vue'),
        meta: { requiresAuth: true, title: '资源回收-addp' }
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

const DEFAULT_TITLE = '系统管理-addp'

router.afterEach((to) => {
  if (typeof document === 'undefined') {
    return
  }
  const pageTitle = typeof to.meta?.title === 'string' ? to.meta.title : DEFAULT_TITLE
  document.title = pageTitle
})

export default router
