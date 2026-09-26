import assert from 'node:assert/strict'
import test from 'node:test'

import { scanRunFailureSamples, waitForScanRuns } from '../src/utils/scanRunBatch.js'

test('one failed run does not stop waiting for later submitted runs', async () => {
  const runs = [{ execution_id: 'first' }, { execution_id: 'second' }, { execution_id: 'third' }]
  const visited = []
  const progress = []
  const failedRun = {
    ...runs[0],
    status: 'failed',
    error_details: {
      message: 'one target failed',
      failed_target_samples: [{ target: 'data/empty.gdb', message: 'cannot inspect' }]
    }
  }

  const results = await waitForScanRuns(runs, async (run, onProgress) => {
    visited.push(run.execution_id)
    onProgress({ ...run, status: 'running' })
    if (run.execution_id === 'first') {
      const error = new Error('failed')
      error.run = failedRun
      throw error
    }
    return { ...run, status: 'success' }
  }, { onProgress: payload => progress.push(payload.index) })

  assert.deepEqual(visited, ['first', 'second', 'third'])
  assert.deepEqual(progress, [0, 1, 2])
  assert.deepEqual(results.map(result => result.error === null), [false, true, true])
  assert.deepEqual(scanRunFailureSamples(results[0].run, results[0].error), [
    { target: 'data/empty.gdb', message: 'cannot inspect' }
  ])
})

test('failure summary falls back to the execution error when no target sample exists', () => {
  assert.deepEqual(scanRunFailureSamples({ error_details: { message: 'connection refused' } }), [
    { target: '', message: 'connection refused' }
  ])
  assert.deepEqual(scanRunFailureSamples({}, new Error('request failed')), [
    { target: '', message: 'request failed' }
  ])
})
