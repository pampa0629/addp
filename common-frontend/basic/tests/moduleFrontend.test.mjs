import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { withModuleFrontend, createModuleFrontendProxies } from '../src/utils/moduleFrontend.mjs'

function redirect(config, url, host, destination = 'document', base = config.base) {
  const plugin = config.plugins.find(plugin => plugin.name === 'addp-module-frontend')
  plugin.configResolved({ base })
  let handler
  plugin.configureServer({ middlewares: { use(fn) { handler = fn } } })
  const response = { headers: {}, setHeader(key, value) { this.headers[key] = value }, end() { this.ended = true } }
  let next = false
  handler({ url, headers: { host, 'sec-fetch-dest': destination } }, response, () => { next = true })
  return { ...response, next }
}

test('direct module documents redirect before initialization and preserve the resource path and query', () => {
  const config = withModuleFrontend('security', {}, { VITE_ADDP_CONSOLE_PORT: '6170' })
  const response = redirect(config, '/protection-enrollments?locator=a%2Fb', 'example.test:6191')
  assert.equal(response.statusCode, 307)
  assert.equal(response.headers.Location, 'http://example.test:6170/module-ui/security/protection-enrollments?locator=a%2Fb')
  assert.equal(response.ended, true)
  assert.equal(redirect(config, '/module-ui/security?tab=details', 'example.test:6191').headers.Location,
    'http://example.test:6170/module-ui/security/?tab=details')
  assert.equal(redirect(config, '/module-ui/security/login?redirect=%2F', '[::1]:6191').headers.Location,
    'http://[::1]:6170/module-ui/security/login?redirect=%2F')
})

test('proxy documents, iframe requests, assets and fixture base overrides do not redirect', () => {
  const config = withModuleFrontend('system', {}, { VITE_ADDP_CONSOLE_PORT: '4170' })
  assert.equal(redirect(config, '/module-ui/system/login', '127.0.0.1:4170').next, true)
  assert.equal(redirect(config, '/module-ui/system/login', '127.0.0.1:4173', 'iframe').next, true)
  assert.equal(redirect(config, '/module-ui/system/src/main.js', '127.0.0.1:4173', 'script').next, true)
  const fixture = withModuleFrontend('system', {}, { ADDP_E2E: '1' })
  assert.equal(redirect(fixture, '/login', '127.0.0.1:4173', 'document', '/').next, true)
  assert.throws(() => redirect(config, '/login', '127.0.0.1:4173', 'document', '/'), /formal public entry/)
})

test('Portal and Workbench preserve their formal product routes', () => {
  const portal = withModuleFrontend('portal', {}, { CONSOLE_FE_PORT: '4170' })
  const workbench = withModuleFrontend('workbench', {}, { CONSOLE_FE_PORT: '4170' })
  assert.equal(portal.base, '/portal/')
  assert.equal(redirect(portal, '/portal/assets/42?tab=details', 'localhost:4185').headers.Location,
    'http://localhost:4170/portal/assets/42?tab=details')
  assert.equal(redirect(workbench, '/module-ui/workbench/data-apps/42', 'localhost:4190').headers.Location,
    'http://localhost:4170/data-apps/42')
})

test('all module resources and HMR are namespaced and proxy keeps the browser Host', () => {
  const config = withModuleFrontend('security', { server: { hmr: { host: 'localhost', port: 5191 } } }, { CONSOLE_FE_PORT: '6170' })
  assert.equal(config.base, '/module-ui/security/')
  assert.deepEqual(config.server.hmr, { clientPort: 6170, path: '__hmr' })
  const proxies = createModuleFrontendProxies({ VITE_ADDP_FRONTEND_PORTS: 'security:6191,workbench:6190', PORTAL_FE_PORT: '6185' })
  assert.equal(proxies['/module-ui/security'].target, 'http://localhost:6191')
  assert.equal(proxies['/module-ui/security'].changeOrigin, false)
  assert.equal(proxies['/module-ui/security'].ws, true)
  assert.equal(proxies['/data-apps'].rewrite('/data-apps/42?x=1'), '/module-ui/workbench/data-apps/42?x=1')
  assert.equal(proxies['/portal'].target, 'http://localhost:6185')
  assert.equal(proxies['/security'], undefined)
  assert.equal(createModuleFrontendProxies({ ADDP_E2E: '1', VITE_ADDP_FRONTEND_PORTS: 'system:4173' })['/module-ui/system'].target, 'http://127.0.0.1:4173')
})

test('every module Vite config uses the single shared entry implementation', () => {
  for (const name of ['system', 'manager', 'meta', 'transfer', 'orchestrator', 'develop', 'service', 'monitor', 'standard', 'model', 'quality', 'security', 'catalog', 'asset', 'agent', 'graph', 'inference', 'ontology', 'portal', 'workbench']) {
    const source = readFileSync(new URL(`../../../${name}/frontend/vite.config.js`, import.meta.url), 'utf8')
    assert.match(source, new RegExp(`withModuleFrontend\\('${name}',`))
    assert.doesNotMatch(source, /base:\s*process\.env\.NODE_ENV/)
  }
})
