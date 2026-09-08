import assert from 'node:assert/strict'
import test from 'node:test'

import {
  engineCapabilityFamily,
  hasDeclaredStorageCapability,
  isContentStorageEngine,
  isNativeTableStorageEngine,
  parseEngineCapabilities,
  queryFederationCapability,
  supportsQueryLanguage
} from '../src/utils/engineCapabilities.mjs'

const openGauss = {
  engine_type: 'opengauss',
  capabilities: JSON.stringify({
    schema_version: 'engine.capabilities/v1',
    engine_type: 'opengauss',
    engine_family: 'tabular',
    storage: { catalog: { supported: true }, store: { table_read_session: true } },
    compute: { query: { supported: true, languages: ['sql'] } }
  })
}

test('engine classification uses structured capabilities instead of engine type names', () => {
  assert.equal(engineCapabilityFamily(openGauss), 'tabular')
  assert.equal(hasDeclaredStorageCapability(openGauss), true)
  assert.equal(isNativeTableStorageEngine(openGauss), true)
  assert.equal(isContentStorageEngine(openGauss), false)
  assert.equal(supportsQueryLanguage(openGauss, 'SQL'), true)
  assert.equal(isNativeTableStorageEngine({ engine_type: 'postgresql' }), false)
})

test('federation source compatibility is read from the runtime contract', () => {
  const runtime = {
    capabilities: {
      compute: {
        query: {
          federation: {
            supported: true,
            source_engine_types: ['opengauss', 'minio'],
            object_formats: ['parquet']
          }
        }
      }
    }
  }
  const federation = queryFederationCapability(runtime)
  assert.equal(federation.sourceEngineTypes.has('opengauss'), true)
  assert.equal(federation.objectFormats.has('parquet'), true)
  assert.equal(parseEngineCapabilities('{invalid'), null)
})
