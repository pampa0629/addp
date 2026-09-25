import { describe, expect, it } from 'vitest'
import { loadConfigFromFile } from 'vite'
import { spawn } from 'node:child_process'
import { createServer as createHttpServer } from 'node:http'
import { createServer as createTcpServer } from 'node:net'
import { setTimeout as delay } from 'node:timers/promises'
import { fileURLToPath } from 'node:url'
import { parseDevPorts } from '../../../common-frontend/basic/src/utils/devPorts'

async function availablePort() {
  while (true) {
    const listener = createTcpServer()
    await new Promise((resolve, reject) => {
      listener.once('error', reject)
      listener.listen(0, '127.0.0.1', resolve)
    })
    const port = listener.address().port
    await new Promise((resolve) => listener.close(resolve))
    if (port !== 5170) return port
  }
}

async function waitForVite(url, child, output) {
  const deadline = Date.now() + 30000
  while (Date.now() < deadline) {
    if (child.exitCode !== null || child.signalCode !== null) {
      throw new Error(`Vite exited before it served the selected port:\n${output()}`)
    }
    try {
      const response = await fetch(url, { signal: AbortSignal.timeout(1000) })
      if (response.ok && (await response.text()).includes('<div id="app">')) return
    } catch {
      // Vite has not bound the selected port yet.
    }
    await delay(200)
  }
  throw new Error(`Vite did not serve the selected port:\n${output()}`)
}

async function stopProcessGroup(child) {
  if (!child.pid || child.exitCode !== null || child.signalCode !== null) return
  try { process.kill(-child.pid, 'SIGTERM') } catch { return }
  const exited = new Promise((resolve) => child.once('exit', resolve))
  await Promise.race([exited, delay(5000)])
  if (child.exitCode === null && child.signalCode === null) {
    try { process.kill(-child.pid, 'SIGKILL') } catch { /* already exited */ }
    await exited
  }
}

describe('开发端口传递', () => {
  it('解析启动脚本传给 iframe 和 API 文档的端口', () => {
    expect(parseDevPorts('console:15170,system:15173')).toEqual({ console: '15170', system: '15173' })
    expect(parseDevPorts('')).toEqual({})
  })

  it('使用启动时端口配置 Console、HMR 与代理', async () => {
    const previous = {
      CONSOLE_FE_PORT: process.env.CONSOLE_FE_PORT,
      GATEWAY_PORT: process.env.GATEWAY_PORT,
      SYSTEM_BACKEND_PORT: process.env.SYSTEM_BACKEND_PORT,
      PORTAL_BACKEND_PORT: process.env.PORTAL_BACKEND_PORT,
      WORKBENCH_FE_PORT: process.env.WORKBENCH_FE_PORT,
    }
    try {
      process.env.CONSOLE_FE_PORT = '15170'
      process.env.GATEWAY_PORT = '18000'
      process.env.SYSTEM_BACKEND_PORT = '18180'
      process.env.PORTAL_BACKEND_PORT = '18184'
      process.env.WORKBENCH_FE_PORT = '15190'
      const { config } = await loadConfigFromFile(
        { command: 'serve', mode: 'development' },
        new URL('../vite.config.js', import.meta.url).pathname,
      )
      expect(config.server.port).toBe(15170)
      expect(config.server.hmr.clientPort).toBe(15170)
      expect(config.server.proxy['/api'].target).toBe('http://localhost:18000')
      expect(config.server.proxy['/module-health/system'].target).toBe('http://localhost:18180')
      expect(config.server.proxy['/module-health/portal'].target).toBe('http://localhost:18184')
      expect(config.server.proxy['/workbench'].target).toBe('http://localhost:15190')
    } finally {
      for (const [name, value] of Object.entries(previous)) {
        if (value === undefined) delete process.env[name]
        else process.env[name] = value
      }
    }
  })

  it('真实 Vite 在解析后的端口提供页面、HMR 和 Gateway 代理', async () => {
    const vitePort = await availablePort()
    const gateway = createHttpServer((request, response) => {
      if (request.url === '/api/__port_probe') {
        response.setHeader('x-addp-port-fixture', 'gateway')
        response.end('gateway-ok')
      } else if (request.url === '/health/ready') {
        response.end('system-ok')
      } else {
        response.writeHead(404).end()
      }
    })
    await new Promise((resolve) => gateway.listen(0, resolve))
    const gatewayPort = gateway.address().port
    let log = ''
    const vite = spawn('npm', ['run', 'dev', '--', '--host', '127.0.0.1', '--port', String(vitePort), '--strictPort'], {
      cwd: fileURLToPath(new URL('..', import.meta.url)),
      detached: true,
      env: {
        ...process.env,
        NODE_ENV: 'development',
        CONSOLE_FE_PORT: String(vitePort),
        GATEWAY_PORT: String(gatewayPort),
        SYSTEM_BACKEND_PORT: String(gatewayPort),
        VITE_ADDP_FRONTEND_PORTS: `console:${vitePort},system:15173`,
      },
      stdio: ['ignore', 'pipe', 'pipe'],
    })
    for (const stream of [vite.stdout, vite.stderr]) {
      stream.on('data', (chunk) => { log = (log + chunk.toString()).slice(-10000) })
    }
    try {
      const origin = `http://127.0.0.1:${vitePort}`
      await waitForVite(origin, vite, () => log)
      const hmrClient = await (await fetch(`${origin}/@vite/client`)).text()
      expect(hmrClient).toContain(`const hmrPort = ${vitePort};`)
      const module = await (await fetch(`${origin}/src/config/portalConfig.js`)).text()
      expect(module).toContain(`console:${vitePort}`)
      const api = await fetch(`${origin}/api/__port_probe`)
      expect(api.status).toBe(200)
      expect(api.headers.get('x-addp-port-fixture')).toBe('gateway')
      expect(await api.text()).toBe('gateway-ok')
      const health = await fetch(`${origin}/module-health/system`)
      expect(health.status).toBe(200)
      expect(await health.text()).toBe('system-ok')
    } finally {
      await stopProcessGroup(vite)
      await new Promise((resolve) => gateway.close(resolve))
    }
  }, 45000)
})
