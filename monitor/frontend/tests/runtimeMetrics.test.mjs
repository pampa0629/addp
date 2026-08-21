import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

test('dashboard loads grouped execution runtime metrics through the single monitor route', () => {
  const apiSource = readFileSync(new URL('../src/api/monitor.js', import.meta.url), 'utf8')
  const dashboardSource = readFileSync(new URL('../src/views/Dashboard.vue', import.meta.url), 'utf8')

  assert.match(apiSource, /client\.get\('\/monitor\/executions\/runtime-metrics'/)
  assert.match(dashboardSource, /getExecutionRuntimeMetrics\(\{ duration: runtimeMetricsDuration\.value \}\)/)
  assert.match(dashboardSource, /row\.pending_count/)
  assert.match(dashboardSource, /row\.p95_queue_duration_ms/)
  assert.match(dashboardSource, /row\.p95_execution_duration_ms/)
  assert.match(dashboardSource, /row\.automatic_retry_rate/)
  assert.match(dashboardSource, /row\.user_retry_rate/)
  assert.match(dashboardSource, /row\.recovery_rate/)
})

test('runtime metric refresh is slower than execution summary polling', () => {
  const dashboardSource = readFileSync(new URL('../src/views/Dashboard.vue', import.meta.url), 'utf8')

  assert.match(dashboardSource, /setInterval\(refreshExecutionSummary, 5000\)/)
  assert.match(dashboardSource, /setInterval\(\(\) => loadRuntimeMetrics\(\{ silent: true \}\), 30000\)/)
})
