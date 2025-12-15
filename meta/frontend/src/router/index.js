import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '../store/auth'

const normalizeRedirect = fullPath => {
  if (!fullPath) {
    return '/scan'
  }
  if (fullPath === '/meta' || fullPath === '/meta/') {
    return '/scan'
  }
  if (fullPath.startsWith('/meta/')) {
    return fullPath.replace('/meta', '') || '/scan'
  }
  return fullPath
}

const routes = [
  {
    path: '/login',
    name: 'Login',
    component: () => import('../views/Login.vue'),
    meta: { public: true, title: '登录-addp' }
  },
  {
    path: '/',
    component: () => import('../components/Layout.vue'),
    meta: { requiresAuth: true, title: '元数据-addp' },
    children: [
      {
        path: '',
        redirect: { name: 'MetadataScan' },
        meta: { requiresAuth: true }
      },
      {
        path: 'scan',
        name: 'MetadataScan',
        component: () => import('../views/MetadataScan.vue'),
        meta: { requiresAuth: true, title: '元数据-addp' }
      },
      {
        path: 'tasks',
        name: 'TaskMonitor',
        component: () => import('../views/TaskMonitor.vue'),
        meta: { requiresAuth: true, title: '元数据-addp' }
      }
    ]
  }
]

const router = createRouter({
  history: createWebHistory(import.meta.env.DEV ? '/' : '/meta/'),
  routes
})

// 路由守卫：检查登录状态
import { createAuthGuard } from '@common-ui'

router.beforeEach(createAuthGuard(useAuthStore, {
  moduleName: 'Meta',
  loginRouteName: 'Login',
  normalizeRedirect  // 传入自定义规范化函数
}))

const DEFAULT_TITLE = '元数据-addp'

router.afterEach((to) => {
  if (typeof document === 'undefined') {
    return
  }
  const pageTitle = typeof to.meta?.title === 'string' ? to.meta.title : DEFAULT_TITLE
  document.title = pageTitle
})

export default router
