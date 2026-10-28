import { describe, expect, it } from 'vitest'
import { scalarResourceReceipts, filesystemMountReceipt } from '../../e2e/online/node-resource-receipts.js'
import { resourceMetrics, filesystemMetrics, inodeMetrics, mountMetrics, networkMetrics } from '../../../../monitor/frontend/src/utils/nodeResources.js'

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

const mountedReceipt = (catalog, dimensions, overrides = {}) => ({ ...receipt(catalog), value: {
  series: catalog.map(metric => ({ metric_key: metric.key, dimensions: { ...dimensions },
    points: [{ data_state: 'valid', value: overrides[metric.key] ?? (metric.unit === 'bytes' ? 2 ** 30 : metric.unit === 'percent' ? 12.345 : 1234567) }] }))
} })
const mount = { device: '/dev/mapper/data', mountpoint: '/data with spaces', fstype: 'ext4' }

describe('Online real filesystem mount receipt', () => {
  it('binds concurrent families, all three dimensions and all nine shared-formatted readings', () => {
    const capacity = mountedReceipt(filesystemMetrics, mount), inodes = mountedReceipt(inodeMetrics, mount)
    const rows = [inodes, receipt(), capacity, receipt(networkMetrics)]
    const result = filesystemMountReceipt(rows, node)
    expect(result).toMatchObject({ capacity_index: 2, inode_index: 0, row_index: 0, dimensions: mount })
    expect(Object.keys(result.metrics)).toEqual(mountMetrics.map(metric => metric.key))
    expect(result.metrics['node.filesystem.total_bytes']).toBe('1.00 GiB')
    expect(result.metrics['node.filesystem.used_percent']).toBe('12.35 %')
    expect(result.metrics['node.filesystem.inodes_total']).toBe('1,234,567 个')
  })
  it('does not merge the same device or path across different mount identities', () => {
    const wrong = mountedReceipt(inodeMetrics, { ...mount, fstype: 'xfs' })
    expect(() => filesystemMountReceipt([mountedReceipt(filesystemMetrics, mount), wrong], node)).toThrow('no complete valid readings')
    wrong.value.series.forEach(series => { series.dimensions = { ...mount, mountpoint: '/another' } })
    expect(() => filesystemMountReceipt([mountedReceipt(filesystemMetrics, mount), wrong], node)).toThrow('no complete valid readings')
  })
  it('keeps zero as a valid reading and selects the complete row after an unsupported mount', () => {
    const missing = { device: 'tmpfs', mountpoint: '/unsupported', fstype: 'tmpfs' }
    const capacity = mountedReceipt(filesystemMetrics, missing), inodes = mountedReceipt(inodeMetrics, missing)
    for (const source of [capacity, inodes]) source.value.series.forEach(series => { series.points[0] = { data_state: 'no_data', value: null } })
    capacity.value.series.push(...mountedReceipt(filesystemMetrics, mount, { 'node.filesystem.available_bytes': 0 }).value.series)
    inodes.value.series.push(...mountedReceipt(inodeMetrics, mount, { 'node.filesystem.inodes_free': 0 }).value.series)
    const result = filesystemMountReceipt([capacity, inodes], node)
    expect(result.row_index).toBe(1)
    expect(result.dimensions).toEqual(mount)
    expect(result.metrics['node.filesystem.available_bytes']).toBe('0.00 B')
    expect(result.metrics['node.filesystem.inodes_free']).toBe('0 个')
  })
  it.each(['no_data', 'stale', 'not_connected'])('rejects %s instead of borrowing an older successful refresh', state => {
    const first = mountedReceipt(filesystemMetrics, mount), following = structuredClone(first)
    following.value.series[0].points[0] = { data_state: state, value: state === 'stale' ? 12 : null }
    expect(() => filesystemMountReceipt([first, mountedReceipt(inodeMetrics, mount), following], node)).toThrow('no complete valid readings')
  })
  it('rejects missing or differently scoped families', () => {
    const capacity = mountedReceipt(filesystemMetrics, mount), inodes = mountedReceipt(inodeMetrics, mount)
    for (const invalid of [{ ...inodes, query: { ...inodes.query, node_id: 'other' } },
      { ...inodes, path: '/api/v1/monitor/platform/resource_trends' },
      { ...inodes, query: { ...inodes.query, end: 'extra-scope' } },
      { ...inodes, query: { ...inodes.query, metrics: inodeMetrics.slice(1).map(row => row.key).join(',') } }]) {
      expect(() => filesystemMountReceipt([capacity, invalid], node)).toThrow('receipts missing')
    }
  })
})
