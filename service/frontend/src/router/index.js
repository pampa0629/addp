import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '../stores/auth'

const normalizeRedirect = fullPath => {
  if (!fullPath) {
    return '/query-services'
  }
  if (fullPath === '/' || fullPath === '/') {
    return '/query-services'
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
    redirect: '/query-services'
  },

  // === 查询服务路由 ===
  {
    path: '/query-services',
    name: 'QueryServiceList',
    component: () => import('../views/QueryServiceList.vue'),
    meta: { requiresAuth: true, title: '查询服务' }
  },
  {
    path: '/query-services/create',
    name: 'QueryServiceCreate',
    component: () => import('../views/QueryServiceForm.vue'),
    meta: { requiresAuth: true, title: '创建查询服务' }
  },
  {
    path: '/query-services/:id/edit',
    name: 'QueryServiceEdit',
    component: () => import('../views/QueryServiceForm.vue'),
    meta: { requiresAuth: true, title: '编辑查询服务' }
  },
  {
    path: '/query-services/:id',
    name: 'QueryServiceDetail',
    component: () => import('../views/QueryServiceDetail.vue'),
    meta: { requiresAuth: true, title: '查询服务详情' }
  },

  // === 注册服务路由（新架构） ===
  {
    path: '/registered-services',
    name: 'RegisteredServiceList',
    component: () => import('../views/RegisteredServiceList.vue'),
    meta: { requiresAuth: true, title: '注册服务' }
  },
  {
    path: '/registered-services/create',
    name: 'RegisteredServiceCreate',
    component: () => import('../views/RegisteredServiceForm.vue'),
    meta: { requiresAuth: true, title: '注册外部服务' }
  },
  {
    path: '/registered-services/:id/edit',
    name: 'RegisteredServiceEdit',
    component: () => import('../views/RegisteredServiceForm.vue'),
    meta: { requiresAuth: true, title: '编辑注册服务' }
  },
  {
    path: '/registered-services/:id',
    name: 'RegisteredServiceDetail',
    component: () => import('../views/RegisteredServiceDetail.vue'),
    meta: { requiresAuth: true, title: '注册服务详情' }
  },

  // === 服务发布路由（原"空间服务"） ===
  {
    path: '/published-services',
    name: 'PublishedServiceList',
    component: () => import('../views/PublishedServiceList.vue'),
    meta: { requiresAuth: true, title: '服务发布' }
  },
  {
    path: '/published-services/create',
    name: 'PublishedServiceCreate',
    component: () => import('../views/PublishedServiceForm.vue'),
    meta: { requiresAuth: true, title: '创建服务' }
  },
  {
    path: '/published-services/:id/edit',
    name: 'PublishedServiceEdit',
    component: () => import('../views/PublishedServiceForm.vue'),
    meta: { requiresAuth: true, title: '编辑服务' }
  },
  {
    path: '/published-services/:id',
    name: 'PublishedServiceDetail',
    component: () => import('../views/PublishedServiceDetail.vue'),
    meta: { requiresAuth: true, title: '服务详情' }
  },
  {
    path: '/published-services/:id/test',
    name: 'PublishedServiceTest',
    component: () => import('../views/PublishedServiceTest.vue'),
    meta: { requiresAuth: true, title: '服务测试' }
  },

  // === 服务注册路由（保持不变） ===
  {
    path: '/services',
    name: 'ServiceManagement',
    component: () => import('../views/ServiceManagement.vue'),
    meta: { requiresAuth: true, title: '服务注册' }
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

  // === 服务目录路由（保持不变） ===
  {
    path: '/catalog',
    name: 'ServiceCatalog',
    component: () => import('../views/ServiceCatalog.vue'),
    meta: { requiresAuth: true, title: '服务目录' }
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
