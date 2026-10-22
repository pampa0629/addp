import client from './client'

export function listMetricCategories() {
  return client.get('/standard/metric-categories')
}

export function getMeasurementUnit(id) {
  return client.get(`/standard/units/${encodeURIComponent(id)}`)
}
