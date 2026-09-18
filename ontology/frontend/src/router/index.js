import { createRouter, createWebHistory } from 'vue-router'
import { createAuthGuard } from '@common-ui'
import { useAuthStore } from '../store/auth'
import Layout from '../components/Layout.vue'
const router = createRouter({
  history: createWebHistory('/ontology/'),
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
      redirect: '/ontologies',
      meta: { requiresAuth: true },
      children: [
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
    moduleName: 'Ontology',
    loginRouteName: 'Login'
  })
)
export default router
