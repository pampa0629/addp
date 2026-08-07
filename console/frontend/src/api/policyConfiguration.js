import client from './client'

export const POLICY_CONFIGS = {
  develop: { get: '/develop/settings/query-policy', put: '/develop/settings/query-policy' },
  manager: { get: '/manager/settings/quick-view-policy', put: '/manager/settings/quick-view-policy' },
  copilot: { get: '/copilot/settings/matching-policy', put: '/copilot/settings/matching-policy' }
  ,transfer: { get: '/transfer/settings/continuous-policy', put: '/transfer/settings/continuous-policy' }
  ,monitor: { get: '/monitor/settings/runtime-policy', put: '/monitor/settings/runtime-policy' }
  ,service: { get: '/service/settings/runtime-policy', put: '/service/settings/runtime-policy' }
}

export function getPolicyConfiguration(owner) { return client.get(POLICY_CONFIGS[owner].get) }
export function updatePolicyConfiguration(owner, payload) { return client.put(POLICY_CONFIGS[owner].put, payload) }
