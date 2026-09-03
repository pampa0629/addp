import { createRouter, createWebHistory } from 'vue-router'
import { createAuthGuard } from '@common-ui'
import { useAuthStore } from '../store/auth'
import Layout from '../components/Layout.vue'
import Login from '../views/Login.vue'
import ClassificationGrading from '../views/ClassificationGrading.vue'
import SensitiveDataDefinitions from '../views/SensitiveDataDefinitions.vue'
import ProtectionBaselineList from '../views/ProtectionBaselineList.vue'
import ProtectionEnrollmentList from '../views/ProtectionEnrollmentList.vue'

const children = [
  { path: 'classification-grading', component: ClassificationGrading, meta: { requiresAuth: true } },
  { path: 'sensitive-data-definitions', component: SensitiveDataDefinitions, meta: { requiresAuth: true } },
  { path: 'protection-baselines', component: ProtectionBaselineList, meta: { requiresAuth: true } }
]
children.push({ path: 'protection-enrollments', component: ProtectionEnrollmentList, meta: { requiresAuth: true } })

const router = createRouter({ history: createWebHistory(import.meta.env.DEV ? '/' : '/security/'), routes: [
  { path: '/login', name: 'Login', component: Login, meta: { requiresAuth: false } },
  { path: '/', component: Layout, redirect: '/sensitive-data-definitions', meta: { requiresAuth: true }, children }
] })
router.beforeEach(createAuthGuard(useAuthStore, { moduleName: 'Security', loginRouteName: 'Login' }))
export default router
