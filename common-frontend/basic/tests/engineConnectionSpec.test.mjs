import assert from 'node:assert/strict'
import test from 'node:test'

import {
  applyConnectionSpecDefaults,
  buildConnectionRules,
  visibleConnectionFields
} from '../src/utils/engineConnectionSpec.js'
import { getEngineFamily, getEngineFamilyLabelKey } from '../src/utils/engineDisplay.js'

const connectionSpec = {
  fields: [
    { key: 'endpoint', label_key: 'endpoint', input: 'text', required: true, default: 'localhost' },
    { key: 'security', label_key: 'security', input: 'select', required: true, default: 'plain' },
    {
      key: 'password',
      label_key: 'password',
      input: 'password',
      required: true,
      sensitive: true,
      visible_when: { field: 'security', values: ['secure'] }
    }
  ]
}

test('Redis connection preserves database zero and false TLS through form defaults', () => {
  const spec = { fields: [
    { key: 'database', input: 'number', default: 0, min: 0 },
    { key: 'use_ssl', input: 'boolean', default: false }
  ] }
  assert.deepEqual(applyConnectionSpecDefaults(spec, {}), { database: 0, use_ssl: false })
  assert.deepEqual(applyConnectionSpecDefaults(spec, { database: 0, use_ssl: false }), { database: 0, use_ssl: false })
  const family = getEngineFamily({ capabilities_view: { summary: [{ id: 'engine_family', value_key: 'system.engine.capabilityView.engineFamily.keyValue' }] } })
  assert.equal(family, 'key_value')
  assert.equal(getEngineFamilyLabelKey(family), 'system.engine.capabilities.keyValue')
})

test('connection spec applies defaults without engine-type branches', () => {
  assert.deepEqual(applyConnectionSpecDefaults(connectionSpec, {}), {
    endpoint: 'localhost',
    security: 'plain',
    password: ''
  })
})

test('connection spec drives conditional fields and rules together', () => {
  assert.deepEqual(
    visibleConnectionFields(connectionSpec, { security: 'plain' }).map(field => field.key),
    ['endpoint', 'security']
  )
  const secureInfo = { security: 'secure' }
  assert.deepEqual(
    visibleConnectionFields(connectionSpec, secureInfo).map(field => field.key),
    ['endpoint', 'security', 'password']
  )
  assert.ok(buildConnectionRules(connectionSpec, secureInfo, key => key)['connection_info.password'])
})

test('connection spec preserves masked sensitive values generically', () => {
  assert.deepEqual(
    applyConnectionSpecDefaults(connectionSpec, { security: 'secure', password: '******' }),
    { endpoint: 'localhost', security: 'secure', password: '********', _has_password: true }
  )
})

test('new sensitive input replaces the stored mask without being reset', () => {
  assert.deepEqual(
    applyConnectionSpecDefaults(connectionSpec, { security: 'secure', password: 'newSecret123', _has_password: true }),
    { endpoint: 'localhost', security: 'secure', password: 'newSecret123' }
  )
  assert.deepEqual(
    applyConnectionSpecDefaults(connectionSpec, { security: 'secure', password: 'name****suffix' }),
    { endpoint: 'localhost', security: 'secure', password: 'name****suffix' }
  )
})
