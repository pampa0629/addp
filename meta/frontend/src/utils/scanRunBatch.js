export const waitForScanRuns = async (runs, waitForRun, hooks = {}) => {
  const results = []
  for (let index = 0; index < runs.length; index += 1) {
    const run = runs[index]
    try {
      const completed = await waitForRun(run, latest => {
        hooks.onProgress?.({ run: latest, index, total: runs.length })
      })
      results.push({ run: completed, error: null })
    } catch (error) {
      results.push({ run: error?.run || run, error })
    }
    hooks.onSettled?.({ index, total: runs.length })
  }
  return results
}

// Scan reads expose stable failure categories, never raw target/error bodies.
export const scanRunFailureSamples = (run, error, t) => [{
  target: '',
  message: t(`common.executionFailure.${failureCategory(run)}`)
}]

const failureCategory = run => {
  const category = run?.error_details?.category
  return ['permission_denied', 'invalid_input', 'child_failed', 'connection_failed', 'resource_exhausted', 'owner_unavailable', 'submission_uncertain', 'coordinator_lost', 'timeout', 'cancelled', 'execution_failed'].includes(category)
    ? category : 'execution_failed'
}
