<template>
  <section class="process-resources">
    <div class="process-toolbar">
      <h2>{{ t('monitor.process.title') }}</h2>
      <AutoRefreshControl v-if="canRead" :model-value="state.refresh" :status="refreshStatus" test-id="process-refresh" @update:model-value="selectRefresh" />
      <el-button :disabled="!canRead || loading" @click="reload">{{ t('common.refresh') }}</el-button>
    </div>
    <el-alert v-if="!canRead" :title="t('monitor.process.denied')" type="warning" :closable="false" />
    <template v-else>
      <form class="process-filters" @submit.prevent="filter">
        <el-input v-model="moduleName" :placeholder="t('monitor.process.moduleFilter')" :aria-label="t('monitor.process.moduleFilter')" clearable @input="queueFilter" @clear="filter" />
        <div class="endpoint-filter">
          <el-input v-model="registeredHost" :placeholder="t('monitor.process.addressFilter')" :aria-label="t('monitor.process.addressFilter')" maxlength="255" clearable @input="queueFilter" @clear="filter" />
          <FieldHelp :content="t('monitor.process.addressHint')" />
        </div>
        <div class="host-name-filter">
          <el-input v-model="nodeName" :placeholder="t('monitor.process.hostNameFilter')" :aria-label="t('monitor.process.hostNameFilter')" clearable @input="queueFilter" @clear="filter" />
          <FieldHelp :content="t('monitor.process.hostNameHint')" />
        </div>
        <div class="host-ip-filter">
          <el-input v-model="nodeIP" :placeholder="t('monitor.process.hostIPFilter')" :aria-label="t('monitor.process.hostIPFilter')" clearable @input="queueFilter" @clear="filter" />
          <FieldHelp :content="t('monitor.process.hostIPHint')" />
        </div>
        <el-select v-model="role" :placeholder="t('monitor.process.role')" :aria-label="t('monitor.process.role')" clearable @change="filter">
          <el-option v-for="item in processRoles" :key="item" :value="item" :label="t(`monitor.process.roles.${item}`)" />
        </el-select>
        <el-select v-model="status" :aria-label="t('monitor.process.online')" @change="filter">
          <el-option value="all" :label="t('monitor.process.allStatuses')" />
          <el-option v-for="item in ['up', 'down']" :key="item" :value="item" :label="t(`monitor.process.status.${item}`)" />
        </el-select>
        <RuntimeInstanceStopReasonSelect v-model="stopReason" @change="filter" />
        <div class="collection-filter">
          <el-select v-model="declared" :aria-label="t('monitor.process.collectionConfig')" @change="filter">
            <el-option v-for="item in ['true', 'false', 'all']" :key="item" :value="item" :label="t(`monitor.process.declarations.${item}`)" />
          </el-select>
          <FieldHelp :content="t('monitor.process.declarationHint')" />
        </div>
        <RuntimeInstanceTimeFilter v-model:time-basis="timeBasis" v-model:period="period" v-model:custom-range="customRange" @change="filter" />
        <el-button @click="resetFilters">{{ t('common.reset') }}</el-button>
        <el-button :disabled="!validTimeDraft" @click="setScope('all')">{{ t('monitor.process.allInstances') }}</el-button>
        <el-button :disabled="!validTimeDraft" v-if="state.status !== 'up' || state.declared !== 'true'" @click="setScope('current')">{{ t('monitor.process.currentInstances') }}</el-button>
      </form>
      <el-alert v-if="errorKey || summaryErrorKey" :title="t(errorKey || summaryErrorKey)" type="warning" :closable="false" data-testid="process-error" />
      <el-table :data="instances" v-loading="loading" :empty-text="t('monitor.process.empty')" data-testid="process-list">
        <el-table-column :label="t('monitor.process.module')" prop="module_name" min-width="120" />
        <el-table-column :label="t('monitor.process.instance')" min-width="170" show-overflow-tooltip>
          <template #default="{ row }">{{ row.runtime_hostname || row.registered_host || row.instance_id }}<p class="process-hint" :title="row.instance_id">{{ t(processRoleLabelKey(row)) }}</p></template>
        </el-table-column>
        <el-table-column :label="t('monitor.process.endpoint')" min-width="210" show-overflow-tooltip><template #default="{ row }"><span data-testid="process-endpoint">{{ getRegisteredEndpoint(row) || t('monitor.process.noEndpoint') }}</span></template></el-table-column>
        <el-table-column :label="t('monitor.process.online')" min-width="140"><template #default="{ row }"><el-tag data-testid="process-online" size="small" :type="row.status === 'up' ? 'success' : 'danger'">{{ row.status.toUpperCase() }} · {{ t(`monitor.process.status.${row.status}`) }}</el-tag></template></el-table-column>
        <el-table-column :label="t('monitor.process.collection')" min-width="175"><template #default="{ row }"><span>{{ collectionState(row) }}</span><p v-if="sampledAt(row)" class="process-hint">{{ sampledAt(row) }}</p></template></el-table-column>
        <el-table-column :label="t('common.runtimeInstanceStopReason.label')" min-width="200">
          <template #default="{ row }"><span data-testid="process-stop-reason">{{ stopReasonLabel(row) }}</span><p v-if="row.status === 'down' && row.stop_reason === 'lease_expired'" class="process-hint">{{ t('common.runtimeInstanceStopReason.leaseExpiredHint') }}</p></template>
        </el-table-column>
        <el-table-column :label="t(`common.runtimeInstanceTime.${state.timeBasis}Time`)" min-width="180"><template #default="{ row }">{{ row[state.timeBasis === 'offline' ? 'stopped_at' : 'registered_at'] ? date(row[state.timeBasis === 'offline' ? 'stopped_at' : 'registered_at']) : '—' }}</template></el-table-column>
        <el-table-column :label="t('monitor.resources.associatedHost')" min-width="160" show-overflow-tooltip>
          <template #default="{ row }">
            <MonitoredHostLink data-testid="process-host" :node-id="row.node_id" :host="hostReferences[row.node_id]" :allowed="canReadHost" />
          </template>
        </el-table-column>
        <el-table-column v-for="metric in processMetrics" :key="metric.key" min-width="200">
          <template #header><span>{{ t(`monitor.process.metrics.${metric.name}`) }}</span><p class="process-hint">{{ t(`monitor.process.hints.${metric.name}`) }}</p></template>
          <template #default="{ row }"><strong>{{ value(row, metric) }}</strong><p v-if="point(row, metric)?.data_state !== 'valid'" class="process-hint">{{ pointState(row, metric) }}</p></template>
        </el-table-column>
        <el-table-column :label="t('monitor.resources.trend')" width="130"><template #default="{ row }"><el-button text type="primary" :disabled="row.status !== 'up' || !row.process_metrics_declared || loading" @click="selectTrend(row.id)">{{ t('monitor.resources.filesystem.viewTrend') }}</el-button></template></el-table-column>
      </el-table>
      <el-pagination v-if="!loading && !errorKey" :current-page="state.page" :page-size="20" :total="total" layout="total, prev, pager, next" @current-change="changePage" />
      <el-dialog :model-value="Boolean(state.instanceID)" :title="t('monitor.process.trend.title')" width="min(960px, calc(100vw - 32px))" destroy-on-close @close="closeTrend">
        <div v-loading="loading" data-testid="process-trend">
          <p v-if="selectedInstance" class="process-trend-subject">{{ selectedInstance.module_name }} · {{ t(processRoleLabelKey(selectedInstance)) }} · {{ selectedInstance.instance_id }}</p>
          <div class="process-toolbar">
            <el-select :model-value="state.range" :aria-label="t('monitor.resources.range')" @update:model-value="range => selectTrend(state.instanceID, range)"><el-option v-for="(_, key) in resourceRanges" :key="key" :value="key" :label="t(`monitor.resources.ranges.${key}`)" /></el-select>
            <el-checkbox :model-value="state.compare" :disabled="!supportsProcessComparison(state.range)" data-testid="process-compare" @change="selectComparison">{{ t('monitor.resources.comparison.enable') }}</el-checkbox>
            <AutoRefreshControl :model-value="state.refresh" :status="refreshStatus" test-id="process-trend-refresh" @update:model-value="selectRefresh" />
            <el-button :disabled="loading" @click="reload">{{ t('common.refresh') }}</el-button>
          </div>
          <p v-if="state.range === '7d'" class="process-hint" data-testid="process-compare-retention">{{ t('monitor.resources.comparison.retention') }}</p>
          <p class="process-hint">{{ t('monitor.process.trend.scope') }}</p>
          <ContainerResources :value="container" :error-key="containerErrorKey" />
          <el-alert v-if="trendErrorKey" :title="t(trendErrorKey)" type="warning" :closable="false" data-testid="process-trend-error" />
          <template v-if="trend">
            <p class="process-hint">{{ t('monitor.resources.window', { start: date(comparisons?.[processMetrics[0].key]?.current.start || trend.start), end: date(trend.end), step: trend.step_seconds }) }}</p>
            <p class="process-hint">{{ t('monitor.resources.gaps') }}</p>
            <section v-for="chart in trendCharts" :key="chart.metric.key">
              <h3>{{ t(`monitor.process.metrics.${chart.metric.name}`) }}</h3>
              <p class="process-hint">{{ t(`monitor.process.hints.${chart.metric.name}`) }}</p>
              <ResourcePeriodComparison v-if="chart.comparison" :comparison="chart.comparison" :title="t(`monitor.process.comparison.${chart.metric.name}`)" :format-value="numeric => comparisonValue(numeric, chart.metric)" :change="comparisonChange(chart.comparison, chart.metric)" :prefix="`process-compare-${chart.metric.name}`" :test-id="`process-comparison-${chart.metric.name}`">
                <p v-if="chart.metric.windowSeconds" class="process-hint">{{ t('monitor.process.comparison.cpuWindow') }}</p>
                <p v-if="!chart.comparison.complete" class="process-hint">{{ t('monitor.process.comparison.launch') }}</p>
              </ResourcePeriodComparison>
              <p class="process-hint" data-testid="process-zoom-hint">{{ t('monitor.process.trend.zoomHint') }}</p>
              <div class="process-trend-chart" role="img" :aria-label="t(`monitor.process.metrics.${chart.metric.name}`)" :data-testid="`process-trend-${chart.metric.name}`"><ChartRenderer :rows="chart.rows" :config="chart.config" zoomable :initial-zoom="trendZoom[chart.metric.key] || null" @zoom-change="trendZoom[chart.metric.key] = $event" @invalid="invalidTrend" /></div>
            </section>
          </template>
        </div>
      </el-dialog>
    </template>
  </section>
</template>

<script setup>
import ContainerResources from '../components/ContainerResources.vue'
import { validateContainerObservation } from '../utils/containerResources'
import ResourcePeriodComparison from '../components/ResourcePeriodComparison.vue'
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import RuntimeInstanceStopReasonSelect from '../../../../common-frontend/basic/src/components/RuntimeInstanceStopReasonSelect.vue'
import { runtimeInstanceStopReasonLabelKey } from '../../../../common-frontend/basic/src/utils/runtimeInstanceStopReason.mjs'
import RuntimeInstanceTimeFilter from '../../../../common-frontend/basic/src/components/RuntimeInstanceTimeFilter.vue'
import { validRuntimeInstanceRange } from '../../../../common-frontend/basic/src/utils/runtimeInstanceTime.mjs'
import FieldHelp from '../../../../common-frontend/basic/src/components/FieldHelp.vue'
import { debounce } from '../../../../common-frontend/basic/src/utils/index.js'
import AutoRefreshControl from '../../../../common-frontend/basic/src/components/AutoRefreshControl.vue'
import { createAutoRefresh } from '../../../../common-frontend/basic/src/utils/autoRefresh.mjs'
import { createLatestRequestScope } from '../../../../common-frontend/basic/src/utils/latestRequestScope.mjs'
import { formatBytes, formatDurationSeconds, scaleByteValue } from '../../../../common-frontend/basic/src/utils/formatters.js'
import ChartRenderer from '../../../../common-frontend/chart/src/ChartRenderer.vue'
import { formatFieldPresentationValue } from '../../../../common-frontend/basic/src/utils/fieldPresentation.mjs'
import { useAuthStore } from '../store/auth'
import { processResourcesAPI as api } from '../api/processResources'
import { nodeResourcesAPI as hostsAPI } from '../api/nodeResources'
import { loadHostReferences } from '../utils/hostReferences'
import MonitoredHostLink from '../components/MonitoredHostLink.vue'
import { getRegisteredEndpoint } from '../../../../common-frontend/basic/src/utils/moduleRuntimeEndpoint.mjs'
import { processMetrics, processRoles, processRoleLabelKey, resolveProcessRoute, processInstanceParameters, validateProcessInstances, validateProcessSummaries, processTrendParameters, validateProcessTrend, processTrendComparisons, supportsProcessComparison } from '../utils/processResources'
import { currentResourceValue, resourceErrorKey, resourceRanges, resourceChartRows } from '../utils/nodeResources'
import { navigateMonitorRoute } from '../utils/moduleNavigation'

const { t, locale } = useI18n(), auth = useAuthStore(), route = useRoute(), router = useRouter()
const canRead = computed(() => auth.contextType === 'platform' && auth.authContext?.principal?.type === 'user' && !auth.authContext?.delegation && auth.hasPermission('monitor.resource_observation.read') && auth.hasPermission('platform.module.read'))
const canReadHost = computed(() => canRead.value && auth.hasPermission('platform.host_node.read'))
const identity = computed(() => JSON.stringify([auth.authContext?.principal, auth.authContext?.context, auth.authContext?.delegation, auth.permissions]))
const state = computed(() => resolveProcessRoute(route.query))
const instances = ref([]), summaries = ref(new Map()), total = ref(0), loading = ref(false), errorKey = ref(''), summaryErrorKey = ref('')
const hostReferences = ref({})
const stopReason = ref('')
const timeBasis = ref('registered'), period = ref('all'), customRange = ref(null)
const validTimeDraft = computed(() => period.value !== 'custom' || validRuntimeInstanceRange(customRange.value))
const comparisons = ref(null)
const trend = ref(null), trendErrorKey = ref('')
const trendZoom = ref({})
const container = ref(null), containerErrorKey = ref('')
const pageVisible = ref(!document.hidden), refreshBlocked = ref(false)
const refreshStatus = computed(() => state.value.refresh === 'off' ? 'manual' : refreshBlocked.value ? 'stopped' : !pageVisible.value ? 'paused' : 'automatic')
let disposed = false
const selectedInstance = computed(() => instances.value.find(row => row.id === state.value.instanceID))
const date = value => formatFieldPresentationValue(value, { temporal_format: 'datetime' }, locale.value)
const trendCharts = computed(() => !trend.value ? [] : processMetrics.slice(0, 2).map(metric => {
  const series = trend.value.series.find(item => item.metric_key === metric.key)
  const comparison = comparisons.value?.[metric.key]
  const points = series.points.filter(point => !comparison || Date.parse(point.evaluated_at) > Date.parse(comparison.current.start))
  const scale = metric.unit === 'bytes' ? scaleByteValue(Math.max(0, ...points.map(point => currentResourceValue(point) ?? 0))) : null
  return { metric, comparison, rows: resourceChartRows({ points }).map(row => ({ ...row, value: row.value === null ? null : row.value / (scale?.divisor || 1) })), config: { chart_type: 'line', dimension: 'evaluated_at', measures: ['value'], field_presentations: [{ field: 'evaluated_at', label: t('monitor.resources.evaluatedAt'), temporal_format: 'datetime' }, { field: 'value', label: t(`monitor.process.metrics.${metric.name}`), unit: scale?.unit || t('monitor.resources.units.cores'), precision: 2 }] } }
}))
function invalidTrend() { trend.value = null; comparisons.value = null; trendErrorKey.value = 'monitor.resources.errors.invalidResponse' }
const moduleName = ref(''), registeredHost = ref(''), nodeName = ref(''), nodeIP = ref(''), role = ref(''), status = ref('up'), declared = ref('true'), requests = createLatestRequestScope()
const point = (row, metric) => summaries.value.get(row.id)?.series.find(item => item.metric_key === metric.key)?.points[0]
function value(row, metric) {
  const numeric = currentResourceValue(point(row, metric))
  if (numeric === null) return '—'
  if (metric.unit === 'bytes') return formatBytes(numeric, 2, locale.value)
  if (metric.unit === 'seconds') return formatDurationSeconds(numeric, locale.value)
  return t('monitor.process.cores', { value: new Intl.NumberFormat(locale.value, { minimumFractionDigits: 2, maximumFractionDigits: 2 }).format(numeric) })
}
function comparisonValue(numeric, metric) {
  if (numeric === null) return '—'
  if (metric.unit === 'bytes') return formatBytes(numeric, 2, locale.value)
  return t('monitor.process.cores', { value: formatFieldPresentationValue(numeric, { precision: 2 }, locale.value) })
}
function comparisonChange(comparison, metric) {
  if (!comparison.complete) return ''
  const delta = comparison.delta
  if (delta === 0) return t('monitor.resources.comparison.unchanged')
  if (Math.round(Math.abs(delta) * 100) === 0) return t(`monitor.process.comparison.${metric.unit === 'cores' ? 'smallCPUChange' : 'smallMemoryChange'}`)
  return t(`monitor.process.comparison.${delta > 0 ? 'increased' : 'decreased'}`, { value: comparisonValue(Math.abs(delta), metric) })
}
function pointState(row, metric) {
  const data = point(row, metric)
  if (!data) return ''
  return t(data.data_state === 'no_data' && metric.name === 'cpu' && summaries.value.get(row.id)?.collection.state === 'collecting' ? 'monitor.process.waiting' : `monitor.process.states.${data.data_state}`)
}
function collectionState(row) {
  return t(summaries.value.has(row.id) ? `monitor.process.collectionStates.${summaries.value.get(row.id).collection.state}` : !row.process_metrics_declared ? 'monitor.process.collectionStates.not_connected' : 'monitor.process.unavailable')
}
function sampledAt(row) {
  const stamp = summaries.value.get(row.id)?.collection.sampled_at
  return stamp ? `${t('monitor.resources.sampledAt')}: ${formatFieldPresentationValue(stamp, { temporal_format: 'datetime' }, locale.value)}` : ''
}
const navigate = query => {
  const target = { path: '/service-resources', query }
  if (router.resolve(target).fullPath === route.fullPath) { refreshCycle.schedule(); return }
  return navigateMonitorRoute(router, target, { history: 'replace' })
}
async function loadHosts(rows, ticket) {
  if (!canReadHost.value) return
  const current = () => requests.current(ticket) && canReadHost.value
  const value = await loadHostReferences(rows.map(row => row.node_id), id => requests.request(config => hostsAPI.node(id, config)), current)
  if (current()) hostReferences.value = value
}
function selectRefresh(refresh) { return navigate(resolveProcessRoute({ ...state.value.query, refresh }).query) }
function selectTrend(instanceID, range = '5m') { return navigate(resolveProcessRoute({ ...state.value.query, compare: instanceID === state.value.instanceID ? state.value.query.compare : undefined, instance_id: String(instanceID), range }).query) }
function selectComparison(enabled) { return navigate(resolveProcessRoute({ ...state.value.query, compare: enabled ? 'previous' : undefined }).query) }
function closeTrend() { if (!state.value.instanceID) return; const { instance_id, range, compare, ...query } = state.value.query; return navigate(query) }
const pendingFilter = ref(false)
const deferredFilter = debounce(() => filter())
function cancelFilter() { deferredFilter.cancel(); pendingFilter.value = false }
function queueFilter() {
  cancelFilter()
  if (!canRead.value || !validTimeDraft.value) return
  pendingFilter.value = true; refreshCycle.stop()
  if (pageVisible.value) deferredFilter()
}
function filter() {
  cancelFilter()
  if (disposed || route.name !== 'ProcessResources' || !canRead.value) return
  if (!validTimeDraft.value) { refreshCycle.schedule(); return }
  const query = resolveProcessRoute({ ...timeDraft(), refresh: state.value.refresh, module_name: moduleName.value.trim(), registered_host: registeredHost.value, node_name: nodeName.value, node_ip: nodeIP.value, role: role.value, stop_reason: stopReason.value, status: status.value, process_metrics_declared: declared.value }).query
  return navigate(query)
}
function setScope(scope) { cancelFilter(); if (!validTimeDraft.value) return; return navigate(resolveProcessRoute({ ...timeDraft(), refresh: state.value.refresh, module_name: moduleName.value.trim(), registered_host: registeredHost.value, node_name: nodeName.value, node_ip: nodeIP.value, role: role.value, stop_reason: stopReason.value, status: scope === 'all' ? 'all' : 'up', process_metrics_declared: scope === 'all' ? 'all' : 'true' }).query) }
function stopReasonLabel(row) {
  const key = row.status === 'down' ? runtimeInstanceStopReasonLabelKey(row.stop_reason) : ''
  return key ? t(key) : '—'
}
function timeDraft() {
  return { time_basis: timeBasis.value, time_period: period.value, ...(period.value === 'custom' && validTimeDraft.value ? { time_from: new Date(customRange.value[0]).toISOString(), time_to: new Date(customRange.value[1]).toISOString() } : {}) }
}
function resetFilters() {
  cancelFilter()
  stopReason.value = ''
  timeBasis.value = 'registered'; period.value = 'all'; customRange.value = null
  moduleName.value = ''; registeredHost.value = ''; nodeName.value = ''; nodeIP.value = ''; role.value = ''; status.value = 'up'; declared.value = 'true'
  return navigate(resolveProcessRoute({ refresh: state.value.refresh }).query)
}
function changePage(page) { if (loading.value || page === state.value.page) return; const { instance_id, range, compare, ...query } = state.value.query; return navigate({ ...query, page: page === 1 ? undefined : String(page) }) }
function processErrorKey(error) {
 if (error.response?.status === 400) return 'monitor.process.invalidQuery'
 const key = resourceErrorKey(error)
 return key.endsWith('.denied') ? 'monitor.process.denied' : key.endsWith('.notFound') ? 'monitor.process.notFound' : key
}
const refreshCycle = createAutoRefresh({
  interval: () => state.value.refresh,
  enabled: () => canRead.value && route.name === 'ProcessResources' && !state.value.changed && !refreshBlocked.value && !loading.value && !pendingFilter.value,
  refresh: () => reload({ automatic: true }),
  pause: () => { requests.invalidate(); loading.value = false },
  visibilityChanged: visible => { pageVisible.value = visible; if (!visible) deferredFilter.cancel(); else if (pendingFilter.value) deferredFilter() }
})
async function reload({ automatic = false } = {}) {
  if (disposed || route.name !== 'ProcessResources') { refreshCycle.stop(); requests.invalidate(); return }
  if (automatic && (loading.value || !canRead.value || !pageVisible.value || refreshBlocked.value)) return
  refreshCycle.stop()
  const ticket = requests.invalidate()
  container.value = null; containerErrorKey.value = ''
  if (!automatic) cancelFilter()
  if (!automatic) { instances.value = []; summaries.value = new Map(); hostReferences.value = {}; total.value = 0; trend.value = null; comparisons.value = null; refreshBlocked.value = false }
  errorKey.value = ''; summaryErrorKey.value = ''; trendErrorKey.value = ''; loading.value = false
  if (!canRead.value) return
  if (state.value.changed) { await navigate(state.value.query); return }
  if (!automatic) { stopReason.value = state.value.stopReason; timeBasis.value = state.value.timeBasis; period.value = state.value.period; customRange.value = state.value.period === 'custom' ? [new Date(state.value.from), new Date(state.value.to)] : null; moduleName.value = state.value.module; registeredHost.value = state.value.registeredHost; nodeName.value = state.value.nodeName; nodeIP.value = state.value.nodeIP; role.value = state.value.role; status.value = state.value.status; declared.value = state.value.declared }
  loading.value = true
  try {
    const listParams = processInstanceParameters(state.value)
    const list = await requests.request(config => api.instances(listParams, config))
    if (!requests.current(ticket)) return
    validateProcessInstances(list, state.value.page, listParams)
    if (!automatic) { instances.value = list.data; total.value = list.total }
    let currentSummaries = new Map(), currentTrend = null, currentComparisons = null
    if (list.data.length) {
      try {
        const observed = await requests.request(config => api.summaries(list.data.map(row => row.id), config))
        if (!requests.current(ticket)) return
        currentSummaries = validateProcessSummaries(observed, list.data)
      } catch (error) {
        if (!requests.current(ticket)) return
        if ([401, 403, 404].includes(error.response?.status)) throw error
        summaryErrorKey.value = processErrorKey(error)
      }
    }
    if (state.value.instanceID) {
      const owner = list.data.find(row => row.id === state.value.instanceID), observed = currentSummaries.get(owner?.id)
      if (!owner || owner.status !== 'up' || !owner.process_metrics_declared) trendErrorKey.value = 'monitor.process.trend.reselect'
      else if (!observed) trendErrorKey.value = summaryErrorKey.value || 'monitor.resources.errors.unavailable'
      else {
        try {
          const value = await requests.request(config => api.container(owner.id, config))
          if (!requests.current(ticket)) return
          container.value = validateContainerObservation(value, owner)
        } catch (error) {
          if (!requests.current(ticket)) return
          containerErrorKey.value = processErrorKey(error)
          if ([401, 403, 404].includes(error.response?.status)) throw error
        }
        try {
          const params = processTrendParameters(owner.id, state.value.range, observed.queried_at, state.value.compare)
          const history = await requests.request(config => api.trend(params, config))
          if (!requests.current(ticket)) return
          currentTrend = validateProcessTrend(history, owner, params)
          if (state.value.compare) currentComparisons = processTrendComparisons(history, owner, params, state.value.range)
        } catch (error) {
          if (!requests.current(ticket)) return
          currentTrend = null; currentComparisons = null
          trendErrorKey.value = processErrorKey(error)
          if ([401, 403, 404].includes(error.response?.status)) throw error
        }
      }
    }
    instances.value = list.data; total.value = list.total; summaries.value = currentSummaries; trend.value = currentTrend; comparisons.value = currentComparisons
    hostReferences.value = {}; void loadHosts(list.data, ticket)
  } catch (error) {
    if (!requests.current(ticket)) return
    container.value = null
    errorKey.value = processErrorKey(error)
    if ([401, 403, 404].includes(error.response?.status)) refreshBlocked.value = true
    trend.value = null; comparisons.value = null; trendErrorKey.value = errorKey.value
    instances.value = []; summaries.value = new Map(); hostReferences.value = {}; total.value = 0
  } finally { if (requests.current(ticket)) { loading.value = false; refreshCycle.schedule() } }
}
watch([() => JSON.stringify({ ...state.value.query, refresh: undefined }), identity], () => { trendZoom.value = {} }, { flush: 'sync' })
watch([errorKey, trendErrorKey], ([error, trendError]) => { if (error || trendError) trendZoom.value = {} }, { flush: 'sync' })
watch([() => route.fullPath, identity], reload, { immediate: true, flush: 'sync' })
onBeforeUnmount(() => { disposed = true; cancelFilter(); refreshCycle.dispose(); requests.dispose() })
</script>

<style scoped>
.process-resources { padding: 24px; color: var(--addp-text-primary); min-width: 0; }
.process-toolbar, .process-filters { display: flex; align-items: center; flex-wrap: wrap; gap: 12px; margin-bottom: 16px; }
.process-toolbar .el-checkbox { height: auto; max-width: 100%; }
.process-toolbar :deep(.el-checkbox__label) { white-space: normal; }
.process-toolbar h2 { flex: 1; }
.process-filters .el-input, .process-filters .el-select { width: 200px; max-width: 100%; }
.collection-filter, .endpoint-filter, .host-name-filter, .host-ip-filter { display: flex; align-items: center; gap: 4px; max-width: 100%; }
.process-hint { margin: 4px 0; font-size: 12px; line-height: 1.5; color: var(--addp-text-secondary); }
@media (max-width: 600px) { .process-toolbar h2 { flex-basis: 100%; } .process-filters > .runtime-instance-time-filter { width: 100%; } }
.el-pagination { margin-top: 16px; }
.process-trend-chart { display: flex; min-width: 0; }
.process-trend-subject { overflow-wrap: anywhere; color: var(--addp-text-primary); }
</style>
