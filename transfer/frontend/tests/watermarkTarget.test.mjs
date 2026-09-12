import test from 'node:test'
import assert from 'node:assert/strict'

import {
  hasAtomicPartitionedTableChangeApply,
  hasBoundedWatermarkRead,
  hasContentWriteCapability,
  hasIdempotentTableUpsert,
  hasNativeTableWriteCapability,
  hasStorageCapability,
  isNativeTableEngine,
  queryReadSessionCapability
} from '../src/utils/transferDisplay.js'

test('resource-tree engine projection keeps event stream storage sources', () => {
  assert.equal(hasStorageCapability({
    engine_type: 'kafka',
    capabilities: {
      engine_family: 'event_stream',
      storage: { catalog: { supported: true } }
    }
  }), true)
  assert.equal(hasStorageCapability({
    engine_type: 'spark',
    capabilities: {
      engine_family: 'compute'
    }
  }), false)
})

test('native table classification is capability-driven for independently named engines', () => {
  assert.equal(isNativeTableEngine({
    engine_type: 'opengauss',
    capabilities: { engine_family: 'tabular', storage: {} }
  }), true)
  assert.equal(isNativeTableEngine({ engine_type: 'postgresql' }), false)
})

test('query source languages and defaults come only from the read-session capability', () => {
  assert.deepEqual(queryReadSessionCapability({
    engine_type: 'independent-relational-engine',
    capabilities: JSON.stringify({
      engine_family: 'tabular',
      compute: {
        query: {
          supported: true,
          read_session: true,
          languages: ['SQL'],
          default_language: 'sql',
          identifier_quotes: { sql: '`', mql: '"' },
          parameters: { supported: true, languages: ['sql'], types: ['string'] }
        }
      }
    })
  }), {
    engineFamily: 'tabular',
    languages: ['sql'],
    defaultLanguage: 'sql',
    identifierQuotes: { sql: '`' },
    parameterLanguages: new Set(['sql']),
    parameterTypes: new Set(['string'])
  })
  assert.equal(queryReadSessionCapability({
    capabilities: { compute: { query: { supported: true, read_session: false, languages: ['mql'] } } }
  }), null)
  assert.equal(queryReadSessionCapability({ engine_type: 'postgresql' }), null)
})

test('watermark source requires declared bounded watermark read', () => {
  assert.equal(hasBoundedWatermarkRead({
    capabilities: {
      storage: {
        store: { bounded_watermark_read: true }
      }
    }
  }), true)

  assert.equal(hasBoundedWatermarkRead({
    capabilities: JSON.stringify({
      storage: {
        store: { bounded_watermark_read: false }
      }
    })
  }), false)

  assert.equal(hasBoundedWatermarkRead({ engine_type: 'postgresql' }), false)
  assert.equal(hasBoundedWatermarkRead({ engine_type: 'mysql' }), false)
})

test('watermark target requires declared idempotent table upsert', () => {
  assert.equal(hasIdempotentTableUpsert({
    engine_type: 'oceanbase',
    capabilities: {
      storage: {
        store: {
          table_upsert: { supported: true, idempotent: true }
        }
      }
    }
  }), true)

  assert.equal(hasIdempotentTableUpsert({
    capabilities: JSON.stringify({
      storage: {
        store: {
          table_upsert: { supported: true, idempotent: false }
        }
      }
    })
  }), false)

  assert.equal(hasIdempotentTableUpsert({ engine_type: 'mysql' }), false)
})

test('snapshot targets require declared write providers instead of engine-family inference', () => {
  assert.equal(hasNativeTableWriteCapability({
    engine_type: 'postgresql',
    capabilities: {
      storage: {
        store: {
          table_write_prepare: true,
          table_write_session: true
        }
      }
    }
  }), true)

  assert.equal(hasNativeTableWriteCapability({
    engine_type: 'oceanbase',
    capabilities: {
      engine_family: 'tabular',
      storage: {
        store: { batch_read: true }
      }
    }
  }), false)
  assert.equal(hasNativeTableWriteCapability({ engine_type: 'mysql' }), false)

  assert.equal(hasContentWriteCapability({
    capabilities: {
      engine_family: 'object',
      storage: {
        store: { stream_write: true }
      }
    }
  }), true)
  assert.equal(hasContentWriteCapability({ engine_type: 's3' }), false)
})

test('continuous target requires atomic monotonic apply operations', () => {
  const engine = {
    capabilities: {
      storage: {
        store: {
          partitioned_table_change_apply: {
            supported: true,
            atomic_position_commit: true,
            monotonic: true,
            position_types: ['kafka_offset/v1'],
            operations: ['upsert', 'delete', 'skip']
          }
        }
      }
    }
  }
  assert.equal(hasAtomicPartitionedTableChangeApply(engine), true)
  assert.equal(hasAtomicPartitionedTableChangeApply(engine, ['upsert', 'delete']), true)
  assert.equal(hasAtomicPartitionedTableChangeApply(engine, ['truncate']), false)
  assert.equal(hasAtomicPartitionedTableChangeApply({ engine_type: 'mysql' }), false)
})
