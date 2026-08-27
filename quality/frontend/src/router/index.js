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
    redirect: '/check-tasks',
    meta: { requiresAuth: true },
    children: [
      {
        path: 'rule-applications',
        name: 'RuleApplicationList',
        component: () => import('../views/RuleApplicationList.vue'),
        meta: { requiresAuth: true, title: '规则应用配置' }
      },
      {
        path: 'check-tasks',
        name: 'CheckTaskList',
        component: () => import('../views/CheckTaskList.vue'),
        meta: { requiresAuth: true, title: '检查任务' }
      },
      {
        path: 'materialization-gate-tasks',
        name: 'MaterializationGateTaskList',
        component: () => import('../views/MaterializationGateTaskList.vue'),
        meta: { requiresAuth: true, title: '物化门禁任务' }
      },
      {
        path: 'executions',
        name: 'ExecutionList',
        component: () => import('../views/ExecutionList.vue'),
        meta: { requiresAuth: true, title: '执行记录' }
      },
      {
        path: 'executions/:execution_id',
        name: 'ExecutionDetail',
        component: () => import('../views/ExecutionDetail.vue'),
        meta: { requiresAuth: true, title: '执行详情' }
      },
      {
        path: 'issues',
        name: 'IssueList',
        component: () => import('../views/IssueList.vue'),
        meta: { requiresAuth: true, title: '问题工单' }
      },
      {
        path: 'issues/:id',
        name: 'IssueDetail',
        component: () => import('../views/IssueDetail.vue'),
        meta: { requiresAuth: true, title: '问题工单详情' }
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
