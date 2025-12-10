import { createRouter, createWebHistory } from 'vue-router'
import SQLEditor from '../views/SQLEditor.vue'

const routes = [
  {
    path: '/',
    name: 'SQLEditor',
    component: SQLEditor,
    meta: { title: 'SQL 开发' }
  }
]

const router = createRouter({
  history: createWebHistory(import.meta.env.DEV ? '/' : '/develop/'),
  routes
})

export default router
