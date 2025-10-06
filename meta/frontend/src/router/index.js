import { createRouter, createWebHistory } from 'vue-router'

const routes = [
  {
    path: '/login',
    name: 'Login',
    component: () => import('../views/Login.vue'),
    meta: { public: true }
  },
  {
    path: '/',
    redirect: '/scan'
  },
  {
    path: '/scan',
    name: 'MetadataScan',
    component: () => import('../views/MetadataScan.vue')
  },
  {
    path: '/metadata',
    name: 'Metadata',
    component: () => import('../views/MetadataBrowser.vue')
  },
  {
    path: '/datasources',
    name: 'Datasources',
    component: () => import('../views/DatasourceList.vue')
  }
]

const router = createRouter({
  history: createWebHistory('/meta/'),
  routes
})

// 路由守卫：检查登录状态
router.beforeEach((to, from, next) => {
  const token = localStorage.getItem('token')
  const isLoginPage = to.path === '/login'

  console.log('=== 路由守卫 ===')
  console.log('from.path:', from.path)
  console.log('from.fullPath:', from.fullPath)
  console.log('to.path:', to.path)
  console.log('to.fullPath:', to.fullPath)
  console.log('token:', token ? token.substring(0, 20) + '...' : null)
  console.log('token存在:', !!token)
  console.log('isLoginPage:', isLoginPage)
  console.log('to.meta.public:', to.meta.public)

  // 保存日志到 localStorage
  const guardLogs = JSON.parse(localStorage.getItem('guard_logs') || '[]')
  guardLogs.push({
    time: new Date().toISOString(),
    from: from.path,
    to: to.path,
    hasToken: !!token,
    isLoginPage,
    isPublic: to.meta.public
  })
  localStorage.setItem('guard_logs', JSON.stringify(guardLogs.slice(-20)))

  if (token && isLoginPage) {
    // 已登录，访问登录页，重定向到首页
    console.log('已登录访问登录页，重定向到/metadata')
    next('/metadata')
  } else if (!token && !to.meta.public) {
    // 未登录，访问需要认证的页面，跳转到登录页
    console.log('未登录访问受保护页面，重定向到/login')
    next('/login')
  } else {
    // 其他情况直接放行
    console.log('直接放行')
    next()
  }
})

export default router
