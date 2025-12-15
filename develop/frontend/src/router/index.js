import { createRouter, createWebHistory } from 'vue-router'
import Layout from '../views/Layout.vue'
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
    component: Layout,
    redirect: '/sql',
    meta: { requiresAuth: true },
    children: [
      {
        path: 'sql',
        name: 'SQLEditor',
        component: () => import('../views/SQLEditor.vue'),
        meta: { requiresAuth: true, title: 'SQL 工作台' }
      },
      {
        path: 'gis-workflow',
        name: 'GISWorkflowEditor',
        component: () => import('../views/GISWorkflowEditor.vue'),
        meta: { requiresAuth: true, title: 'GIS 工作流编辑器' }
      },
      {
        path: 'gis-tasks',
        name: 'GISTasks',
        component: () => import('../views/GISTasks.vue'),
        meta: { requiresAuth: true, title: 'GIS 任务管理' }
      },
      {
        path: 'gis-executions',
        name: 'GISExecutions',
        component: () => import('../views/GISExecutions.vue'),
        meta: { requiresAuth: true, title: '执行历史' }
      },
      {
        path: 'gis-executions/:id',
        name: 'GISExecutionDetail',
        component: () => import('../views/GISExecutionDetail.vue'),
        meta: { requiresAuth: true, title: '执行详情' }
      },
      // 保留旧路由以兼容
      {
        path: 'spatial',
        redirect: '/gis-tasks'
      }
    ]
  }
]

const router = createRouter({
  history: createWebHistory(import.meta.env.DEV ? '/' : '/develop/'),
  routes
})

// 路由守卫：支持两种运行模式（Portal 嵌入 + 独立访问）
import { createAuthGuard } from '@common-ui'

// 路径规范化函数：处理 /develop/ 前缀
const normalizeRedirect = fullPath => {
  if (!fullPath) {
    return '/sql'  // 默认重定向到 SQL 编辑器
  }

  // 处理根路径重定向
  if (fullPath === '/develop' || fullPath === '/develop/') {
    return '/sql'
  }

  // 移除模块前缀（如果存在）
  if (fullPath.startsWith('/develop/')) {
    return fullPath.replace('/develop', '')
  }

  return fullPath
}

router.beforeEach(createAuthGuard(useAuthStore, {
  moduleName: 'Develop',
  loginRouteName: 'Login',
  normalizeRedirect
}))

export default router
