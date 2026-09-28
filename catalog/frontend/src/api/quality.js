import client from './client'

// Quality owns these facts. Read with the current User Token, never Catalog's runtime identity.
export function listDomainQualityRules(params) {
  return client.get('/quality/rules', { params })
}

export function listDomainQualityPlans(params) {
  return client.get('/quality/plans', { params })
}

export function listDomainQualityIssues(params) {
  return client.get('/quality/issues', { params })
}
