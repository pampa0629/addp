import assert from 'node:assert/strict'
import test from 'node:test'
import { executionStepStates } from '../src/utils/executionPresentation.js'
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
