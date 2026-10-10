import { createRouter, createWebHistory } from 'vue-router'
import { createAuthGuard } from '@common-ui'
import { resolveModuleLandingRoute } from '@common-ui/authorization/consoleRouteAccess'
import { useAuthStore } from '../store/auth'
import Layout from '../components/Layout.vue'
const router = createRouter({
  history: createWebHistory(import.meta.env.BASE_URL),
  routes: [
    {
      path: '/login',
      name: 'Login',
      component: () => import('../views/Login.vue'),
      meta: { requiresAuth: false }
    },
    {
      path: '/',
      component: Layout,
      meta: { requiresAuth: true },
      children: [
        {
          path: '',
          beforeEnter: () => {
            const auth = useAuthStore()
            return resolveModuleLandingRoute('/ontology', ['/platform/definitions', '/ontologies'], auth.contextType, auth.permissions)
          },
          meta: { handlesForbidden: true }
        },
        {
          path: 'platform/definitions/:capability?',
          component: () => import('../views/PlatformDefinitions.vue')
        },
        {
          path: 'ontologies',
          component: () => import('../views/OntologyList.vue')
        },
        {
          path: 'ontologies/new',
          component: () => import('../views/RevisionEditor.vue')
        },
        {
          path: 'ontologies/:ontology_id/trial',
          component: () => import('../views/RuleTrial.vue')
        },
        {
          path: 'ontologies/:ontology_id',
          component: () => import('../views/OntologyDetail.vue')
        },
        {
          path: 'ontologies/:ontology_id/revisions/:revision',
          component: () => import('../views/RevisionEditor.vue')
        }
      ]
    }
  ]
})
router.beforeEach(
  createAuthGuard(useAuthStore, {
    router, moduleName: 'Ontology',
    loginRouteName: 'Login'
  })
)
export default router
