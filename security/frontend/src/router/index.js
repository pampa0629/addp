import { createRouter, createWebHistory } from 'vue-router'
import { createAuthGuard } from '@common-ui'
import { useAuthStore } from '../store/auth'
import Layout from '../components/Layout.vue'

const children = [
  { path: 'classification-grading', component: () => import('../views/ClassificationGrading.vue'), meta: { requiresAuth: true } },
  { path: 'sensitive-data-definitions', component: () => import('../views/SensitiveDataDefinitions.vue'), meta: { requiresAuth: true } },
  { path: 'protection-enrollments', component: () => import('../views/ProtectionEnrollmentList.vue'), meta: { requiresAuth: true } }
]

const router = createRouter({ history: createWebHistory(import.meta.env.DEV ? '/' : '/security/'), routes: [
  { path: '/login', name: 'Login', component: () => import('../views/Login.vue'), meta: { requiresAuth: false } },
  { path: '/', component: Layout, redirect: '/sensitive-data-definitions', meta: { requiresAuth: true }, children }
] })
router.beforeEach(createAuthGuard(useAuthStore, { moduleName: 'Security', loginRouteName: 'Login' }))
export default router
