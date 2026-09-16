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
    redirect: '/plans',
    meta: { requiresAuth: true },
    children: [
      {
        path: 'rules', name: 'RuleList', component: () => import('../views/RuleList.vue'),
        meta: { requiresAuth: true, title: '质量规则', requiredPermissions: ['quality.rule.read'] }
      },
      {
        path: 'plans',
        name: 'PlanList',
        component: () => import('../views/PlanList.vue'),
        meta: { requiresAuth: true, title: '质量检查方案', requiredPermissions: ['quality.plan.read'] }
      },
      {
        path: 'executions/:execution_id',
        name: 'ExecutionDetail',
        component: () => import('../views/ExecutionDetail.vue'),
        meta: { requiresAuth: true, title: '执行详情', requiredPermissions: ['monitor.execution.read'] }
      },
      {
        path: 'issues',
        name: 'IssueList',
        component: () => import('../views/IssueList.vue'),
        meta: { requiresAuth: true, title: '问题工单', requiredPermissions: ['quality.issue.read'] }
      },
      {
        path: 'issues/:id',
        name: 'IssueDetail',
        component: () => import('../views/IssueDetail.vue'),
        meta: { requiresAuth: true, title: '问题工单详情', requiredPermissions: ['quality.issue.read'] }
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

router.beforeEach((to) => {
  const authStore = useAuthStore()
  if (!authStore.isAuthenticated) return true
  const required = to.meta?.requiredPermissions || []
  if (!required.some(permission => !authStore.hasPermission(permission))) return true

  const fallback = routes[1].children.find(route =>
    (route.meta?.requiredPermissions || []).every(permission => authStore.hasPermission(permission))
  )
  if (fallback && to.name !== fallback.name) return { name: fallback.name }
  return false
})

export default router
