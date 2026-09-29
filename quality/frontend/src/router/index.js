import { createRouter, createWebHistory } from 'vue-router'
import { createAuthGuard } from '@common-ui'
import { resolveModuleLandingRoute } from '../../../../common-frontend/basic/src/authorization/consoleRouteAccess.js'
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
    meta: { requiresAuth: true },
    children: [
      {
        path: '',
        name: 'QualityHome',
        beforeEnter: () => {
          const authStore = useAuthStore()
          return resolveModuleLandingRoute('/quality', ['/overview', '/rules', '/plans', '/issues'], authStore.contextType, authStore.permissions)
        },
        meta: { requiresAuth: true, handlesForbidden: true }
      },
      {
        path: 'overview', name: 'QualityOverview', component: () => import('../views/Overview.vue'),
        meta: { requiresAuth: true, title: '质量概览' }
      },
      {
        path: 'rules', name: 'RuleList', component: () => import('../views/RuleList.vue'),
        meta: { requiresAuth: true, title: '质量规则' }
      },
      {
        path: 'plans',
        name: 'PlanList',
        component: () => import('../views/PlanList.vue'),
        meta: { requiresAuth: true, title: '质量检查方案' }
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
  history: createWebHistory(import.meta.env.BASE_URL),
  routes
})

router.beforeEach(createAuthGuard(useAuthStore, {
  router, moduleName: 'Quality',
  loginRouteName: 'Login'
}))

export default router
