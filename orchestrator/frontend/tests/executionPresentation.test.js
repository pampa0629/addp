import assert from 'node:assert/strict'
import test from 'node:test'
import { executionStepStates, executionFailureMessage } from '../src/utils/executionPresentation.js'
const steps = [{ id: 'a' }, { id: 'b' }, { id: 'c' }]

test('completed results, the running step, and pending successors have distinct states', () => {
  const execution = { status: 'running', current_step: 'b', metadata: { step_results: { a: { status: 'success', duration: 12 } } } }
  const before = JSON.stringify(execution)
  const states = executionStepStates(execution, steps)
  assert.deepEqual(Object.values(states).map(s => s.status), ['success', 'running', 'pending'])
  assert.equal(states.a.duration, 12)
  assert.equal(JSON.stringify(execution), before)
})
test('a failed execution never labels unexecuted successors as successful or running', () => {
  const states = executionStepStates({ status: 'failed', current_step: 'b', metadata: { step_results: {
    a: { status: 'success' }, b: { status: 'failed', error: 'bad input' }
  } } }, steps)
  assert.deepEqual(Object.values(states).map(s => s.status), ['success', 'failed', 'not_started'])
  assert.equal(states.b.error, 'bad input')
  assert.deepEqual(executionStepStates(null, steps), {})
})

test('a terminal parent preserves the recorded child state without presenting it as live', () => {
  for (const status of ['failed', 'timeout', 'cancelled']) {
    const execution = { status, progress: 50, metadata: { step_results: {
      a: { status: 'success' }, b: { status: 'running', phase: 'waiting', result: { execution_id: 'child-still-running' } }
    } } }
    const before = JSON.stringify(execution)
    const states = executionStepStates(execution, steps)
    assert.deepEqual(Object.values(states).map(s => s.status), ['success', 'running', 'not_started'])
    assert.deepEqual(Object.values(states).map(s => s.observationStopped), [false, true, false])
    assert.equal(states.b.executionId, 'child-still-running')
    assert.equal(JSON.stringify(execution), before)
  }
  const active = executionStepStates({ status: 'running', metadata: { step_results: { a: { status: 'running' } } } }, steps)
  assert.equal(active.a.observationStopped, false)
})

test('safe failures retain their category when there is no closed owner message', () => {
  const t = key => `translated:${key}`
  assert.equal(executionFailureMessage({ error_details: { category: 'connection_failed' } }, t), 'translated:common.executionFailure.connection_failed')
  assert.equal(executionFailureMessage({ error_details: { category: 'execution_failed', message: 'closed owner message' } }, t), 'closed owner message')
  assert.equal(executionFailureMessage(null, t), '')
  assert.equal(executionFailureMessage({ status: 'success' }, t), '')
})
