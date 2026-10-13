export function overviewResponse(stamp = '2026-10-13T00:00:00Z') {
  const top = Array.from({ length: 5 }, (_, i) => ({ node: { node_id: `00000000-0000-4000-8000-${String(41 - i).padStart(12, '0')}`, display_name: `完整范围主机${41 - i}`, node_kind: 'virtual', version: 3, enabled: true }, value: [88, 70, 55, 10, 0][i], sampled_at: stamp }))
  return { scope: 'current_authorized_host_nodes', evaluated_at: stamp, queried_at: stamp, policy_version: 2, lookback_seconds: 300,
    inventory: { managed: 43, enabled: 41, disabled: 2, connected: 40, not_connected: 3, collection: { collecting: 30, failed: 2, stale: 3, no_sample: 5, not_connected: 3 } },
    rankings: ['node.cpu.busy_percent', 'node.memory.used_percent'].map((metric_key, i) => ({ metric_key, unit: 'percent', window_seconds: i === 0 ? 60 : 0, coverage: { expected: 43, valid: 5, no_data: 32, stale: 3, not_connected: 3, complete: false }, top: structuredClone(top) })) }
}
