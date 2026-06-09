import { createRouter, createWebHistory } from 'vue-router'
import Layout from '../components/Layout.vue'
import { useAuthStore } from '../store/auth'

const normalizeRedirect = fullPath => {
  if (!fullPath) {
    return '/orchestrations'
  }
  if (fullPath === '/' || fullPath === '/') {
    return '/orchestrations'
  }
  return fullPath
}

const routes = [
  {
    path: '/login',
    name: 'Login',
    component: () => import('../views/Login.vue'),
    meta: { public: true, title: '登录-编排引擎' }
  },
  {
    path: '/',
    component: Layout,
    redirect: '/orchestrations',
    meta: { requiresAuth: true },
    children: [
      {
        path: 'orchestrations',
        name: 'OrchestrationList',
        component: () => import('../views/OrchestrationList.vue'),
        meta: { requiresAuth: true, title: '编排任务列表' }
      },
      {
        path: 'orchestrations/new',
        name: 'OrchestrationCreate',
        component: () => import('../views/OrchestrationForm.vue'),
        meta: { requiresAuth: true, title: '创建编排任务' }
      },
      {
        path: 'orchestrations/:id/edit',
        name: 'OrchestrationEdit',
        component: () => import('../views/OrchestrationForm.vue'),
        meta: { requiresAuth: true, title: '编辑编排任务' }
      },
      {
        path: 'orchestrations/:id',
        redirect: to => ({
          name: 'OrchestrationEdit',
          params: { id: to.params.id },
          query: to.query
        })
      },
      {
        path: 'orchestrations/:id/executions',
        name: 'ExecutionList',
        component: () => import('../views/ExecutionList.vue'),
        meta: { requiresAuth: true, title: '执行历史' }
      },
      {
        path: 'executions',
        name: 'ExecutionRecords',
        component: () => import('../views/ExecutionRecords.vue'),
        meta: { requiresAuth: true, title: '执行记录' }
      }
    ]
  }
]

const router = createRouter({
  history: createWebHistory(import.meta.env.DEV ? '/' : '/orchestrator/'),
  routes
})

// 路由守卫：支持两种运行模式
import { createAuthGuard } from '@common-ui'

router.beforeEach(createAuthGuard(useAuthStore, {
  moduleName: 'Orchestrator',
  loginRouteName: 'Login',
  normalizeRedirect
}))

const DEFAULT_TITLE = '编排引擎'

router.afterEach((to) => {
  if (typeof document === 'undefined') {
    return
  }
  const pageTitle = typeof to.meta?.title === 'string' ? to.meta.title : DEFAULT_TITLE
  document.title = pageTitle
})

export default router
