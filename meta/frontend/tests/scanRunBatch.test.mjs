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
  assert.deepEqual(scanRunFailureSamples(results[0].run, results[0].error, key => key), [
    { target: '', message: 'common.executionFailure.execution_failed' }
  ])
})

test('failure summary uses safe categories and ignores raw target and error values', () => {
  for (const category of ['permission_denied', 'invalid_input', 'child_failed', 'connection_failed', 'resource_exhausted', 'owner_unavailable', 'submission_uncertain', 'coordinator_lost', 'timeout', 'cancelled', 'execution_failed', 'password=secret']) {
    const run = { error_details: { category, message: 'secret', failed_target_samples: [{ target: 'private', message: 'secret' }] } }
    assert.deepEqual(scanRunFailureSamples(run, new Error('secret'), key => key), [{
      target: '', message: `common.executionFailure.${category === 'password=secret' ? 'execution_failed' : category}`
    }])
  }
})
