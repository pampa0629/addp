import { parseDevPorts } from './devPorts.js'

// Development service ports; public browser URLs never contain these ports.
const frontendPorts = {
  system: 5173, manager: 5174, meta: 5175, transfer: 5176,
  orchestrator: 5177, develop: 5178, monitor: 5179, service: 5180,
  standard: 5181, model: 5182, quality: 5183, asset: 5184,
  portal: 5185, agent: 5186, graph: 5187, inference: 5188,
  catalog: 5189, workbench: 5190, security: 5191, ontology: 5192
}

export function createModuleFrontendProxies(environment = process.env) {
  const registered = parseDevPorts(environment.VITE_ADDP_FRONTEND_PORTS)
  const upstreamHost = environment.ADDP_E2E === '1' ? '127.0.0.1' : 'localhost'
  const proxy = (name) => ({
    target: `http://${upstreamHost}:${registered[name] || environment[`${name.toUpperCase()}_FE_PORT`] || frontendPorts[name]}`,
    changeOrigin: false,
    ws: true
  })
  return {
    ...Object.fromEntries(Object.keys(frontendPorts).filter(name => name !== 'portal')
      .map(name => [`/module-ui/${name}`, proxy(name)])),
    '/portal': proxy('portal'),
    '/data-apps': { ...proxy('workbench'), rewrite: path => `/module-ui/workbench${path}` }
  }
}

// Build-time only: shared public base, HMR and pre-authentication redirect policy.
export function withModuleFrontend(moduleName, config, environment = process.env) {
  if (!Object.hasOwn(frontendPorts, moduleName)) throw new Error('Invalid frontend module name')
  const base = moduleName === 'portal' ? '/portal/' : `/module-ui/${moduleName}/`
  const consolePort = Number(environment.VITE_ADDP_CONSOLE_PORT || environment.CONSOLE_FE_PORT || 5170)
  let resolvedBase = base
  const { host, port, ...hmr } = config.server?.hmr || {}
  return {
    ...config,
    base,
    server: { ...config.server, hmr: config.server?.hmr === false ? false : { ...hmr, clientPort: consolePort, path: '__hmr' } },
    plugins: [...(config.plugins || []), {
      name: 'addp-module-frontend',
      configResolved(resolved) {
        if (resolved.base !== base && environment.ADDP_E2E !== '1') {
          throw new Error('Module frontend base must use its formal public entry')
        }
        resolvedBase = resolved.base
      },
      configureServer(server) {
        server.middlewares.use((request, response, next) => {
          // CLI base overrides are isolated test fixtures, not product entries.
          if (resolvedBase !== base || request.headers['sec-fetch-dest'] !== 'document' || !request.headers.host) return next()
          const origin = new URL(`${request.socket?.encrypted ? 'https' : 'http'}://${request.headers.host}`)
          if (origin.port === String(consolePort)) return next()
          const incoming = new URL(request.url, origin)
          let path = incoming.pathname === base.slice(0, -1) ? ''
            : incoming.pathname.startsWith(base) ? incoming.pathname.slice(base.length) : incoming.pathname.replace(/^\//, '')
          path = moduleName === 'workbench' && (path === 'data-apps' || path.startsWith('data-apps/'))
            ? `/${path}` : `${base}${path}`
          origin.port = String(consolePort)
          // A redirect without a fragment preserves the browser's existing fragment.
          response.statusCode = 307
          response.setHeader('Location', `${origin.origin}${path}${incoming.search}`)
          response.setHeader('Cache-Control', 'no-store')
          response.end()
        })
      }
    }]
  }
}
