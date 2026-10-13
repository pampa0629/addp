import client from './client'
export const resourceOverviewAPI = { overview: config => client.get('/monitor/platform/resource_overview', config) }
