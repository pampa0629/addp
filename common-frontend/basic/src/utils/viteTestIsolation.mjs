import { tmpdir } from 'node:os'
import { resolve } from 'node:path'
import { rmSync } from 'node:fs'

// Build-time only: every deterministic browser fixture uses this single policy.
export function withFrontendTestIsolation(moduleName, config) {
  if (process.env.ADDP_E2E !== '1') return config
  if (!/^[a-z][a-z0-9-]*$/.test(moduleName)) throw new Error('Invalid frontend module name')

  const cacheDir = resolve(tmpdir(), `addp-${moduleName}-vite-test-${process.pid}`)
  const cleanup = () => rmSync(cacheDir, { recursive: true, force: true })
  let shutdown
  process.once('exit', cleanup)
  return {
    ...config,
    cacheDir,
    server: { ...config.server, hmr: false, strictPort: true },
    plugins: [...(config.plugins || []), {
      name: 'addp-frontend-test-isolation',
      configureServer(server) {
        let closing = false
        shutdown = async () => {
          if (closing) return
          closing = true
          let exitCode = 0
          try {
            await server.close()
          } catch (error) {
            console.error('Frontend fixture shutdown failed:', error)
            exitCode = 1
          } finally {
            cleanup()
            process.exit(exitCode)
          }
        }
        process.once('SIGTERM', shutdown)
        process.once('SIGINT', shutdown)
      },
      closeBundle() {
        cleanup()
        process.removeListener('exit', cleanup)
        if (shutdown) {
          process.removeListener('SIGTERM', shutdown)
          process.removeListener('SIGINT', shutdown)
        }
      }
    }]
  }
}
