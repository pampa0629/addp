const STATUS_REQUESTS = [
  { key: 'engines', permission: 'system.engine.read', url: '/system/engines?page_size=1' },
  { key: 'datasets', permission: 'meta.catalog.read', url: '/meta/stats' },
  { key: 'services', permission: 'service.definition.read', url: '/service/query?page_size=1' },
  { key: 'tasks', permission: 'monitor.execution.read', url: '/monitor/executions?status=running&page_size=1' }
]

export async function fetchPortalStatus(client, permissions = []) {
  const granted = new Set(permissions)
  const status = Object.fromEntries(STATUS_REQUESTS.map(({ key }) => [key, null]))
  const requests = STATUS_REQUESTS.filter(({ permission }) => granted.has(permission))

  await Promise.all(requests.map(async ({ key, url }) => {
    try {
      const response = await client.get(url)
      status[key] = response?.data?.total ?? response?.total ?? 0
    } catch {
      status[key] = null
    }
  }))

  return status
}
