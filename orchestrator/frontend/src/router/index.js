import { createRouter, createWebHistory } from 'vue-router'
import OrchestrationList from '../views/OrchestrationList.vue'
import OrchestrationForm from '../views/OrchestrationForm.vue'
import ExecutionList from '../views/ExecutionList.vue'

const routes = [
  {
    path: '/',
    redirect: '/orchestrations'
  },
  {
    path: '/orchestrations',
    name: 'OrchestrationList',
    component: OrchestrationList
  },
  {
    path: '/orchestrations/new',
    name: 'OrchestrationCreate',
    component: OrchestrationForm
  },
  {
    path: '/orchestrations/:id/edit',
    name: 'OrchestrationEdit',
    component: OrchestrationForm
  },
  {
    path: '/orchestrations/:id/executions',
    name: 'ExecutionList',
    component: ExecutionList
  }
]

const router = createRouter({
  history: createWebHistory(),
  routes
})

export default router
