import { createRouter, createWebHistory } from 'vue-router'
import { createAuthGuard } from '@common-ui'
import { resolveModuleLandingRoute } from '../../../../common-frontend/basic/src/authorization/consoleRouteAccess.js'
import { useAuthStore } from '../store/auth'
import Layout from '../components/Layout.vue'
import Login from '../views/Login.vue'

const routes = [
  {
    path: '/portal/login',
    name: 'Login',
    component: Login,
    meta: { requiresAuth: false }
  },
  {
    path: '/portal',
    component: Layout,
    meta: { requiresAuth: true },
    children: [
      {
        path: '',
        name: 'PortalRoot',
        beforeEnter: () => {
          const authStore = useAuthStore()
          return resolveModuleLandingRoute('', ['/portal/home', '/portal/my/applications'], authStore.contextType, authStore.permissions)
        },
        meta: { requiresAuth: true, handlesForbidden: true }
      },
      {
        path: 'home',
        name: 'Home',
        component: () => import('../views/Home.vue'),
        meta: { requiresAuth: true, title: '门户首页' }
      },
      {
        path: 'search',
        name: 'Search',
        component: () => import('../views/Search.vue'),
        meta: { requiresAuth: true, title: '搜索结果' }
      },
      {
        path: 'categories/:id',
        name: 'Category',
        component: () => import('../views/Category.vue'),
        meta: { requiresAuth: true, title: '资产目录浏览' }
      },
      {
        path: 'assets/:id',
        name: 'AssetDetail',
        component: () => import('../views/AssetDetail.vue'),
        meta: { requiresAuth: true, title: '资产详情' }
      },
      {
        path: 'my/applications',
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
  router,
  moduleName: 'Portal',
  loginRouteName: 'Login',
  homeRoute: '/portal/'
}))

export default router
