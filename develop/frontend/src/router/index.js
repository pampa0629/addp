import { createRouter, createWebHistory } from 'vue-router'
import Layout from '../components/Layout.vue'
import Login from '../views/Login.vue'
import { useAuthStore } from '../store/auth'

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
        name: 'QueryEditor',
        alias: 'query',
        component: () => import('../views/QueryEditor.vue'),
        meta: { requiresAuth: true, title: '查询编辑器' }
      },
      {
        path: 'notebook',
        name: 'NotebookEditor',
        component: () => import('../views/NotebookEditor.vue'),
        meta: { requiresAuth: true, title: 'Notebook 开发' }
      },
      {
        path: 'sql-tasks',
        name: 'QueryTasks',
        component: () => import('../views/QueryTasks.vue'),
        meta: { requiresAuth: true, title: '查询任务' }
      },
      {
        path: 'workflow',
        name: 'WorkflowEditor',
        component: () => import('../views/WorkflowEditor.vue'),
        meta: { requiresAuth: true, title: '工作流编辑器' }
      },
      {
        path: 'tasks',
        name: 'TaskManagement',
        component: () => import('../views/TaskManagement.vue'),
        meta: { requiresAuth: true, title: '任务管理' }
      },
      {
        path: 'executions',
        name: 'ExecutionMonitor',
        component: () => import('../views/ExecutionMonitor.vue'),
        meta: { requiresAuth: true, title: '执行监控' }
      },
      {
        path: 'executions/:execution_id',
        name: 'ExecutionDetail',
        component: () => import('../views/ExecutionDetail.vue'),
        meta: { requiresAuth: true, title: '执行详情' }
      }
    ]
  }
]

const router = createRouter({
  history: createWebHistory(import.meta.env.DEV ? '/' : '/develop/'),
  routes
})

// 路由守卫：支持两种运行模式（Console 嵌入 + 独立访问）
import { createAuthGuard } from '@common-ui'

// 路径规范化函数：处理 /develop/ 前缀
const normalizeRedirect = fullPath => {
  if (!fullPath) {
    return '/query'  // 默认重定向到 SQL 编辑器
  }

  // 处理根路径重定向
  if (fullPath === '/develop' || fullPath === '/develop/') {
    return '/query'
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
