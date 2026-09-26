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

export const scanRunFailureSamples = (run, error) => {
  const details = run?.error_details
  const samples = Array.isArray(details?.failed_target_samples)
    ? details.failed_target_samples.filter(sample => sample && (sample.target || sample.message))
    : []
  if (samples.length > 0) {
    return samples.map(sample => ({
      target: String(sample.target || ''),
      message: String(sample.message || '')
    }))
  }
  return [{
    target: '',
    message: String(details?.message || error?.message || run?.error_message || run?.error || run?.status || '')
  }]
}
