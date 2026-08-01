import test from 'node:test'
import assert from 'node:assert/strict'

import {
  executionStatusLabelKey,
  executionStatusTagType
} from '../src/utils/executionStatus.mjs'

test('cancelled execution is distinct from failed execution', () => {
  assert.equal(executionStatusLabelKey('cancelled'), 'transfer.executionStatus.cancelled')
  assert.equal(executionStatusTagType('cancelled'), 'info')
  assert.equal(executionStatusLabelKey('failed'), 'transfer.executionStatus.failed')
  assert.equal(executionStatusTagType('failed'), 'danger')
})

test('missing execution status uses the pending label', () => {
  assert.equal(executionStatusLabelKey('pending'), 'transfer.executionStatus.pending')
  assert.equal(executionStatusLabelKey(''), 'transfer.executionStatus.pending')
})
