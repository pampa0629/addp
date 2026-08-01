import test from 'node:test'
import assert from 'node:assert/strict'

import { hasAtomicPartitionedTableChangeApply, hasIdempotentTableUpsert } from '../src/utils/transferDisplay.js'

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
