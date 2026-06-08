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
        path: 'data-retrieval',
        name: 'DataRetrieval',
        component: () => import('../views/DataRetrieval.vue'),
        meta: { requiresAuth: true, title: '数据检索-addp' }
      },
      {
        path: 'vectorization-tasks',
        name: 'VectorizationTasks',
        component: () => import('../views/VectorizationTasks.vue'),
        meta: { requiresAuth: true, title: '向量化任务-addp' }
      },
      {
        path: 'mvt-tasks',
        name: 'MvtTasks',
        component: () => import('../views/MvtTasks.vue'),
        meta: { requiresAuth: true, title: 'MVT任务-addp' }
      },
      {
        path: 'spatial-preview',
        name: 'SpatialPreview',
        component: () => import('../views/SpatialPreview.vue'),
        meta: { requiresAuth: true, title: '空间预览-addp' }
      }
    ]
  }
]

const router = createRouter({
  history: createWebHistory(import.meta.env.DEV ? '/' : '/manager/'),
  routes
})

// 路由守卫：支持两种运行模式（Console 嵌入 + 独立访问）
import { createAuthGuard } from '@common-ui'

router.beforeEach(createAuthGuard(useAuthStore, {
  moduleName: 'Manager',
  loginRouteName: 'Login'
}))

const DEFAULT_TITLE = '数据管理-addp'

router.afterEach((to) => {
  if (typeof document === 'undefined') {
    return
  }
  const pageTitle = typeof to.meta?.title === 'string' ? to.meta.title : DEFAULT_TITLE
  document.title = pageTitle
})

export default router
