import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

export function browserTestRun() {
  if (!process.env.ADDP_BROWSER_RUN) throw new Error('Use npm run test:e2e to launch an isolated browser gate')
  return JSON.parse(process.env.ADDP_BROWSER_RUN)
}

export function browserTestOrigin(moduleName) {
  const port = browserTestRun().ports[moduleName]
  if (!Number.isInteger(port) || port < 1 || port > 65535) throw new Error(`Missing browser fixture: ${moduleName}`)
  return `http://127.0.0.1:${port}`
}

export function browserTestFixtures(frontend) {
  const packageData = JSON.parse(readFileSync(resolve(frontend, 'package.json'), 'utf8'))
  const fixtures = packageData.addpBrowserTest?.fixtures
  if (!Array.isArray(fixtures) || !fixtures.length) throw new Error('Browser fixtures must be declared in package.json')
  const modules = new Set()
  for (const fixture of fixtures) {
    if (!fixture || typeof fixture.module !== 'string' || !/^[a-z][a-z0-9-]*$/.test(fixture.module) || modules.has(fixture.module)
      || Object.keys(fixture).some(key => !['module', 'base', 'readyPath'].includes(key))
      || typeof fixture.readyPath !== 'string' || !fixture.readyPath.startsWith('/') || fixture.readyPath.startsWith('//')
      || (fixture.base !== undefined && (typeof fixture.base !== 'string' || !/^\/[a-z0-9/-]*$/.test(fixture.base)))) {
      throw new Error('Invalid or duplicate browser fixture')
    }
    modules.add(fixture.module)
  }
  return fixtures
}

export function withBrowserTestIsolation(owner, config) {
  const run = browserTestRun()
  if (run.owner !== owner) throw new Error('Browser gate owner does not match its isolated run')
  if ('webServer' in config || 'outputDir' in config || 'projects' in config || Object.hasOwn(config.use || {}, 'baseURL')) {
    throw new Error('Browser addresses, servers and output are owned by shared isolation')
  }
  const fixtures = browserTestFixtures(resolve(run.repository, owner, 'frontend'))
  const environment = {
    ...process.env,
    ADDP_E2E: '1',
    VITE_ADDP_FRONTEND_PORTS: Object.entries(run.ports).map(([name, port]) => `${name}:${port}`).join(','),
    VITE_ADDP_CONSOLE_PORT: String(run.ports.console || run.ports[owner]),
    VITE_ADDP_SERVICE_FIXTURE_ORIGIN: run.ports.service ? browserTestOrigin('service') : ''
  }
  for (const [name, port] of Object.entries(run.ports)) environment[`${name.toUpperCase()}_FE_PORT`] = String(port)
  return {
    ...config,
    outputDir: run.outputDir,
    use: { ...config.use, baseURL: browserTestOrigin(owner) },
    webServer: fixtures.map(fixture => ({
      command: `npm run dev -- --host 127.0.0.1 --port ${run.ports[fixture.module]} --strictPort${fixture.base === undefined ? '' : ` --base ${fixture.base}`}`,
      cwd: resolve(run.repository, fixture.module, 'frontend'),
      env: environment,
      url: browserTestOrigin(fixture.module) + fixture.readyPath,
      reuseExistingServer: false,
      gracefulShutdown: { signal: 'SIGTERM', timeout: 5000 },
      timeout: 30_000
    }))
  }
}
