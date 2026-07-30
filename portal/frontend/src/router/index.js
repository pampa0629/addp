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
    redirect: '/portal/home',
    meta: { requiresAuth: true },
    children: [
      {
        path: 'portal/home',
        name: 'Home',
        component: () => import('../views/Home.vue'),
        meta: { requiresAuth: true, title: '门户首页' }
      },
      {
        path: 'portal/search',
        name: 'Search',
        component: () => import('../views/Search.vue'),
        meta: { requiresAuth: true, title: '搜索结果' }
      },
      {
        path: 'portal/catalogs/:id',
        name: 'Catalog',
        component: () => import('../views/Catalog.vue'),
        meta: { requiresAuth: true, title: '目录浏览' }
      },
      {
        path: 'portal/assets/:id',
        name: 'AssetDetail',
        component: () => import('../views/AssetDetail.vue'),
        meta: { requiresAuth: true, title: '资产详情' }
      },
      {
        path: 'portal/my/applications',
        name: 'MyApplications',
        component: () => import('../views/MyApplications.vue'),
        meta: { requiresAuth: true, title: '我的申请与授权' }
      }
    ]
  }
]

const router = createRouter({
  history: createWebHistory('/'),
  routes
})

router.beforeEach(createAuthGuard(useAuthStore, {
  moduleName: 'Portal',
  loginRouteName: 'Login'
}))

export default router
