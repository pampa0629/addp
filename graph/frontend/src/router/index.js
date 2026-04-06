import { createRouter, createWebHistory } from 'vue-router'
import { createAuthGuard } from '@common-ui'
import { useAuthStore } from '../store/auth'
import Layout from '../components/Layout.vue'

const routes = [
  {
    path: '/login',
    name: 'Login',
    component: () => import('../views/Login.vue'),
    meta: { public: true, title: '登录-知识图谱' }
  },
  {
    path: '/',
    component: Layout,
    redirect: '/ontologies',
    meta: { requiresAuth: true },
    children: [
      {
        path: 'ontologies',
        name: 'OntologyList',
        component: () => import('../views/OntologyList.vue'),
        meta: { requiresAuth: true, title: '本体建模' }
      },
      {
        path: 'ontologies/create',
        name: 'OntologyCreate',
        component: () => import('../views/OntologyForm.vue'),
        meta: { requiresAuth: true, title: '新建本体' }
      },
      {
        path: 'ontologies/:id/edit',
        name: 'OntologyEdit',
        component: () => import('../views/OntologyForm.vue'),
        meta: { requiresAuth: true, title: '编辑本体' }
      },
      {
        path: 'ontologies/:id',
        name: 'OntologyDetail',
        component: () => import('../views/OntologyDetail.vue'),
        meta: { requiresAuth: true, title: '本体详情' }
      },
      {
        path: 'graphs',
        name: 'GraphList',
        component: () => import('../views/KnowledgeGraphList.vue'),
        meta: { requiresAuth: true, title: '知识图谱' }
      },
      {
        path: 'graphs/:id/browse',
        name: 'GraphBrowser',
        component: () => import('../views/GraphBrowser.vue'),
        meta: { requiresAuth: true, title: '图谱浏览器' }
      },
      {
        path: 'graphs/:id/build',
        name: 'BuildManager',
        component: () => import('../views/BuildManager.vue'),
        props: route => ({ graphId: route.params.id }),
        meta: { requiresAuth: true, title: '图谱构建' }
      },
      {
        path: 'graphs/:id/build/tasks/:tid',
        name: 'BuildTaskDetail',
        component: () => import('../views/BuildTaskDetail.vue'),
        meta: { requiresAuth: true, title: '构建任务详情' }
      },
      {
        path: 'graphs/:id/review',
        name: 'ReviewQueue',
        component: () => import('../views/ReviewQueue.vue'),
        meta: { requiresAuth: true, title: '审核队列' }
      },
      {
        path: 'analysis',
        name: 'GraphAnalysis',
        component: () => import('../views/GraphAnalysis.vue'),
        meta: { requiresAuth: true, title: '图算法分析' }
      },
      {
        path: 'knowledge-service',
        name: 'KnowledgeService',
        component: () => import('../views/KnowledgeService.vue'),
        meta: { requiresAuth: true, title: '知识服务' }
      }
    ]
  }
]

const router = createRouter({
  history: createWebHistory(import.meta.env.DEV ? '/' : '/graph/'),
  routes
})

router.beforeEach(createAuthGuard(useAuthStore, {
  moduleName: 'Graph',
  loginRouteName: 'Login'
}))

const DEFAULT_TITLE = '知识图谱'

router.afterEach((to) => {
  if (typeof document === 'undefined') return
  const pageTitle = typeof to.meta?.title === 'string' ? to.meta.title : DEFAULT_TITLE
  document.title = pageTitle
})

export default router
