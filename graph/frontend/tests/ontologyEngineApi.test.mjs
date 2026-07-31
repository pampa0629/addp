import { beforeEach, describe, expect, it, vi } from 'vitest'

import client from '../src/api/client'
import { engineAPI } from '../src/api/ontology'

vi.mock('../src/api/client', () => ({
  default: {
    get: vi.fn(),
    post: vi.fn()
  }
}))

describe('ontology engine API', () => {
  beforeEach(() => {
    client.get.mockReset()
  })

  it('returns the complete engine array already extracted by the shared client', async () => {
    const engines = [{ id: 7, engine_type: 'neo4j' }]
    client.get.mockResolvedValue(engines)

    await expect(engineAPI.getNeo4jEngines()).resolves.toBe(engines)
    expect(client.get).toHaveBeenCalledWith('/system/engines', {
      params: { engine_type: 'neo4j' }
    })
  })
})
