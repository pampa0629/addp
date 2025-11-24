import { createRouter, createWebHistory } from 'vue-router'
import LineageView from '../views/LineageView.vue'

const routes = [
  {
    path: '/',
    name: 'lineage',
    component: LineageView
  }
]

const router = createRouter({
  history: createWebHistory(),
  routes
})

export default router
