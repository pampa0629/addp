import { describe, expect, it } from 'vitest'
import { loadConfigFromFile } from 'vite'
import { spawn } from 'node:child_process'
import { createServer as createHttpServer } from 'node:http'
import { createServer as createTcpServer } from 'node:net'
import { setTimeout as delay } from 'node:timers/promises'
import { fileURLToPath } from 'node:url'
import { mkdtemp, readdir, access, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
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
  if (!child.pid || (child.stdout.closed && child.stderr.closed)) return
  // npm can exit before Vite: inherited pipes close after its worker shuts down.
  let hasClosed = false
  const closed = new Promise((resolve) => child.once('close', () => {
    hasClosed = true
    resolve()
  }))
  try { process.kill(-child.pid, 'SIGTERM') } catch (error) {
    if (error.code !== 'ESRCH') throw error
  }
  await Promise.race([closed, delay(5000, undefined, { ref: false })])
  if (!hasClosed) {
    try { process.kill(-child.pid, 'SIGKILL') } catch (error) {
      if (error.code !== 'ESRCH') throw error
    }
    await closed
  }
}

describe('开发端口传递', () => {
  it('等待启动包装进程退出后的子进程完成清理', async () => {
    const runtimeDir = await mkdtemp(join(tmpdir(), 'addp-console-stop-'))
    const cacheFile = join(runtimeDir, 'owned-cache')
    const worker = `
      const { writeFileSync, rmSync } = require('node:fs')
      writeFileSync(process.argv[1], 'owned cache')
      process.on('SIGTERM', () => setTimeout(() => {
        rmSync(process.argv[1], { force: true })
        process.exit(0)
      }, 200))
      setInterval(() => {}, 1000)
      process.send('ready')
    `
    const wrapper = spawn(process.execPath, ['-e', `
      const { spawn } = require('node:child_process')
      const child = spawn(process.execPath, ['-e', ${JSON.stringify(worker)}, ${JSON.stringify(cacheFile)}], {
        stdio: ['ignore', 'inherit', 'inherit', 'ipc'],
      })
      process.on('SIGTERM', () => process.exit(0))
      child.once('message', () => process.stdout.write('ready'))
    `], { detached: true, stdio: ['ignore', 'pipe', 'pipe'] })
    const closed = new Promise(resolve => wrapper.once('close', resolve))
    try {
      await new Promise((resolve, reject) => {
        wrapper.stdout.once('data', resolve)
        wrapper.once('error', reject)
        wrapper.once('exit', () => reject(new Error('Wrapper exited before its worker was ready')))
      })
      await stopProcessGroup(wrapper)
      expect(await readdir(runtimeDir)).toEqual([])
    } finally {
      if (!wrapper.stdout.closed || !wrapper.stderr.closed) {
        try { process.kill(-wrapper.pid, 'SIGKILL') } catch (error) {
          if (error.code !== 'ESRCH') throw error
        }
      }
      await closed
      await rm(runtimeDir, { recursive: true, force: true })
    }
  }, 10000)

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
      expect(config.server.proxy['/data-apps'].target).toBe('http://localhost:15190')
      expect(config.server.proxy['/module-ui/workbench'].target).toBe('http://localhost:15190')
      expect(config.server.proxy['/data-apps'].rewrite('/data-apps/application-a')).toBe('/module-ui/workbench/data-apps/application-a')
      expect(config.server.proxy).not.toHaveProperty('/workbench')
    } finally {
      for (const [name, value] of Object.entries(previous)) {
        if (value === undefined) delete process.env[name]
        else process.env[name] = value
      }
    }
  })

  it('真实 Vite 在解析后的端口提供页面、HMR 和 Gateway 代理', async () => {
    const runtimeDir = await mkdtemp(join(tmpdir(), 'addp-console-hmr-'))
    const testEnvironment = {
      ...process.env,
      NODE_ENV: 'development',
      ADDP_E2E: '1',
      ADDP_BROWSER_RUN: JSON.stringify({ runtimeDir, hmr: true }),
    }
    const vitePort = await availablePort()
    const modulePort = await availablePort()
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
        ...testEnvironment,
        CONSOLE_FE_PORT: String(vitePort),
        GATEWAY_PORT: String(gatewayPort),
        SYSTEM_BACKEND_PORT: String(gatewayPort),
        VITE_ADDP_FRONTEND_PORTS: `console:${vitePort},system:${modulePort}`,
      },
      stdio: ['ignore', 'pipe', 'pipe'],
    })
    for (const stream of [vite.stdout, vite.stderr]) {
      stream.on('data', (chunk) => { log = (log + chunk.toString()).slice(-10000) })
    }
    const moduleServer = spawn('npm', ['--prefix', '../../system/frontend', 'run', 'dev', '--', '--host', '127.0.0.1', '--port', String(modulePort), '--strictPort'], {
      cwd: fileURLToPath(new URL('..', import.meta.url)),
      detached: true,
      env: { ...testEnvironment, VITE_ADDP_CONSOLE_PORT: String(vitePort) },
      stdio: ['ignore', 'pipe', 'pipe'],
    })
    for (const stream of [moduleServer.stdout, moduleServer.stderr]) {
      stream.on('data', (chunk) => { log = (log + chunk.toString()).slice(-10000) })
    }
    let socket
    try {
      const origin = `http://127.0.0.1:${vitePort}`
      await waitForVite(origin, vite, () => log)
      await waitForVite(`http://127.0.0.1:${modulePort}/module-ui/system/`, moduleServer, () => log)
      const hmrClient = await (await fetch(`${origin}/@vite/client`)).text()
      expect(hmrClient).toContain(`const hmrPort = ${vitePort};`)
      const module = await (await fetch(`${origin}/src/config/portalConfig.js`)).text()
      expect(module).toContain('`${window.location.origin}/module-ui/${module}/`')
      expect(module).not.toContain('15173')
      const moduleHTML = await (await fetch(`${origin}/module-ui/system/`)).text()
      expect(moduleHTML).toContain('/module-ui/system/src/main.js')
      const moduleHMR = await (await fetch(`${origin}/module-ui/system/@vite/client`)).text()
      expect(moduleHMR).toContain(`const hmrPort = ${vitePort};`)
      expect(moduleHMR).toContain('/module-ui/system/__hmr')
      socket = new WebSocket(`ws://127.0.0.1:${vitePort}/module-ui/system/__hmr`, 'vite-hmr')
      const connected = await new Promise((resolve, reject) => {
        const timeout = setTimeout(() => reject(new Error('Module HMR proxy did not connect')), 5000)
        socket.addEventListener('message', event => { clearTimeout(timeout); resolve(JSON.parse(event.data)) }, { once: true })
        socket.addEventListener('error', () => { clearTimeout(timeout); reject(new Error('Module HMR proxy failed')) }, { once: true })
      })
      expect(connected).toEqual({ type: 'connected' })
      const api = await fetch(`${origin}/api/__port_probe`)
      expect(api.status).toBe(200)
      expect(api.headers.get('x-addp-port-fixture')).toBe('gateway')
      expect(await api.text()).toBe('gateway-ok')
      const health = await fetch(`${origin}/module-health/system`)
      expect(health.status).toBe(200)
      expect(await health.text()).toBe('system-ok')
      const caches = (await readdir(runtimeDir)).filter(name => name.includes('-vite-test-'))
      expect(caches.map(name => name.replace(/-\d+$/, '')).sort()).toEqual([
        'addp-console-vite-test', 'addp-system-vite-test',
      ])
      // Both real optimizers must have written only into this test's runtime.
      await expect.poll(async () => Promise.all(caches.map(name =>
        access(join(runtimeDir, name, 'deps/_metadata.json')).then(() => true, () => false)
      )), { timeout: 5000 }).toEqual([true, true])
    } finally {
      try {
        if (socket && socket.readyState !== WebSocket.CLOSED) {
          await new Promise((resolve, reject) => {
            const timeout = setTimeout(() => reject(new Error('Module HMR socket did not close')), 5000)
            socket.addEventListener('close', () => { clearTimeout(timeout); resolve() }, { once: true })
            socket.close()
          })
        }
      } finally {
        await stopProcessGroup(moduleServer)
        await stopProcessGroup(vite)
        await new Promise((resolve) => gateway.close(resolve))
        try {
          expect(await readdir(runtimeDir), log).toEqual([])
        } finally {
          await rm(runtimeDir, { recursive: true, force: true })
        }
      }
    }
  }, 45000)
})
