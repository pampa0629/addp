import { describe, expect, it } from 'vitest'

import { switchStorageEngineType } from './engineForm'

describe('engine registration form', () => {
  it('clears type-specific connection fields when switching engine type', () => {
    const form = {
      engine_type: 'postgresql',
      name: 'Business Oracle',
      description: '业务数据库',
      lifecycle_state: 'active',
      connection_info: {
        host: 'localhost',
        port: 5432,
        database: 'addp',
        user: 'postgres'
      }
    }

    expect(switchStorageEngineType(form, 'oracle')).toEqual({
      engine_type: 'oracle',
      name: 'Business Oracle',
      description: '业务数据库',
      lifecycle_state: 'active',
      connection_info: {}
    })
  })
})
