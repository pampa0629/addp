import { describe, expect, it } from 'vitest'
import { hasEngineAddressChanged } from '../src/utils/engineForm'

describe('engine address change confirmation', () => {
  const descriptor = {
    connection_spec: {
      fields: [
        { key: 'endpoint', identity: true },
        { key: 'access_key', sensitive: true },
        { key: 'secret_key', sensitive: true }
      ]
    }
  }

  it('requires confirmation when an identity address field changes', () => {
    expect(hasEngineAddressChanged(descriptor, { endpoint: '127.0.0.1:9002' }, { endpoint: '127.0.0.1:19002' })).toBe(true)
  })

  it('does not require confirmation for credential rotation', () => {
    expect(hasEngineAddressChanged(descriptor,
      { endpoint: '127.0.0.1:9002', access_key: 'old' },
      { endpoint: '127.0.0.1:9002', access_key: 'new' }
    )).toBe(false)
  })
})
