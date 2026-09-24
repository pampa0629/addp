import { describe, expect, it } from 'vitest'
import { loadConfigFromFile } from 'vite'
import { parseDevPorts } from '../../../common-frontend/basic/src/utils/devPorts'

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
})
