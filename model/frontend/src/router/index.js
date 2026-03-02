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
    redirect: '/modeling/dw-layers',
    meta: { requiresAuth: true },
    children: [
      {
        path: 'modeling/dw-layers',
        name: 'DWLayerList',
        component: () => import('../views/DWLayerList.vue'),
        meta: { requiresAuth: true, title: '数仓分层' }
      },
      {
        path: 'modeling/entities',
        name: 'EntityList',
        component: () => import('../views/EntityList.vue'),
        meta: { requiresAuth: true, title: '业务实体' }
      },
      {
        path: 'modeling/entities/:id',
        name: 'EntityDetail',
        component: () => import('../views/EntityDetail.vue'),
        meta: { requiresAuth: true, title: '实体详情' }
      },
      {
        path: 'modeling/logical-tables',
        name: 'LogicalTableList',
        component: () => import('../views/LogicalTableList.vue'),
        meta: { requiresAuth: true, title: '逻辑表设计' }
      },
      {
        path: 'modeling/logical-tables/:id',
        name: 'LogicalTableDetail',
        component: () => import('../views/LogicalTableDetail.vue'),
        meta: { requiresAuth: true, title: '逻辑表详情' }
      },
      {
        path: 'modeling/er-diagram',
        name: 'ERDiagramManager',
        component: () => import('../views/ERDiagramManager.vue'),
        meta: { requiresAuth: true, title: '实体关系图' }
      },
      {
        path: 'modeling/star-schema',
        name: 'StarSchema',
        component: () => import('../views/StarSchemaView.vue'),
        meta: { requiresAuth: true, title: '星型建模视图' }
      }
    ]
  }
]

const router = createRouter({
  history: createWebHistory(import.meta.env.DEV ? '/' : '/model/'),
  routes
})

router.beforeEach(createAuthGuard(useAuthStore, {
  moduleName: 'Model',
  loginRouteName: 'Login'
}))

export default router
