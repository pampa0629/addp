import { createRouter, createWebHistory } from 'vue-router'
import { createAuthGuard } from '@common-ui'
import { resolveModuleLandingRoute } from '../../../../common-frontend/basic/src/authorization/consoleRouteAccess.js'
import { useAuthStore } from '../store/auth'
import Layout from '../components/Layout.vue'

const children = [
  {
    path: '',
    name: 'SecurityHome',
    beforeEnter: () => {
      const authStore = useAuthStore()
      return resolveModuleLandingRoute('/security', ['/classification-grading', '/sensitive-data-definitions', '/protection-enrollments'], authStore.contextType, authStore.permissions)
    },
    meta: { requiresAuth: true, handlesForbidden: true }
  },
  { path: 'classification-grading', component: () => import('../views/ClassificationGrading.vue'), meta: { requiresAuth: true } },
  { path: 'sensitive-data-definitions', component: () => import('../views/SensitiveDataDefinitions.vue'), meta: { requiresAuth: true } },
  { path: 'protection-enrollments', component: () => import('../views/ProtectionEnrollmentList.vue'), meta: { requiresAuth: true } }
]

const router = createRouter({ history: createWebHistory(import.meta.env.BASE_URL), routes: [
  { path: '/login', name: 'Login', component: () => import('../views/Login.vue'), meta: { requiresAuth: false } },
  { path: '/', component: Layout, meta: { requiresAuth: true }, children }
] })
router.beforeEach(createAuthGuard(useAuthStore, { router, moduleName: 'Security', loginRouteName: 'Login' }))
export default router
