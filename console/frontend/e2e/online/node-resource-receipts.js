import { resourceMetrics } from '../../../../monitor/frontend/src/utils/nodeResources.js'

// Resource families arrive concurrently. Request identity, not arrival order, owns the receipt.
export function scalarResourceReceipts(receipts, nodeId) {
  const keys = resourceMetrics.map(metric => metric.key)
  return receipts.filter(row => {
    const query = row?.query
    if (row?.path !== '/api/v1/monitor/platform/resource_observations' || query?.node_id !== nodeId ||
        Object.keys(query).sort().join(',') !== 'metrics,node_id' || typeof query.metrics !== 'string') return false
    const metrics = query.metrics.split(',')
    return metrics.length === keys.length && new Set(metrics).size === keys.length && keys.every(key => metrics.includes(key))
  })
}
