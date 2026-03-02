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
    redirect: '/quality/check-tasks',
    meta: { requiresAuth: true },
    children: [
      {
        path: 'quality/rule-applications',
        name: 'RuleApplicationList',
        component: () => import('../views/RuleApplicationList.vue'),
        meta: { requiresAuth: true, title: '规则应用配置' }
      },
      {
        path: 'quality/check-tasks',
        name: 'CheckTaskList',
        component: () => import('../views/CheckTaskList.vue'),
        meta: { requiresAuth: true, title: '检查任务' }
      },
      {
        path: 'quality/executions',
        name: 'ExecutionList',
        component: () => import('../views/ExecutionList.vue'),
        meta: { requiresAuth: true, title: '执行记录' }
      },
      {
        path: 'quality/executions/:id',
        name: 'ExecutionDetail',
        component: () => import('../views/ExecutionDetail.vue'),
        meta: { requiresAuth: true, title: '执行详情' }
      },
      {
        path: 'quality/issues',
        name: 'IssueList',
        component: () => import('../views/IssueList.vue'),
        meta: { requiresAuth: true, title: '问题工单' }
      }
    ]
  }
]

const router = createRouter({
  history: createWebHistory(import.meta.env.DEV ? '/' : '/quality/'),
  routes
})

router.beforeEach(createAuthGuard(useAuthStore, {
  moduleName: 'Quality',
  loginRouteName: 'Login'
}))

export default router
