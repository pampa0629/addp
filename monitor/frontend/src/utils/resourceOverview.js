import { autoRefreshOptions } from '../../../../common-frontend/basic/src/utils/autoRefresh.mjs'
import { summaryMetrics } from './nodeResources.js'

export function resolveOverviewRoute(query = {}) {
  const refresh = autoRefreshOptions.includes(query.refresh) ? query.refresh : '15'
  const canonical = refresh === '15' ? {} : { refresh }
  return { refresh, query: canonical, changed: JSON.stringify(query) !== JSON.stringify(canonical) }
}

// Domain validation of a complete server aggregate, never browser-side ranking.
export function validateResourceOverview(value) {
  const fail = () => { throw new Error('invalid_resource_response') }
  const count = number => Number.isSafeInteger(number) && number >= 0
  const evaluated = Date.parse(value?.evaluated_at), completed = Date.parse(value?.queried_at)
  if (value?.scope !== 'current_authorized_host_nodes' || !Number.isFinite(evaluated) || evaluated % 1000 !== 0 || !Number.isFinite(completed) || completed < evaluated || value.lookback_seconds !== 300 || !count(value.policy_version)) fail()
  const inventory = value.inventory, states = ['collecting', 'failed', 'stale', 'no_sample', 'not_connected']
  if (!inventory || !['managed', 'enabled', 'disabled', 'connected', 'not_connected'].every(key => count(inventory[key])) || inventory.managed > 50 || inventory.enabled + inventory.disabled !== inventory.managed || inventory.connected + inventory.not_connected !== inventory.managed || inventory.connected > inventory.enabled || !inventory.collection || Object.keys(inventory.collection).length !== states.length || !states.every(key => count(inventory.collection[key])) || states.reduce((sum, key) => sum + inventory.collection[key], 0) !== inventory.managed || inventory.collection.not_connected !== inventory.not_connected) fail()
  if (!Array.isArray(value.rankings) || value.rankings.length !== summaryMetrics.length) fail()
  const known = new Map()
  for (let i = 0; i < summaryMetrics.length; i++) {
    const metric = summaryMetrics[i], rank = value.rankings[i], coverage = rank?.coverage
    if (rank?.metric_key !== metric.key || rank.unit !== metric.unit || rank.window_seconds !== (metric.windowSeconds || 0) || !coverage || !['expected', 'valid', 'no_data', 'stale', 'not_connected'].every(key => count(coverage[key])) || coverage.expected !== inventory.managed || coverage.valid + coverage.no_data + coverage.stale + coverage.not_connected !== coverage.expected || coverage.not_connected !== inventory.not_connected || coverage.complete !== (coverage.valid === coverage.expected) || !Array.isArray(rank.top) || rank.top.length !== Math.min(5, coverage.valid)) fail()
    const seen = new Set()
    for (let j = 0; j < rank.top.length; j++) {
      const row = rank.top[j], node = row?.node, sampled = Date.parse(row?.sampled_at), previous = rank.top[j - 1]
      if (!node || !/^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(node.node_id) || seen.has(node.node_id) || typeof node.display_name !== 'string' || !node.display_name.trim() || [...node.display_name].length > 255 || /[\u0000-\u001f\u007f]/.test(node.display_name) || !['physical', 'virtual'].includes(node.node_kind) || !count(node.version) || node.version < 1 || node.enabled !== true || typeof row.value !== 'number' || !Number.isFinite(row.value) || row.value < 0 || row.value > 100 || !Number.isFinite(sampled) || sampled > evaluated || completed - sampled > 60000 || (previous && (previous.value < row.value || (previous.value === row.value && previous.node.node_id >= node.node_id)))) fail()
      if (known.has(node.node_id) && ['node_id', 'display_name', 'node_kind', 'version', 'enabled'].some(key => known.get(node.node_id)[key] !== node[key])) fail()
      known.set(node.node_id, node); seen.add(node.node_id)
    }
  }
  return value
}
