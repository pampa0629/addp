import client from './client'

export function getIAMSecurityPolicy() {
  return client.get('/system/platform/security_policy')
}

export function updateIAMSecurityPolicy(payload) {
  return client.put('/system/platform/security_policy', payload)
}
