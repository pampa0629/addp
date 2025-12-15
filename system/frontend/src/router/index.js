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
        path: 'users',
        name: 'Users',
        component: () => import('../views/Users.vue'),
        meta: { requiresAuth: true, title: '系统管理-addp' }
      },
      {
        path: 'logs',
        name: 'Logs',
        component: () => import('../views/Logs.vue'),
        meta: { requiresAuth: true, title: '系统管理-addp' }
      },
      {
        path: 'resources',
        name: 'Resources',
        component: () => import('../views/Resources.vue'),
        meta: { requiresAuth: true, title: '系统管理-addp' }
      },
      {
        path: 'applications',
        name: 'Applications',
        component: () => import('../views/Applications.vue'),
        meta: { requiresAuth: true, title: '应用管理-addp' }
      },
      {
        path: 'developer',
        name: 'Developer',
        component: () => import('../views/Developer.vue'),
        meta: { requiresAuth: true, title: '系统管理-addp' }
      },
      {
        path: 'dev',
        redirect: '/developer'
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
