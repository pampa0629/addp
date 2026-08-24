import { readFileSync } from 'node:fs'

import { beforeEach, describe, expect, it, vi } from 'vitest'

import client from '../src/api/client'
import { modulesAPI } from '../src/api/modules'

vi.mock('../src/api/client', () => ({
  default: {
    get: vi.fn(),
    put: vi.fn()
  }
}))

describe('modules API', () => {
  beforeEach(() => {
    client.get.mockReset()
  })

  it('requests a filtered, paginated module instance history', async () => {
    client.get.mockResolvedValue({ data: [], total: 0, page: 2, page_size: 20 })

    await modulesAPI.listInstances('agent/copilot', {
      role: 'worker',
      status: 'down',
      page: 2,
      page_size: 20
    })

    expect(client.get).toHaveBeenCalledWith(
      '/system/platform/modules/agent%2Fcopilot/instances',
      {
        params: {
          role: 'worker',
          status: 'down',
          page: 2,
          page_size: 20
        }
      }
    )
  })
})

describe('modules view contract', () => {
  it('internationalizes definition and history record identifiers', () => {
    const source = readFileSync(
      new URL('../src/views/Modules.vue', import.meta.url),
      'utf8'
    )

    expect(source).not.toContain('label="ID"')
    expect(source.match(/t\('system\.module\.columns\.id'\)/g)).toHaveLength(2)
  })
})
