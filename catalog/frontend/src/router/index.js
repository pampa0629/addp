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
    redirect: '/entries',
    meta: { requiresAuth: true },
    children: [
      {
        path: 'entries',
        name: 'EntryList',
        component: () => import('../views/EntryList.vue'),
        meta: { requiresAuth: true }
      },
      {
        path: 'entries/:id',
        name: 'EntryDetail',
        component: () => import('../views/EntryDetail.vue'),
        meta: { requiresAuth: true }
      },
      {
        path: 'governance/tasks',
        name: 'GovernanceTasks',
        component: () => import('../views/GovernanceTasks.vue'),
        meta: { requiresAuth: true, requiredPermission: 'catalog.entry.update' }
      },
      {
        path: 'me/entries',
        name: 'MyCatalog',
        component: () => import('../views/MyCatalog.vue'),
        meta: { requiresAuth: true, requiredPermission: 'catalog.entry.read' }
      },
      {
        path: 'collections',
        name: 'CollectionList',
        component: () => import('../views/CollectionList.vue'),
        meta: { requiresAuth: true, requiredPermission: 'catalog.collection.read' }
      },
      {
        path: 'collections/:id',
        name: 'CollectionDetail',
        component: () => import('../views/CollectionDetail.vue'),
        meta: { requiresAuth: true, requiredPermission: 'catalog.collection.read' }
      }
    ]
  }
]

const router = createRouter({
  history: createWebHistory(import.meta.env.DEV ? '/' : '/catalog/'),
  routes
})

router.beforeEach(createAuthGuard(useAuthStore, {
  moduleName: 'Catalog',
  loginRouteName: 'Login'
}))

export default router
