import { createRouter, createWebHistory } from 'vue-router'
import { createAuthGuard } from '@common-ui'
import { useAuthStore } from '../store/auth'
import Layout from '../components/Layout.vue'
import Login from '../views/Login.vue'
import FoundationList from '../views/FoundationList.vue'
import ProtectionEnrollmentList from '../views/ProtectionEnrollmentList.vue'

const children = [
  ['classifications', 'classification'],
  ['grades', 'grade'],
  ['sensitive-data-types', 'sensitiveDataType'],
  ['protection-baselines', 'protectionBaseline']
].map(([path, resource]) => ({ path, component: FoundationList, meta: { requiresAuth: true, resource } }))
children.push({ path: 'protection-enrollments', component: ProtectionEnrollmentList, meta: { requiresAuth: true } })

const router = createRouter({ history: createWebHistory(import.meta.env.DEV ? '/' : '/security/'), routes: [
  { path: '/login', name: 'Login', component: Login, meta: { requiresAuth: false } },
  { path: '/', component: Layout, redirect: '/sensitive-data-types', meta: { requiresAuth: true }, children }
] })
router.beforeEach(createAuthGuard(useAuthStore, { moduleName: 'Security', loginRouteName: 'Login' }))
export default router
