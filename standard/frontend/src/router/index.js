import { createRouter, createWebHistory } from 'vue-router'
import { createAuthGuard } from '@common-ui'
import { useAuthStore } from '../store/auth'
import Layout from '../components/Layout.vue'
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
    component: Layout,
    redirect: '/domains',
    meta: { requiresAuth: true },
    children: [
      {
        path: 'domains',
        name: 'DomainList',
        component: () => import('../views/DomainList.vue'),
        meta: { requiresAuth: true, title: '业务域管理' }
      },
      {
        path: 'collections',
        name: 'StandardCollectionList',
        component: () => import('../views/StandardCollectionList.vue'),
        meta: { requiresAuth: true, title: '标准集管理' }
      },
      {
        path: 'collections/:id',
        name: 'StandardCollectionDetail',
        component: () => import('../views/StandardCollectionDetail.vue'),
        meta: { requiresAuth: true, title: '标准集详情' }
      },
      {
        path: 'glossaries',
        name: 'GlossaryList',
        component: () => import('../views/GlossaryList.vue'),
        meta: { requiresAuth: true, title: '业务术语词典' }
      },
      {
        path: 'glossaries/:id',
        name: 'GlossaryDetail',
        component: () => import('../views/GlossaryDetail.vue'),
        meta: { requiresAuth: true, title: '业务术语详情' }
      },
      {
        path: 'elements',
        name: 'ElementList',
        component: () => import('../views/ElementList.vue'),
        meta: { requiresAuth: true, title: '数据元管理' }
      },
      {
        path: 'elements/:id',
        name: 'ElementDetail',
        component: () => import('../views/ElementDetail.vue'),
        meta: { requiresAuth: true, title: '数据元详情' }
      },
      {
        path: 'code-sets',
        name: 'CodeSetList',
        component: () => import('../views/CodeSetList.vue'),
        meta: { requiresAuth: true, title: '码值集管理' }
      },
      {
        path: 'code-sets/:id',
        name: 'CodeSetDetail',
        component: () => import('../views/CodeSetDetail.vue'),
        meta: { requiresAuth: true, title: '码值集详情' }
      },
      {
        path: 'units',
        name: 'UnitList',
        component: () => import('../views/UnitList.vue'),
        meta: { requiresAuth: true, title: '计量单位' }
      },
      {
        path: 'metrics',
        name: 'MetricList',
        component: () => import('../views/MetricList.vue'),
        meta: { requiresAuth: true, title: '指标管理' }
      },
      {
        path: 'metrics/:id',
        name: 'MetricDetail',
        component: () => import('../views/MetricDetail.vue'),
        meta: { requiresAuth: true, title: '指标详情' }
      },
      {
        path: 'documents',
        name: 'DocumentList',
        component: () => import('../views/DocumentList.vue'),
        meta: { requiresAuth: true, title: '全局文档库' }
      },
      {
        path: 'documents/:id',
        name: 'DocumentDetail',
        component: () => import('../views/DocumentDetail.vue'),
        meta: { requiresAuth: true, title: '标准文档详情' }
      }
    ]
  }
]

const router = createRouter({
  history: createWebHistory(import.meta.env.DEV ? '/' : '/standard/'),
  routes
})

router.beforeEach(createAuthGuard(useAuthStore, {
  moduleName: 'Standard',
  loginRouteName: 'Login'
}))

export default router
