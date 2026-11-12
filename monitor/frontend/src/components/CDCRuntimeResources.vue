<template>
  <section class="cdc-resources" :aria-busy="loading">
    <header class="toolbar">
      <div class="page-title"><h2>{{ t('monitor.cdc.resourcesTitle') }}</h2><p class="hint">{{ t('monitor.cdc.resourcesSubtitle') }}</p></div>
      <AutoRefreshControl v-if="canRead" :model-value="state.refresh" :status="refreshStatus" test-id="cdc-refresh" @update:model-value="refresh => navigate({ ...state.query, refresh })" />
      <el-button :loading="loading" :disabled="!canRead" @click="reload()">{{ t('common.refresh') }}</el-button>
    </header>
    <el-alert v-if="!canRead" :title="t('monitor.cdc.denied')" type="warning" :closable="false" />
    <template v-else>
      <el-alert v-if="error" :title="t(error)" type="warning" :closable="false" data-testid="cdc-error" />
      <el-alert v-else-if="observed?.discovery_state === 'unavailable'" :title="t('monitor.cdc.discoveryUnavailable')" type="warning" :closable="false" />
      <div class="toolbar">
        <el-button v-if="auth.hasPermission('platform.module.read')" link type="primary" data-testid="cdc-open-workers" @click="navigateMonitorRoute(router, { path: '/service-resources', query: { module_name: 'transfer' } }, { history: 'push' })">{{ t('monitor.cdc.openWorkers') }}</el-button>
        <el-button v-if="auth.hasPermission('platform.engine_deployment.read')" link type="primary" @click="navigateMonitorRoute(router, { path: '/engine-resources' }, { history: 'push' })">{{ t('monitor.cdc.openEngines') }}</el-button>
      </div>
      <div class="cdc-grid" v-loading="loading && !observed">
        <el-card v-for="kind in stages" :key="kind" shadow="never" :data-testid="`cdc-card-${kind}`">
          <template #header><div class="card-heading"><h3>{{ t(`monitor.cdc.stages.${kind}`) }}</h3></div><p class="hint">{{ t(`monitor.cdc.roles.${kind}`) }}</p></template>
          <p class="collection" :data-testid="`cdc-state-${kind}`">{{ collection(kind) }}<span v-if="row(kind)?.collection.sampled_at"> · {{ date(row(kind).collection.sampled_at) }}</span></p>
          <dl class="metrics">
            <div v-for="metric in cdcMetrics[kind].slice(0, 3)" :key="metric.key">
              <dt><span>{{ metricLabel(kind, metric) }}</span><FieldHelp v-if="metric.name === 'sourceDelay' || metric.name === 'pendingRecords'" :content="hint(kind, metric)" /></dt>
              <dd :data-testid="`cdc-value-${metric.name}`">{{ display(kind, metric) }}</dd>
            </div>
          </dl>
          <p v-if="kind === 'kafka-connect' && number(kind, 'connectors') === 0" class="hint" data-testid="cdc-empty-connectors">{{ t('monitor.cdc.emptyConnectors') }}</p>
          <p v-if="kind === 'kafka-connect' && number(kind, 'sourceDelay') === null && row(kind)?.collection.state === 'collecting'" class="hint">{{ t('monitor.cdc.noDelay') }}</p>
          <p v-if="kind === 'redpanda' && number(kind, 'pendingRecords') === null && row(kind)?.collection.state === 'collecting'" class="hint" data-testid="cdc-no-lag">{{ t('monitor.cdc.noLag') }}</p>
          <details>
            <summary>{{ t('monitor.process.moreMetrics') }}</summary>
            <dl class="metrics secondary"><div v-for="metric in cdcMetrics[kind].slice(3)" :key="metric.key"><dt><span>{{ metricLabel(kind, metric) }}</span><FieldHelp v-if="metric.name === 'offsetFailure' || metric.name === 'pendingRecords'" :content="hint(kind, metric)" /></dt><dd>{{ display(kind, metric) }}</dd></div></dl>
          </details>
          <el-button link type="primary" :disabled="!row(kind)" @click="navigate({ ...state.query, stage: kind, metric: undefined }, 'push')">{{ t('monitor.engines.viewTrend') }}</el-button>
        </el-card>
      </div>
      <section class="trend-panel" data-testid="cdc-trend-panel">
        <div class="toolbar">
          <h3>{{ t('monitor.cdc.trend') }}</h3>
          <el-select :model-value="state.stage" :aria-label="t('monitor.cdc.stage')" @update:model-value="stage => navigate({ ...state.query, stage, metric: undefined })"><el-option v-for="kind in stages" :key="kind" :value="kind" :label="t(`monitor.cdc.stages.${kind}`)" /></el-select>
          <el-select :model-value="state.metric.key" :aria-label="t('monitor.http.metric')" @update:model-value="metric => navigate({ ...state.query, metric })"><el-option v-for="metric in cdcMetrics[state.stage]" :key="metric.key" :value="metric.key" :label="metricLabel(state.stage, metric)" /></el-select>
          <el-select :model-value="state.range" :aria-label="t('monitor.http.range')" @update:model-value="range => navigate({ ...state.query, range })"><el-option v-for="range in Object.keys(resourceRanges)" :key="range" :value="range" :label="t(`monitor.resources.ranges.${range}`)" /></el-select>
        </div>
        <el-alert v-if="trendError" :title="t(trendError)" type="warning" :closable="false" />
        <p v-else-if="!history" class="hint">{{ t('monitor.cdc.noTrend') }}</p>
        <template v-if="chart"><p class="hint">{{ t('monitor.resources.window', { start: date(history.start), end: date(history.end), step: history.step_seconds }) }}</p><ChartRenderer :rows="chart.rows" :config="chart.config" zoomable data-testid="cdc-trend" @invalid="history = null; trendError = 'monitor.resources.errors.invalidResponse'" /></template>
      </section>
    </template>
  </section>
</template>

<script setup>
import { computed, ref, watch, onBeforeUnmount } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import AutoRefreshControl from '../../../../common-frontend/basic/src/components/AutoRefreshControl.vue'
import FieldHelp from '../../../../common-frontend/basic/src/components/FieldHelp.vue'
import ChartRenderer from '../../../../common-frontend/chart/src/ChartRenderer.vue'
import { createLatestRequestScope } from '../../../../common-frontend/basic/src/utils/latestRequestScope.mjs'
import { createAutoRefresh } from '../../../../common-frontend/basic/src/utils/autoRefresh.mjs'
import { formatBytes, scaleByteValue } from '../../../../common-frontend/basic/src/utils/formatters.js'
import { formatFieldPresentationValue } from '../../../../common-frontend/basic/src/utils/fieldPresentation.mjs'
import { useAuthStore } from '../store/auth'
import { cdcResourcesAPI as api } from '../api/cdcResources'
import { cdcMetrics, resolveCDCRoute, validateCDCResources } from '../utils/cdcResources'
import { currentResourceValue, resourceErrorKey, resourceChartRows, resourceRanges } from '../utils/nodeResources'
import { navigateMonitorRoute } from '../utils/moduleNavigation'

const auth = useAuthStore(), route = useRoute(), router = useRouter(), { t, locale } = useI18n()
const canRead = computed(() => auth.contextType === 'platform' && auth.authContext?.principal?.type === 'user' && !auth.authContext?.delegation && auth.hasPermission('monitor.resource_observation.read'))
const identity = computed(() => JSON.stringify([auth.authContext, auth.permissions]))
const state = computed(() => resolveCDCRoute(route.query)), stages = ['kafka-connect', 'redpanda']
const observed = ref(null), history = ref(null)
const loading = ref(false), error = ref(''), trendError = ref(''), blocked = ref(false), visible = ref(!document.hidden)
const requests = createLatestRequestScope()
let context = ''
const row = kind => observed.value?.data.find(item => item.subject.component_type === kind)
const date = value => formatFieldPresentationValue(value, { temporal_format: 'datetime' }, locale.value)
const metricLabel = (kind, metric) => t(`monitor.cdc.metrics.${metric.name}`)
const hint = (kind, metric) => t(`monitor.cdc.hints.${metric.name}`)
const number = (kind, name) => currentResourceValue(row(kind)?.series.find(series => series.metric_key === cdcMetrics[kind].find(metric => metric.name === name)?.key)?.points[0])
function display(kind, metric) {
  const value = number(kind, metric.name)
  if (value === null) return '—'
  return metric.unit === 'bytes' ? formatBytes(value, null, locale.value) : formatFieldPresentationValue(value, { precision: metric.unit === 'seconds' ? 3 : metric.unit === 'percent' ? 2 : 0, unit: metric.unit === 'percent' ? '%' : metric.unit === 'seconds' ? 's' : '' }, locale.value)
}
function collection(kind) {
  const observedRow = row(kind)
  if (observedRow) return t(observedRow.collection.state === 'snapshot_unavailable' ? 'monitor.cdc.snapshotUnavailable' : `monitor.engines.states.${observedRow.collection.state}`)
  return t(observed.value?.discovery_state === 'available' && !error.value ? 'monitor.cdc.notDeployed' : 'monitor.cdc.unknown')
}
const refreshStatus = computed(() => state.value.refresh === 'off' ? 'manual' : blocked.value ? 'stopped' : !visible.value ? 'paused' : 'automatic')
const navigate = (query, history = 'replace') => navigateMonitorRoute(router, { path: '/cdc-resources', query: resolveCDCRoute(query).query }, { history })
const chart = computed(() => {
  const series = history.value?.data[0]?.series[0]
  if (!series) return null
  const scale = series.unit === 'bytes' ? scaleByteValue(Math.max(0, ...series.points.map(point => currentResourceValue(point) ?? 0))) : null
  return { rows: resourceChartRows(series).map(row => ({ ...row, value: row.value === null ? null : row.value / (scale?.divisor || 1) })), config: { chart_type: 'line', dimension: 'evaluated_at', measures: ['value'], field_presentations: [{ field: 'evaluated_at', label: t('monitor.resources.evaluatedAt'), temporal_format: 'datetime' }, { field: 'value', label: metricLabel(state.value.stage, state.value.metric), unit: scale?.unit || (series.unit === 'seconds' ? 's' : series.unit === 'percent' ? '%' : ''), precision: series.unit === 'seconds' ? 3 : series.unit === 'percent' ? 2 : 0 }] } }
})
const cycle = createAutoRefresh({ interval: () => state.value.refresh, enabled: () => canRead.value && route.name === 'CDCResources' && !blocked.value && !loading.value, refresh: () => reload(true), pause: () => { requests.invalidate(); loading.value = false }, visibilityChanged: value => { visible.value = value } })
function failure(cause) { if ([401, 403, 404].includes(cause.response?.status)) blocked.value = true; return resourceErrorKey(cause) }
async function reload(automatic = false) {
  cycle.stop()
  const ticket = requests.invalidate()
  if (!automatic) blocked.value = false
  const next = JSON.stringify([identity.value, { ...state.value.query, refresh: undefined }])
  if (next !== context) { context = next; observed.value = null; history.value = null }
  error.value = ''; trendError.value = ''; loading.value = false
  if (!canRead.value || route.name !== 'CDCResources') return
  if (state.value.changed) { await navigate(state.value.query); return }
  loading.value = true
  try {
    try {
      const data = validateCDCResources(await requests.request(config => api.instant(config)))
      if (!requests.current(ticket)) return
      if (JSON.stringify(data.data.map(item => item.subject)) !== JSON.stringify(observed.value?.data.map(item => item.subject))) history.value = null
      observed.value = data
    } catch (cause) { if (!requests.current(ticket)) return; observed.value = null; history.value = null; error.value = failure(cause) }
    if (blocked.value) return
    const selected = row(state.value.stage)
    if (selected) {
      try {
        const end = Math.floor(Date.parse(observed.value.queried_at) / 1000) * 1000
        const params = { component_id: selected.subject.component_id, metrics: state.value.metric.key, start: new Date(end - resourceRanges[state.value.range] * 1000).toISOString(), end: new Date(end).toISOString() }
        const data = validateCDCResources(await requests.request(config => api.trend(params, config)), params, selected.subject)
        if (requests.current(ticket)) history.value = data
      } catch (cause) { if (!requests.current(ticket)) return; history.value = null; trendError.value = failure(cause) }
    } else history.value = null
  } finally { if (requests.current(ticket)) { loading.value = false; cycle.schedule() } }
}
watch([() => route.fullPath, identity], () => reload(), { immediate: true, flush: 'sync' })
onBeforeUnmount(() => { cycle.dispose(); requests.dispose() })
</script>

<style scoped>
.cdc-resources { padding: 24px; min-width: 0; color: var(--addp-text-primary); }
.toolbar, .card-heading { display: flex; align-items: center; flex-wrap: wrap; gap: 12px; }
.toolbar { margin-bottom: 16px; }
.page-title { flex: 1; }
h2, h3 { margin: 0; }
.hint { color: var(--addp-text-secondary); font-size: 13px; line-height: 1.6; margin: 8px 0; }
.cdc-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(100%, 320px), 1fr)); gap: 16px; align-items: start; margin: 16px 0 24px; }
.collection { font-size: 12px; color: var(--addp-text-secondary); overflow-wrap: anywhere; }
.metrics { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 16px 12px; margin: 16px 0; }
.metrics dt { display: flex; align-items: center; gap: 4px; color: var(--addp-text-secondary); font-size: 12px; }
.metrics dd { margin: 6px 0 0; font-size: 21px; font-weight: 600; overflow-wrap: anywhere; }
.secondary dd { font-size: 17px; }
summary { cursor: pointer; color: var(--addp-text-secondary); margin: 12px 0; }
.trend-panel { border: 1px solid var(--addp-border-color); border-radius: 8px; padding: 20px; min-width: 0; }
.trend-panel h3 { margin-right: auto; }
.trend-panel .el-select { width: 210px; max-width: 100%; }
@media (max-width: 600px) { .cdc-resources { padding: 16px; } .page-title { flex-basis: 100%; } }
</style>
