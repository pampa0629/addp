import assert from 'node:assert/strict'
import test from 'node:test'

import {
  isNativeTableEngine,
  isPMTilesEngine,
  isQueryableTableEngine
} from '../src/utils/resourceSelection.js'

const engine = (type, family, extra = {}) => ({
  engine_type: type,
  capabilities: {
    schema_version: 'engine.capabilities/v1',
    engine_type: type,
    engine_family: family,
    storage: {},
    ...extra
  }
})

test('native table selection follows capabilities and includes openGauss without a type list', () => {
  const openGauss = engine('opengauss', 'tabular', {
    compute: { query: { supported: true, languages: ['sql'] } }
  })
  assert.equal(isNativeTableEngine(openGauss), true)
  assert.equal(isQueryableTableEngine(openGauss), true)
  assert.equal(isNativeTableEngine({ engine_type: 'postgresql' }), false)
})

test('encoded table selection follows the selected federation runtime contract', () => {
  const minio = engine('minio', 'object')
  const nfs = engine('nfs', 'file')
  const runtime = {
    capabilities: {
      compute: {
        query: {
          federation: {
            supported: true,
            source_engine_types: ['minio'],
            object_formats: ['parquet']
          }
        }
      }
    }
  }
  assert.equal(isQueryableTableEngine(minio, [runtime]), true)
  assert.equal(isQueryableTableEngine(nfs, [runtime]), false)
  assert.equal(isPMTilesEngine(nfs), true)
})
