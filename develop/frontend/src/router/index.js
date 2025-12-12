import { createRouter, createWebHistory } from 'vue-router'
import SQLEditor from '../views/SQLEditor.vue'
import SpatialTasks from '../views/SpatialTasks.vue'
import Login from '../views/Login.vue'
import { useAuthStore } from '../stores/auth'

const routes = [
  {
    path: '/login',
    name: 'Login',
    component: Login,
    meta: { requiresAuth: false }
  },
  {
    path: '/',
    name: 'SQLEditor',
    component: SQLEditor,
    meta: { requiresAuth: true, title: 'SQL 开发' }
  },
  {
    path: '/sql',
    name: 'SQLEditorAlias',
    component: SQLEditor,
    meta: { requiresAuth: true, title: 'SQL 开发' }
  },
  {
    path: '/spatial',
    name: 'SpatialTasks',
    component: SpatialTasks,
    meta: { requiresAuth: true, title: '空间计算任务' }
  }
]

const router = createRouter({
  history: createWebHistory(import.meta.env.DEV ? '/' : '/develop/'),
  routes
})

// 路由守卫
router.beforeEach((to, from, next) => {
  const authStore = useAuthStore()

  // 白名单路由
  if (!to.meta.requiresAuth) {
    next()
    return
  }

  // 需要认证的路由
  if (authStore.isAuthenticated) {
    next()
  } else {
    next('/login')
  }
})

export default router
