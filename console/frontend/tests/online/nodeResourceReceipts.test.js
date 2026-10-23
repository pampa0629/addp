import { describe, expect, it } from 'vitest'
import { scalarResourceReceipts } from '../../e2e/online/node-resource-receipts.js'
import { resourceMetrics, filesystemMetrics, networkMetrics } from '../../../../monitor/frontend/src/utils/nodeResources.js'

const node = '10000000-0000-4000-8000-000000000001'
const receipt = (metrics = resourceMetrics, query = {}) => ({ path: '/api/v1/monitor/platform/resource_observations',
  query: { node_id: node, metrics: metrics.map(row => row.key).join(','), ...query }, value: { receipt: 'unchanged-body' } })

describe('Online scalar resource receipt identity', () => {
  it.each(['network-last', 'scalar-last', 'filesystem-last'])('keeps ten scalar metrics with %s completion order', order => {
    const scalar = receipt(), network = receipt(networkMetrics), filesystem = receipt(filesystemMetrics)
    const rows = order === 'network-last' ? [scalar, filesystem, network]
      : order === 'scalar-last' ? [network, filesystem, scalar] : [network, scalar, filesystem]
    expect(scalarResourceReceipts(rows, node)).toEqual([scalar])
    expect(scalarResourceReceipts(rows, node)[0]).toBe(scalar)
    expect(rows).toHaveLength(3)
  })
  it('accepts the same complete set in a different order and preserves separate refresh receipts', () => {
    const first = receipt([...resourceMetrics].reverse()), second = receipt()
    expect(scalarResourceReceipts([first, receipt(networkMetrics), second], node)).toEqual([first, second])
  })
  it('rejects incomplete, duplicate or expanded metric collections', () => {
    const rows = [receipt(resourceMetrics.slice(1)), receipt([...resourceMetrics, resourceMetrics[0]]),
      receipt([...resourceMetrics.slice(1), resourceMetrics[1]]), receipt([...resourceMetrics, ...networkMetrics])]
    expect(scalarResourceReceipts(rows, node)).toEqual([])
  })
  it('requires the exact ten current scalars including TCP, rejecting the old nine-key request', () => {
    expect(resourceMetrics).toHaveLength(10)
    expect(resourceMetrics.find(metric => metric.key === 'node.tcp.current_connections')?.unit).toBe('connections')
    expect(scalarResourceReceipts([receipt(resourceMetrics.filter(metric => metric.key !== 'node.tcp.current_connections'))], node)).toEqual([])
    expect(scalarResourceReceipts([receipt()], node)).toHaveLength(1)
  })
  it('does not borrow another host, trend, endpoint or query scope', () => {
    const rows = [receipt(resourceMetrics, { node_id: 'another-host' }), receipt(resourceMetrics, { end: '2026-10-19T00:00:00Z' }),
      { ...receipt(), path: '/api/v1/monitor/platform/resource_trends' },
      { ...receipt(), path: '/another/resource_observations' }]
    expect(scalarResourceReceipts(rows, node)).toEqual([])
  })
  it('missing or malformed request evidence cannot become a scalar receipt', () => {
    expect(scalarResourceReceipts([{}, { ...receipt(), query: {} }, receipt(resourceMetrics, { metrics: null })], node)).toEqual([])
  })
})
