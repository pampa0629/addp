import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createMemoryHistory, createRouter } from 'vue-router'
import { resolveModuleLandingRoute } from '../../../common-frontend/basic/src/authorization/consoleRouteAccess.js'

const source = readFileSync(new URL('../src/router/index.js', import.meta.url), 'utf8')
const routeTable = source.slice(source.indexOf('const routes ='), source.indexOf('const router ='))
const normalizeRedirectSource = source.slice(source.indexOf('const normalizeRedirect ='), source.indexOf('router.beforeEach'))
const normalizeRedirect = new Function(`${normalizeRedirectSource}; return normalizeRedirect`)()

function createDevelopRouter(permissions, contextType = 'tenant') {
  const authStore = { contextType, permissions }
  const routes = new Function('Layout', 'Login', 'useAuthStore', 'resolveModuleLandingRoute', `${routeTable}; return routes`)(
    {}, {}, () => authStore, resolveModuleLandingRoute)
  for (const child of routes[1].children) {
    if (child.component) child.component = {}
  }
  routes.push({ path: '/forbidden', component: {} })
  return createRouter({ history: createMemoryHistory(), routes })
}

const notebookOnly = createDevelopRouter(['develop.notebook.read'])
await notebookOnly.push('/')
assert.equal(notebookOnly.currentRoute.value.path, '/notebook')
await notebookOnly.push('/notebook?action=edit&id=12')
assert.equal(notebookOnly.currentRoute.value.fullPath, '/notebook?action=edit&id=12')

const tasksOnly = createDevelopRouter(['develop.task.read'])
await tasksOnly.push('/')
assert.equal(tasksOnly.currentRoute.value.path, '/tasks')

const queryAccess = createDevelopRouter(['develop.task.read', 'meta.catalog.read'])
await queryAccess.push('/')
assert.equal(queryAccess.currentRoute.value.path, '/sql')

const noAccess = createDevelopRouter([])
await noAccess.push('/')
assert.equal(noAccess.currentRoute.value.path, '/forbidden')

const wrongContext = createDevelopRouter(['develop.notebook.read'], 'platform')
await wrongContext.push('/')
assert.equal(wrongContext.currentRoute.value.path, '/forbidden')

assert.equal(normalizeRedirect('/develop'), '/')
assert.equal(normalizeRedirect('/develop/'), '/')
assert.equal(normalizeRedirect('/develop/tasks?type=query'), '/tasks?type=query')

console.log('Develop landing route tests passed')
