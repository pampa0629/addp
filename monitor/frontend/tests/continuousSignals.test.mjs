import test from 'node:test'
import assert from 'node:assert/strict'

import {
  buildContinuousPartitionRows,
  buildContinuousSignals,
  getContinuousCapture,
  hasContinuousExecutionMetadata
} from '../../../common-frontend/basic/src/utils/continuousExecution.js'

const now = Date.parse('2026-07-15T08:00:00Z')

test('prioritizes circuit and retention critical signals', () => {
  const signals = buildContinuousSignals({
    recovery_reason: 'execution_failed',
    recovery_consecutive_failures: 5,
    recovery_not_before: '2026-07-15T08:05:00Z',
    recovery_circuit_state: 'open',
    continuous: {
      diagnostics: {
        health: 'critical',
        checkpoint_health: 'degraded'
      }
    }
  }, 'pending', now)

  assert.deepEqual(signals.map(signal => [signal.code, signal.severity]), [
    ['recovery_circuit_open', 'critical'],
    ['retention_critical', 'critical'],
    ['checkpoint_stalled', 'warning']
  ])
})

test('derives recovery waiting without inventing a persisted alert state', () => {
  const signals = buildContinuousSignals({
    recovery_reason: 'lease_expired',
    recovery_consecutive_failures: 2,
    recovery_not_before: '2026-07-15T08:00:10Z',
    recovery_circuit_state: 'closed'
  }, 'pending', now)

  assert.equal(signals.length, 1)
  assert.equal(signals[0].code, 'recovery_waiting')
  assert.equal(signals[0].recovery.waitMilliseconds, 10000)
})

test('maps owner-provided checkpoint diagnostics into partition rows', () => {
  const rows = buildContinuousPartitionRows({
    continuous: {
      diagnostics: {
        partitions: {
          0: {
            next_offset: 80,
            latest_offset: 100,
            checkpoint_age_seconds: 360,
            checkpoint_health: 'degraded'
          }
        }
      }
    }
  })

  assert.equal(rows.length, 1)
  assert.equal(rows[0].checkpointAgeSeconds, 360)
  assert.equal(rows[0].checkpointHealth, 'degraded')
})

test('identifies continuous execution metadata without relying on progress', () => {
  assert.equal(hasContinuousExecutionMetadata({ continuous: { owner_instance_id: 'worker-a' } }), true)
  assert.equal(hasContinuousExecutionMetadata({ recovery_reason: 'worker_shutdown' }), true)
  assert.equal(hasContinuousExecutionMetadata({}), false)
})

test('derives schema change blocked only from the owner pending projection', () => {
  const blocked = buildContinuousSignals({
    continuous: { schema_change: { request_id: 9, status: 'pending', unexpected_fields: ['new_field'] } }
  }, 'failed', now)
  assert.deepEqual(blocked.map(signal => [signal.code, signal.severity]), [['schema_change_blocked', 'critical']])
  assert.equal(blocked[0].schemaChange.request_id, 9)

  for (const status of ['applied', 'stopped']) {
    assert.equal(buildContinuousSignals({ continuous: { schema_change: { status } } }, 'failed', now).length, 0)
  }
})

test('derives source recovery and observation availability signals without transaction thresholds', () => {
  const critical = buildContinuousSignals({
    continuous: {
      capture: {
        generation: 1,
        source_recovery: { health: 'critical', capture_position: '100', earliest_available_position: '110' },
        source_transactions: { status: 'available', active_count: 3, oldest_duration_seconds: 86400, used_undo_blocks: '999999' }
      }
    }
  }, 'running', now)
  assert.deepEqual(critical.map(signal => [signal.code, signal.severity]), [['source_recovery_critical', 'critical']])

  const unavailable = buildContinuousSignals({
    continuous: {
      capture: {
        source_recovery: { health: 'unknown' },
        source_transactions: { status: 'unavailable' }
      }
    }
  }, 'running', now)
  assert.deepEqual(unavailable.map(signal => [signal.code, signal.severity]), [
    ['source_recovery_unavailable', 'warning'],
    ['source_transactions_unavailable', 'warning']
  ])

  assert.equal(buildContinuousSignals({ continuous: { capture: { generation: 1 } } }, 'running', now).length, 0)
  assert.deepEqual(getContinuousCapture({ continuous: { capture: { generation: 1 } } }), { generation: 1 })
  assert.deepEqual(getContinuousCapture({ continuous: { capture: [] } }), {})
})
