import assert from 'node:assert/strict'
import test from 'node:test'

import {
  applyConnectionSpecDefaults,
  buildConnectionRules,
  visibleConnectionFields
} from '../src/utils/engineConnectionSpec.js'

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
  assert.deepEqual(
    applyConnectionSpecDefaults(connectionSpec, { security: 'secure', password: 'abcd****wxyz' }),
    { endpoint: 'localhost', security: 'secure', password: '********', _has_password: true }
  )
})
