import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '../stores/auth'

const normalizeRedirect = fullPath => {
  if (!fullPath) {
    return '/services'
  }
  if (fullPath === '/' || fullPath === '/') {
    return '/services'
  }
  return fullPath
}

const routes = [
  {
    path: '/login',
    name: 'Login',
    component: () => import('../views/Login.vue'),
    meta: { public: true, title: '登录-数据服务' }
  },
  {
    path: '/',
    redirect: '/services'
  },
  {
    path: '/services',
    name: 'ServiceManagement',
    component: () => import('../views/ServiceManagement.vue'),
    meta: { requiresAuth: true, title: '服务管理' }
  },
  {
    path: '/services/create',
    name: 'ServiceCreate',
    component: () => import('../views/ServiceForm.vue'),
    meta: { requiresAuth: true, title: '创建服务' }
  },
  {
    path: '/services/:id/edit',
    name: 'ServiceEdit',
    component: () => import('../views/ServiceForm.vue'),
    meta: { requiresAuth: true, title: '编辑服务' }
  },
  {
    path: '/services/:id',
    name: 'ServiceDetail',
    component: () => import('../views/ServiceDetail.vue'),
    meta: { requiresAuth: true, title: '服务详情' }
  },
  {
    path: '/catalog',
    name: 'ServiceCatalog',
    component: () => import('../views/ServiceCatalog.vue'),
    meta: { requiresAuth: true, title: '服务目录' }
  },
  {
    path: '/query',
    name: 'DataQuery',
    component: () => import('../views/DataQuery.vue'),
    meta: { requiresAuth: true, title: '数据查询' }
  }
]

const router = createRouter({
  history: createWebHistory(import.meta.env.DEV ? '/' : '/service/'),
  routes
})

// 路由守卫：支持两种运行模式
import { createAuthGuard } from '@common-ui'

router.beforeEach(createAuthGuard(useAuthStore, {
  moduleName: 'Service',
  loginRouteName: 'Login',
  normalizeRedirect  // 传入自定义规范化函数
}))

const DEFAULT_TITLE = '数据服务'

router.afterEach((to) => {
  if (typeof document === 'undefined') {
    return
  }
  const pageTitle = typeof to.meta?.title === 'string' ? to.meta.title : DEFAULT_TITLE
  document.title = pageTitle
})

export default router
