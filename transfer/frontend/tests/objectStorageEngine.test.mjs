import assert from 'node:assert/strict'
import test from 'node:test'
import { isObjectStorageEngine } from '../src/utils/transferDisplay.js'

const capabilities = {
  engine_family: 'object',
  storage: {
    catalog: { supported: true },
    catalog_model: {
      path_version: 'catalog.path/v1', root_term: 'service',
      levels: [
        { term: 'bucket', role: 'branch' },
        { term: 'prefix', role: 'branch', optional: true },
        { term: 'object', role: 'leaf' }
      ]
    }
  }
}

test('object endpoint paths follow the declared catalog rather than the engine name', () => {
  for (const engine_type of ['rustfs', 's3', 'minio', 'synthetic-storage']) {
    assert.equal(isObjectStorageEngine({ engine_type, capabilities }), true)
    assert.equal(isObjectStorageEngine({ engine_type, capabilities: JSON.stringify(capabilities) }), true)
  }
  for (const engine_type of ['rustfs', 's3', 'minio']) {
    assert.equal(isObjectStorageEngine({ engine_type }), false)
    assert.equal(isObjectStorageEngine({ engine_type, capabilities: { engine_family: 'object' } }), false)
  }
})

test('file catalogs and unavailable object catalogs do not create object locators', () => {
  const file = structuredClone(capabilities)
  file.storage.catalog_model.root_term = 'root'
  file.storage.catalog_model.levels = [{ term: 'file', role: 'leaf' }]
  assert.equal(isObjectStorageEngine({ engine_type: 'rustfs', capabilities: file }), false)
  const unavailable = structuredClone(capabilities)
  unavailable.storage.catalog.supported = false
  assert.equal(isObjectStorageEngine({ capabilities: unavailable }), false)
})
