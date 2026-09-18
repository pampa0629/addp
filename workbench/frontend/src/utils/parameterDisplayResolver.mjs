import { createLatestRequestCoordinator } from '../../../../common-frontend/basic/src/utils/latestRequest.js'
import { parameterDisplayResult } from './applicationParameterDisplay.mjs'

export function createParameterDisplayResolver(execute, commit) {
  const requests = createLatestRequestCoordinator()
  return {
    invalidate() { requests.invalidate() },
    async resolve(query, initialStatus = 'selected') {
      const request = requests.begin('parameter-name')
      commit({ status: query ? 'loading' : initialStatus, label: '' })
      if (!query) return
      try {
        const response = await execute(query.operation, query.body)
        if (requests.isCurrent(request, 'parameter-name')) commit(parameterDisplayResult(query, response.data))
      } catch {
        if (requests.isCurrent(request, 'parameter-name')) commit({ status: 'failed', label: '' })
      }
    },
  }
}
