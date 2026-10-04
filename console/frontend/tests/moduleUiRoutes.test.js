import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

const repositoryRoot = new URL('../../../', import.meta.url)
const read = path => readFileSync(new URL(path, repositoryRoot), 'utf8')

const consoleRoutes = {
  ontology: 'ontology',
  system: 'system',
  manager: 'manager',
  meta: 'meta',
  transfer: 'transfer',
  orchestrator: 'orchestrator',
  develop: 'develop',
  service: 'service',
  workbench: 'workbench',
  monitor: 'monitor',
  standard: 'standard',
  modeling: 'model',
  quality: 'quality',
  security: 'security',
  catalog: 'catalog',
  asset: 'asset',
  agent: 'agent',
  graph: 'graph',
  inference: 'inference',
}

describe('Console public routes and iframe entry ownership', () => {
  const consoleConfig = read('console/frontend/src/config/portalConfig.js')
  const nginx = read('nginx/nginx.conf')

  it('uses a trailing slash for module roots so they match Nginx locations', () => {
    expect(consoleConfig).toContain('`${window.location.origin}/module-ui/${module}/`')
  })

  for (const [publicModule, frontend] of Object.entries(consoleRoutes)) {
    it(`${publicModule} uses the same private entry in Console, Vite and Nginx`, () => {
      const vite = read(`${frontend}/frontend/vite.config.js`)
      const router = read(`${frontend}/frontend/src/router/index.js`)
      expect(consoleConfig).toMatch(new RegExp(`\\b${publicModule}:\\s+_url\\('${frontend}'\\)`))
      expect(vite).toContain(`withModuleFrontend('${frontend}',`)
      expect(nginx).toContain(`location /module-ui/${frontend}/ {`)
      expect(nginx).not.toContain(`location /${frontend}/ {`)
      expect(router).toContain('import.meta.env.BASE_URL')
    })
  }

  it('keeps the independent Portal and data application routes', () => {
    expect(nginx).toContain('location /portal/ {')
    expect(nginx).toContain('location /data-apps/ {')
    expect(nginx).not.toContain('location = /agent {')
  })

  it('proxies the Workbench runtime and its Vite resources without taking Console public routes', () => {
    const vite = read('console/frontend/vite.config.js')
    expect(consoleConfig).toContain("_url('workbench')")
    expect(vite).toContain('...createModuleFrontendProxies()')
    expect(vite).not.toContain("'/workbench': {")
  })

  it('preserves the existing direct Swagger document routes', () => {
    expect(nginx).toContain('location = /ontology/swagger/doc.json {')
    expect(nginx).toContain('location = /catalog/swagger/doc.json {')
  })

  it('packages the same Nginx routing table that runs in the container', () => {
    const packageScript = read('scripts/build/package.sh')
    expect(packageScript).toContain('cp nginx/nginx.conf "$OUTPUT_DIR/nginx/"')
    expect(packageScript).not.toContain('nginx.prod.conf')
  })
})
