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
    redirect: '/asset/assets',
    meta: { requiresAuth: true },
    children: [
      {
        path: 'asset/type-definitions',
        name: 'TypeDefinitionList',
        component: () => import('../views/TypeDefinitionList.vue'),
        meta: { requiresAuth: true, title: '资产类型' }
      },
      {
        path: 'asset/categories',
        name: 'CatalogManagement',
        component: () => import('../views/CatalogManagement.vue'),
        meta: { requiresAuth: true, title: '目录管理' }
      },
      {
        path: 'asset/assets',
        name: 'AssetManager',
        component: () => import('../views/AssetManager.vue'),
        meta: { requiresAuth: true, title: '资产管理' }
      },
      {
        path: 'asset/applications',
        name: 'ApplicationList',
        component: () => import('../views/ApplicationList.vue'),
        meta: { requiresAuth: true, title: '申请与授权' }
      },
      {
        path: 'asset/dashboard',
        name: 'Dashboard',
        component: () => import('../views/Dashboard.vue'),
        meta: { requiresAuth: true, title: '运营看板' }
      }
    ]
  }
]

const router = createRouter({
  history: createWebHistory(import.meta.env.DEV ? '/' : '/asset/'),
  routes
})

router.beforeEach(createAuthGuard(useAuthStore, {
  moduleName: 'Asset',
  loginRouteName: 'Login'
}))

export default router
