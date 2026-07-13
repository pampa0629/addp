import test from 'node:test'
import assert from 'node:assert/strict'

import {
  buildContinuousPartitionRows,
  continuousHealthTagType,
  formatContinuousDurationSeconds
} from '../../../common-frontend/basic/src/utils/continuousExecution.js'

test('merges committed positions with continuous diagnostics', () => {
  const rows = buildContinuousPartitionRows({
    continuous: {
      partitions: {
        0: { type: 'kafka_offset', version: 'v1', values: { next_offset: '42' } }
      },
      diagnostics: {
        partitions: {
          0: {
            earliest_offset: 10,
            latest_offset: 50,
            next_offset: 42,
            lag_records: 8,
            recovery_headroom_records: 32,
            retention_horizon_seconds: 3600,
            health: 'degraded'
          },
          1: { earliest_offset: 0, latest_offset: 0, health: 'unknown' }
        }
      }
    }
  })

  assert.equal(rows.length, 2)
  assert.deepEqual(rows[0], {
    partition: '0', nextOffset: 42, earliestOffset: 10, latestOffset: 50,
    lagRecords: 8, recoveryHeadroomRecords: 32, sourceRateRecordsPerSecond: null,
    retentionHorizonSeconds: 3600, health: 'degraded', positionType: 'kafka_offset/v1'
  })
  assert.equal(rows[1].nextOffset, null)
  assert.equal(rows[1].health, 'unknown')
})

test('formats continuous health and horizon without inventing unknown values', () => {
  assert.equal(continuousHealthTagType('critical'), 'danger')
  assert.equal(continuousHealthTagType('healthy'), 'success')
  assert.equal(formatContinuousDurationSeconds(null), '-')
  assert.equal(formatContinuousDurationSeconds(5400), '1.5h')
})
