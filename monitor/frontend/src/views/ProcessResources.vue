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
        <el-input v-model="moduleName" :placeholder="t('monitor.process.moduleFilter')" :aria-label="t('monitor.process.moduleFilter')" clearable />
        <el-select v-model="role" :placeholder="t('monitor.process.role')" :aria-label="t('monitor.process.role')" clearable>
          <el-option v-for="item in processRoles" :key="item" :value="item" :label="t(`monitor.process.roles.${item}`)" />
        </el-select>
        <el-select v-model="status" :aria-label="t('monitor.process.online')">
          <el-option value="all" :label="t('monitor.process.allStatuses')" />
          <el-option v-for="item in ['up', 'down']" :key="item" :value="item" :label="t(`monitor.process.status.${item}`)" />
        </el-select>
        <div class="collection-filter">
          <el-select v-model="declared" :aria-label="t('monitor.process.collectionConfig')" aria-describedby="collection-config-hint">
            <el-option v-for="item in ['true', 'false', 'all']" :key="item" :value="item" :label="t(`monitor.process.declarations.${item}`)" />
          </el-select>
          <p id="collection-config-hint" class="process-hint">{{ t('monitor.process.declarationHint') }}</p>
        </div>
        <el-button native-type="submit">{{ t('common.search') }}</el-button>
        <el-button @click="setScope('all')">{{ t('monitor.process.allInstances') }}</el-button>
        <el-button v-if="state.status !== 'up' || state.declared !== 'true'" @click="setScope('current')">{{ t('monitor.process.currentInstances') }}</el-button>
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
        <el-table-column :label="t('monitor.process.host')" min-width="160" show-overflow-tooltip>
          <template #default="{ row }">
            <span data-testid="process-host" :title="row.node_id || undefined">
              <el-button v-if="canReadHost && Object.hasOwn(hostReferences, row.node_id)" text type="primary" :title="`${t('monitor.resources.view')} · ${row.node_id}`" @click="openHost(row)">{{ hostReferences[row.node_id].display_name }}</el-button>
              <span v-else>{{ row.node_id || t('monitor.process.unbound') }}</span>
            </span>
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
            <AutoRefreshControl :model-value="state.refresh" :status="refreshStatus" test-id="process-trend-refresh" @update:model-value="selectRefresh" />
            <el-button :disabled="loading" @click="reload">{{ t('common.refresh') }}</el-button>
          </div>
          <p class="process-hint">{{ t('monitor.process.trend.scope') }}</p>
          <el-alert v-if="trendErrorKey" :title="t(trendErrorKey)" type="warning" :closable="false" data-testid="process-trend-error" />
          <template v-if="trend">
            <p class="process-hint">{{ t('monitor.resources.window', { start: date(trend.start), end: date(trend.end), step: trend.step_seconds }) }}</p>
            <p class="process-hint">{{ t('monitor.resources.gaps') }}</p>
            <section v-for="chart in trendCharts" :key="chart.metric.key">
              <h3>{{ t(`monitor.process.metrics.${chart.metric.name}`) }}</h3>
              <p class="process-hint">{{ t(`monitor.process.hints.${chart.metric.name}`) }}</p>
              <div class="process-trend-chart" role="img" :aria-label="t(`monitor.process.metrics.${chart.metric.name}`)" :data-testid="`process-trend-${chart.metric.name}`"><ChartRenderer :rows="chart.rows" :config="chart.config" @invalid="invalidTrend" /></div>
            </section>
          </template>
        </div>
      </el-dialog>
    </template>
  </section>
</template>

<script setup>
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
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
import { getRegisteredEndpoint } from '../../../../common-frontend/basic/src/utils/moduleRuntimeEndpoint.mjs'
import { processMetrics, processRoles, processRoleLabelKey, resolveProcessRoute, validateProcessInstances, validateProcessSummaries, processTrendParameters, validateProcessTrend } from '../utils/processResources'
import { currentResourceValue, resourceErrorKey, resourceRanges, resourceChartRows } from '../utils/nodeResources'
import { navigateMonitorRoute } from '../utils/moduleNavigation'

const { t, locale } = useI18n(), auth = useAuthStore(), route = useRoute(), router = useRouter()
const canRead = computed(() => auth.contextType === 'platform' && auth.authContext?.principal?.type === 'user' && !auth.authContext?.delegation && auth.hasPermission('monitor.resource_observation.read') && auth.hasPermission('platform.module.read'))
const canReadHost = computed(() => canRead.value && auth.hasPermission('platform.host_node.read'))
const identity = computed(() => JSON.stringify([auth.authContext?.principal, auth.authContext?.context, auth.authContext?.delegation, auth.permissions]))
const state = computed(() => resolveProcessRoute(route.query))
const instances = ref([]), summaries = ref(new Map()), total = ref(0), loading = ref(false), errorKey = ref(''), summaryErrorKey = ref('')
const hostReferences = ref({})
const trend = ref(null), trendErrorKey = ref('')
const pageVisible = ref(!document.hidden), refreshBlocked = ref(false)
const refreshStatus = computed(() => state.value.refresh === 'off' ? 'manual' : refreshBlocked.value ? 'stopped' : !pageVisible.value ? 'paused' : 'automatic')
let disposed = false
const selectedInstance = computed(() => instances.value.find(row => row.id === state.value.instanceID))
const date = value => formatFieldPresentationValue(value, { temporal_format: 'datetime' }, locale.value)
const trendCharts = computed(() => !trend.value ? [] : processMetrics.slice(0, 2).map(metric => {
  const series = trend.value.series.find(item => item.metric_key === metric.key)
  const scale = metric.unit === 'bytes' ? scaleByteValue(Math.max(0, ...series.points.map(point => currentResourceValue(point) ?? 0))) : null
  return { metric, rows: resourceChartRows(series).map(row => ({ ...row, value: row.value === null ? null : row.value / (scale?.divisor || 1) })), config: { chart_type: 'line', dimension: 'evaluated_at', measures: ['value'], field_presentations: [{ field: 'evaluated_at', label: t('monitor.resources.evaluatedAt'), temporal_format: 'datetime' }, { field: 'value', label: t(`monitor.process.metrics.${metric.name}`), unit: scale?.unit || t('monitor.resources.units.cores'), precision: 2 }] } }
}))
function invalidTrend() { trend.value = null; trendErrorKey.value = 'monitor.resources.errors.invalidResponse' }
const moduleName = ref(''), role = ref(''), status = ref('up'), declared = ref('true'), requests = createLatestRequestScope()
const point = (row, metric) => summaries.value.get(row.id)?.series.find(item => item.metric_key === metric.key)?.points[0]
function value(row, metric) {
  const numeric = currentResourceValue(point(row, metric))
  if (numeric === null) return '—'
  if (metric.unit === 'bytes') return formatBytes(numeric, 2, locale.value)
  if (metric.unit === 'seconds') return formatDurationSeconds(numeric, locale.value)
  return t('monitor.process.cores', { value: new Intl.NumberFormat(locale.value, { minimumFractionDigits: 2, maximumFractionDigits: 2 }).format(numeric) })
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
const navigate = query => navigateMonitorRoute(router, { path: '/service-resources', query }, { history: 'replace' })
function openHost(row) {
  if (canReadHost.value && Object.hasOwn(hostReferences.value, row.node_id)) return navigateMonitorRoute(router, { path: `/node-resources/${row.node_id}` }, { history: 'push' })
}
async function loadHosts(rows, ticket) {
  if (!canReadHost.value) return
  const current = () => requests.current(ticket) && canReadHost.value
  const value = await loadHostReferences(rows.map(row => row.node_id), id => requests.request(config => hostsAPI.node(id, config)), current)
  if (current()) hostReferences.value = value
}
function selectRefresh(refresh) { return navigate(resolveProcessRoute({ ...state.value.query, refresh }).query) }
function selectTrend(instanceID, range = '5m') { return navigate(resolveProcessRoute({ ...state.value.query, instance_id: String(instanceID), range }).query) }
function closeTrend() { if (!state.value.instanceID) return; const { instance_id, range, ...query } = state.value.query; return navigate(query) }
function filter() { return navigate(resolveProcessRoute({ refresh: state.value.refresh, module_name: moduleName.value.trim(), role: role.value, status: status.value, process_metrics_declared: declared.value }).query) }
function setScope(scope) { return navigate(resolveProcessRoute({ refresh: state.value.refresh, module_name: moduleName.value.trim(), role: role.value, status: scope === 'all' ? 'all' : 'up', process_metrics_declared: scope === 'all' ? 'all' : 'true' }).query) }
function changePage(page) { if (loading.value || page === state.value.page) return; const { instance_id, range, ...query } = state.value.query; return navigate({ ...query, page: page === 1 ? undefined : String(page) }) }
function processErrorKey(error) {
 const key = resourceErrorKey(error)
 return key.endsWith('.denied') ? 'monitor.process.denied' : key.endsWith('.notFound') ? 'monitor.process.notFound' : key
}
const refreshCycle = createAutoRefresh({
  interval: () => state.value.refresh,
  enabled: () => canRead.value && route.name === 'ProcessResources' && !state.value.changed && !refreshBlocked.value && !loading.value,
  refresh: () => reload({ automatic: true }),
  pause: () => { requests.invalidate(); loading.value = false },
  visibilityChanged: visible => { pageVisible.value = visible }
})
async function reload({ automatic = false } = {}) {
  if (disposed || route.name !== 'ProcessResources') { refreshCycle.stop(); requests.invalidate(); return }
  if (automatic && (loading.value || !canRead.value || !pageVisible.value || refreshBlocked.value)) return
  refreshCycle.stop()
  const ticket = requests.invalidate()
  if (!automatic) { instances.value = []; summaries.value = new Map(); hostReferences.value = {}; total.value = 0; trend.value = null; refreshBlocked.value = false }
  errorKey.value = ''; summaryErrorKey.value = ''; trendErrorKey.value = ''; loading.value = false
  if (!canRead.value) return
  if (state.value.changed) { await navigate(state.value.query); return }
  if (!automatic) { moduleName.value = state.value.module; role.value = state.value.role; status.value = state.value.status; declared.value = state.value.declared }
  loading.value = true
  try {
    const list = await requests.request(config => api.instances(state.value.params, config))
    if (!requests.current(ticket)) return
    validateProcessInstances(list, state.value.page, state.value.params)
    if (!automatic) { instances.value = list.data; total.value = list.total }
    let currentSummaries = new Map(), currentTrend = null
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
          const params = processTrendParameters(owner.id, state.value.range, observed.queried_at)
          const history = await requests.request(config => api.trend(params, config))
          if (!requests.current(ticket)) return
          currentTrend = validateProcessTrend(history, owner, params)
        } catch (error) {
          if (!requests.current(ticket)) return
          trendErrorKey.value = processErrorKey(error)
          if ([401, 403, 404].includes(error.response?.status)) throw error
        }
      }
    }
    instances.value = list.data; total.value = list.total; summaries.value = currentSummaries; trend.value = currentTrend
    hostReferences.value = {}; void loadHosts(list.data, ticket)
  } catch (error) {
    if (!requests.current(ticket)) return
    errorKey.value = processErrorKey(error)
    if ([401, 403, 404].includes(error.response?.status)) refreshBlocked.value = true
    trend.value = null; trendErrorKey.value = errorKey.value
    instances.value = []; summaries.value = new Map(); hostReferences.value = {}; total.value = 0
  } finally { if (requests.current(ticket)) { loading.value = false; refreshCycle.schedule() } }
}
watch([() => route.fullPath, identity], reload, { immediate: true, flush: 'sync' })
onBeforeUnmount(() => { disposed = true; refreshCycle.dispose(); requests.dispose() })
</script>

<style scoped>
.process-resources { padding: 24px; color: var(--addp-text-primary); min-width: 0; }
.process-toolbar, .process-filters { display: flex; align-items: center; flex-wrap: wrap; gap: 12px; margin-bottom: 16px; }
.process-toolbar h2 { flex: 1; }
.process-filters .el-input, .process-filters .el-select { width: 200px; max-width: 100%; }
.collection-filter { max-width: 100%; }
.process-hint { margin: 4px 0; font-size: 12px; line-height: 1.5; color: var(--addp-text-secondary); }
.el-pagination { margin-top: 16px; }
.process-trend-chart { display: flex; min-width: 0; }
.process-trend-subject { overflow-wrap: anywhere; color: var(--addp-text-primary); }
</style>
