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

  it('requests a cross-module, filtered, paginated instance list', async () => {
    client.get.mockResolvedValue({ data: [], total: 0, page: 2, page_size: 20 })

    await modulesAPI.listInstances({
      module_name: 'agent',
      registered_host: 'worker.local',
      role: 'worker',
      status: 'down',
      time_basis: 'offline',
      time_from: '2026-09-29T00:00:00Z',
      page: 2,
      page_size: 20
    })

    expect(client.get).toHaveBeenCalledWith(
      '/system/platform/module-instances',
      {
        params: {
          module_name: 'agent',
          registered_host: 'worker.local',
          role: 'worker',
          status: 'down',
          time_basis: 'offline',
          time_from: '2026-09-29T00:00:00Z',
          page: 2,
          page_size: 20
        }
      }
    )
  })
})

describe('modules view contract', () => {
  it('internationalizes module and instance identifiers in both views', () => {
    const overview = readFileSync(
      new URL('../src/views/Modules.vue', import.meta.url),
      'utf8'
    )
    const query = readFileSync(new URL('../src/components/ModuleInstances.vue', import.meta.url), 'utf8')

    expect(overview).not.toContain('label="ID"')
    expect(query).not.toContain('label="ID"')
    expect(overview).toContain("t('system.module.columns.id')")
    expect(query).toContain("t('system.module.instances.id')")
  })
})
