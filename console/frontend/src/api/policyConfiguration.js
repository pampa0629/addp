import client from './client'

export const POLICY_CONFIGS = {
  develop: { get: '/develop/settings/query-policy', put: '/develop/settings/query-policy' },
  manager: { get: '/manager/settings/quick-view-policy', put: '/manager/settings/quick-view-policy' },
  copilot: { get: '/copilot/settings/matching-policy', put: '/copilot/settings/matching-policy' }
}

export function getPolicyConfiguration(owner) { return client.get(POLICY_CONFIGS[owner].get) }
export function updatePolicyConfiguration(owner, payload) { return client.put(POLICY_CONFIGS[owner].put, payload) }
