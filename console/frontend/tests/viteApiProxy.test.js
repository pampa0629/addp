import { createServer as createGateway, get } from 'node:http'
import { once } from 'node:events'
import { fileURLToPath } from 'node:url'
import { createServer as createViteServer, loadConfigFromFile } from 'vite'
import { expect, it, vi } from 'vitest'

it('forwards Notebook kernel upgrades through the configured Gateway with the browser origin intact', async () => {
  const gateway = createGateway()
  gateway.listen(0, '127.0.0.1')
  await once(gateway, 'listening')
  let vite
  try {
    vi.stubEnv('GATEWAY_PORT', String(gateway.address().port))
    const { config } = await loadConfigFromFile(
      { command: 'serve', mode: 'development' },
      fileURLToPath(new URL('../vite.config.js', import.meta.url))
    )
    vite = await createViteServer({
      configFile: false, logLevel: 'silent', appType: 'custom',
      server: { host: '127.0.0.1', port: 0, hmr: false, proxy: config.server.proxy },
      optimizeDeps: { noDiscovery: true }
    })
    await vite.listen()
    const host = '127.0.0.1:' + vite.httpServer.address().port
    const origin = 'http://' + host
    const path = '/api/v1/develop/notebook-sessions/test-session/api/kernels/test-kernel/channels'
    let received
    gateway.once('upgrade', (req, socket) => {
      received = { path: req.url, host: req.headers.host, origin: req.headers.origin }
      socket.end('HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n' +
        'Sec-WebSocket-Accept: s3pPLMBiTxaQ9kYGzzhZRbK+xOo=\r\n\r\n')
    })
    const status = await new Promise((resolve, reject) => {
      const req = get(origin + path, {
        signal: AbortSignal.timeout(3000),
        headers: {
          Connection: 'Upgrade', Upgrade: 'websocket', Origin: origin,
          'Sec-WebSocket-Version': '13', 'Sec-WebSocket-Key': 'dGhlIHNhbXBsZSBub25jZQ=='
        }
      })
      req.once('error', reject)
      req.once('upgrade', (response, socket) => {
        socket.destroy()
        resolve(response.statusCode)
      })
      req.once('response', response => { response.resume(); resolve(response.statusCode) })
    })
    expect(status).toBe(101)
    expect(received).toEqual({ path, host, origin })
  } finally {
    await vite?.close()
    await new Promise(resolve => gateway.close(resolve))
    vi.unstubAllEnvs()
  }
}, 10000)
