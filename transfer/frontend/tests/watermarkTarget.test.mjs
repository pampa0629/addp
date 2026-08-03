import test from 'node:test'
import assert from 'node:assert/strict'

import {
  hasAtomicPartitionedTableChangeApply,
  hasBoundedWatermarkRead,
  hasIdempotentTableUpsert,
  hasStorageCapability
} from '../src/utils/transferDisplay.js'

test('resource-tree engine projection keeps event stream storage sources', () => {
  assert.equal(hasStorageCapability({
    engine_type: 'kafka',
    engine_family: 'event_stream'
  }), true)
  assert.equal(hasStorageCapability({
    engine_type: 'spark',
    engine_family: 'compute'
  }), false)
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
