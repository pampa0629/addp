import client from './client'
export const resourceOverviewAPI = {
  overview: config => client.get('/monitor/platform/resource_overview', config),
  collectionHealth: config => client.get('/monitor/platform/metrics_collection_health', config)
}
