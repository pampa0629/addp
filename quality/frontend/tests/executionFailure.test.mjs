import assert from 'node:assert/strict'
import test from 'node:test'

import { executionFailureLabel } from '../src/utils/executionFailure.js'

const t = key => key

test('execution failure labels include bounded timeout terminal state', () => {
  assert.equal(executionFailureLabel({
    status: 'timeout',
    error_details: { code: 'quality.execution.timeout' }
  }, t), 'quality.execution.failureTimeout')
})

test('execution failure labels reject non-error terminal states', () => {
  assert.equal(executionFailureLabel({
    status: 'success',
    error_details: { code: 'quality.execution.timeout' }
  }, t), '')
})
