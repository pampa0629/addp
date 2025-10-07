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
  }
]

const router = createRouter({
  history: createWebHistory('/meta/'),
  routes
})

// 路由守卫：检查登录状态
router.beforeEach((to, from, next) => {
  const token = localStorage.getItem('token')
  const isPublic = to.meta.public

  if (!token && !isPublic) {
    // 未登录，访问受保护页面，跳转到登录页
    next('/login')
  } else if (token && to.path === '/login') {
    // 已登录，访问登录页，重定向到首页
    next('/scan')
  } else {
    // 其他情况直接放行
    next()
  }
})

export default router
