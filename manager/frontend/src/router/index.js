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
    component: () => import('../components/Layout.vue'),
    meta: { requiresAuth: true, title: '数据管理-addp' },
    children: [
      {
        path: '',
        redirect: 'data-explorer'
      },
      {
        path: 'data-explorer',
        name: 'DataExplorer',
        component: () => import('../views/DataExplorer.vue'),
        meta: { requiresAuth: true, title: '数据管理-addp' }
      },
      {
        path: 'fulltext-search',
        name: 'FullTextSearch',
        component: () => import('../views/FullTextSearch.vue'),
        meta: { requiresAuth: true, title: '全文检索-addp' }
      }
    ]
  }
]

const router = createRouter({
  history: createWebHistory('/manager/'),
  routes
})

router.beforeEach((to, from, next) => {
  const authStore = useAuthStore()

  // 检测是否在 iframe 中
  const isInIframe = window.self !== window.top

  // 如果在 iframe 中,跳过认证检查(开发模式)
  // 生产环境应该通过 URL 参数或 postMessage 传递 token
  if (isInIframe) {
    console.log('Manager Router: In iframe, bypassing auth check')
    next()
    return
  }

  if (to.meta.requiresAuth && !authStore.isAuthenticated) {
    next('/login')
  } else {
    next()
  }
})

const DEFAULT_TITLE = '数据管理-addp'

router.afterEach((to) => {
  if (typeof document === 'undefined') {
    return
  }
  const pageTitle = typeof to.meta?.title === 'string' ? to.meta.title : DEFAULT_TITLE
  document.title = pageTitle
})

export default router
