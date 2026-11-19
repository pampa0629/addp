<template>
  <article class="runtime-card" :aria-busy="loading" :data-testid="`engine-runtime-${source}`">
    <h3>{{ t(`monitor.engineRuntime.${source}`) }}</h3>
    <el-alert v-if="errorKey" :title="t(errorKey)" type="warning" :closable="false" />
    <template v-else-if="current">
      <p class="runtime-state" data-testid="engine-runtime-state">
        {{ t(stateKey) }}<template v-if="current.collection.sampled_at"> · {{ date(current.collection.sampled_at) }}</template>
      </p>
      <dl v-if="current.id" class="runtime-metrics">
        <div v-for="metric in metrics" :key="metric">
          <dt>
            {{ t(`monitor.engineRuntime.metrics.${metric}`) }}
            <FieldHelp v-if="metric === 'tasks_retained_failed' || metric === 'effective_cpu_cores' || metric === 'memory_limit_bytes'" :content="t(`monitor.engineRuntime.hints.${metric}`)" />
          </dt>
          <dd :data-testid="`engine-runtime-value-${metric}`">{{ display(metric) }}</dd>
        </div>
      </dl>
      <el-button v-if="current.id" text type="primary" @click="toggleTrend">
        {{ t(showTrend ? 'monitor.engineRuntime.hideTrend' : 'monitor.engines.viewTrend') }}
      </el-button>
    </template>
    <section v-if="showTrend && current?.id" class="runtime-trend" :aria-busy="trendLoading">
      <div class="runtime-controls">
        <el-select v-model="selectedMetric" :aria-label="t('monitor.engines.metric')">
          <el-option v-for="metric in metrics" :key="metric" :value="metric" :label="t(`monitor.engineRuntime.metrics.${metric}`)" />
        </el-select>
        <el-select v-model="range" :aria-label="t('monitor.resources.range')">
          <el-option v-for="(_, key) in resourceRanges" :key="key" :value="key" :label="t(`monitor.resources.ranges.${key}`)" />
        </el-select>
      </div>
      <p class="runtime-state">{{ t('monitor.engineRuntime.trendHint') }}</p>
      <el-alert v-if="trendError" :title="t(trendError)" type="warning" :closable="false" />
      <div v-else-if="chart" data-testid="engine-runtime-chart">
        <ChartRenderer :rows="chart.rows" :config="chart.config" @invalid="invalidTrend" />
      </div>
    </section>
  </article>
</template>

<script setup>
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import FieldHelp from '../../../../common-frontend/basic/src/components/FieldHelp.vue'
import ChartRenderer from '../../../../common-frontend/chart/src/ChartRenderer.vue'
import { createLatestRequestScope } from '../../../../common-frontend/basic/src/utils/latestRequestScope.mjs'
import { formatBytes, formatDurationSeconds, scaleByteValue } from '../../../../common-frontend/basic/src/utils/formatters.js'
import { formatFieldPresentationValue } from '../../../../common-frontend/basic/src/utils/fieldPresentation.mjs'
import { engineResourcesAPI as api } from '../api/engineResources'
import { currentResourceValue, resourceChartRows, resourceErrorKey, resourceRanges } from '../utils/nodeResources'
import { isTargetUUID } from '../utils/monitoringTargets'

const props = defineProps({ source: { type: String, required: true } })
const emit = defineEmits(['access-lost'])
const { t, locale } = useI18n()
const units = props.source === 'meilisearch'
  ? { tasks_pending: 'tasks', tasks_oldest_wait_seconds: 'seconds', tasks_retained_failed: 'tasks' }
  : { effective_cpu_cores: 'cores', memory_limit_bytes: 'bytes', raster_running: 'tasks', raster_waiting: 'tasks', raster_admission_enabled: 'status' }
const metrics = Object.keys(units)
const current = ref(null), history = ref(null), loading = ref(false), trendLoading = ref(false)
const errorKey = ref(''), trendError = ref(''), showTrend = ref(false), selectedMetric = ref(metrics[0]), range = ref('5m')
const requests = createLatestRequestScope(), trendRequests = createLatestRequestScope()
const date = value => formatFieldPresentationValue(value, { temporal_format: 'datetime' }, locale.value)
const stateKey = computed(() => current.value.discovery_state === 'unavailable'
  ? 'monitor.engineRuntime.discoveryUnavailable'
  : !current.value.id ? 'monitor.engineRuntime.empty'
    : current.value.collection.state === 'snapshot_unavailable' ? 'monitor.engineRuntime.snapshotUnavailable'
      : `monitor.engines.states.${current.value.collection.state}`)
const label = metric => t(`monitor.engineRuntime.metrics.${metric}`)

function display(metric) {
  const value = currentResourceValue(current.value.series.find(series => series.metric_key === metric)?.points[0])
  if (value === null) return '—'
  if (units[metric] === 'status') return t(`monitor.engineRuntime.admission.${value}`)
  if (units[metric] === 'seconds') return formatDurationSeconds(value, locale.value)
  if (units[metric] === 'bytes') return formatBytes(value, null, locale.value)
  return formatFieldPresentationValue(value, { precision: units[metric] === 'cores' ? 2 : 0, unit: t(`monitor.engineRuntime.units.${units[metric]}`) }, locale.value)
}

const chart = computed(() => {
  const series = history.value?.series[0]
  if (!series) return null
  const unit = series.unit
  const scale = unit === 'bytes' ? scaleByteValue(Math.max(0, ...series.points.map(point => currentResourceValue(point) ?? 0))) : null
  return {
    rows: resourceChartRows(series).map(row => ({ ...row, value: row.value === null ? null : row.value / (scale?.divisor || 1) })),
    config: {
      chart_type: 'line', dimension: 'evaluated_at', measures: ['value'],
      field_presentations: [
        { field: 'evaluated_at', label: t('monitor.resources.evaluatedAt'), temporal_format: 'datetime' },
        { field: 'value', label: label(series.metric_key), unit: scale?.unit || t(`monitor.engineRuntime.units.${unit}`), precision: unit === 'cores' ? 2 : 0 }
      ]
    }
  }
})

function checkResponse(value, trend) {
  const keys = trend ? [selectedMetric.value] : metrics
  const invalid = () => { throw new Error('invalid_resource_response') }
  if (value?.source !== props.source || !['available', 'unavailable'].includes(value.discovery_state) ||
      !Number.isFinite(Date.parse(value.queried_at)) || !Array.isArray(value.series) || !value.collection) invalid()
  if (!value.id) {
    if (trend || value.series.length) invalid()
    return value
  }
  if (!isTargetUUID(value.id) || !Number.isFinite(Date.parse(value.started_at)) ||
      (trend && (value.id !== current.value.id || value.started_at !== current.value.started_at)) || value.series.length !== keys.length) invalid()
  for (const key of keys) {
    const series = value.series.find(item => item.metric_key === key)
    if (series?.unit !== units[key] || series.window_seconds !== 0 || !Array.isArray(series.points) ||
        !series.points.length || series.points.length > (trend ? 1000 : 1)) invalid()
  }
  return value
}

function handleAccess(error) {
  if ([401, 403, 404].includes(error.response?.status)) {
    current.value = null
    history.value = null
    emit('access-lost')
  }
}

function closeTrend() {
  trendRequests.invalidate()
  showTrend.value = false
  history.value = null
  trendLoading.value = false
  trendError.value = ''
}
function toggleTrend() {
  if (showTrend.value) closeTrend()
  else showTrend.value = true
}
function invalidTrend() {
  history.value = null
  trendError.value = 'monitor.resources.errors.invalidResponse'
}

async function loadTrend() {
  const ticket = trendRequests.invalidate()
  const row = current.value
  if (!showTrend.value || !row?.id) return
  const end = Math.floor(Date.parse(row.queried_at) / 1000) * 1000
  const start = end - resourceRanges[range.value] * 1000
  trendError.value = ''
  trendLoading.value = true
  try {
    const result = await trendRequests.request(config => api.runtime({
      source: props.source, id: row.id, metric: selectedMetric.value,
      start: new Date(start).toISOString(), end: new Date(end).toISOString()
    }, config))
    if (trendRequests.current(ticket)) history.value = checkResponse(result, true)
  } catch (error) {
    if (!trendRequests.current(ticket)) return
    history.value = null
    trendError.value = resourceErrorKey(error)
    handleAccess(error)
  } finally {
    if (trendRequests.current(ticket)) trendLoading.value = false
  }
}

async function reload() {
  const ticket = requests.invalidate()
  errorKey.value = ''
  loading.value = true
  try {
    const result = await requests.request(config => api.runtime({ source: props.source }, config))
    if (!requests.current(ticket)) return
    const value = checkResponse(result, false)
    if (current.value?.id !== value.id || current.value?.started_at !== value.started_at) closeTrend()
    current.value = value
    if (showTrend.value) await loadTrend()
  } catch (error) {
    if (!requests.current(ticket)) return
    current.value = null
    closeTrend()
    errorKey.value = resourceErrorKey(error)
    handleAccess(error)
  } finally {
    if (requests.current(ticket)) loading.value = false
  }
}

watch([showTrend, selectedMetric, range], () => {
  history.value = null
  if (showTrend.value) void loadTrend()
})
defineExpose({ reload })
onBeforeUnmount(() => { requests.dispose(); trendRequests.dispose() })
</script>

<style scoped>
.runtime-card { padding: 20px; border: 1px solid var(--addp-border-color); border-radius: 8px; background: var(--addp-bg-secondary); min-width: 0; }
.runtime-card h3 { margin: 0; font-size: 18px; }
.runtime-state, .runtime-metrics dt { color: var(--addp-text-secondary); font-size: 13px; }
.runtime-metrics { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 16px 12px; margin: 16px 0; }
.runtime-metrics dt { display: flex; align-items: center; gap: 4px; }
.runtime-metrics dd { margin: 6px 0 0; font-size: 20px; overflow-wrap: anywhere; }
.runtime-controls { display: flex; gap: 12px; flex-wrap: wrap; }
.runtime-controls .el-select { width: 220px; max-width: 100%; }
.runtime-trend { margin-top: 12px; }
</style>
