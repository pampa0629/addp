<template>
  <section class="engine-resources">
    <header class="toolbar">
      <h2>{{ t('monitor.engines.title') }}</h2>
      <AutoRefreshControl v-if="canRead" :model-value="state.refresh" :status="refreshStatus" test-id="engine-refresh" @update:model-value="refresh => navigate({ ...state.query, refresh })" />
      <el-button :disabled="!canRead || loading" @click="reload()">{{ t('common.refresh') }}</el-button>
    </header>
    <el-alert v-if="!canRead" :title="t('monitor.engines.denied')" type="warning" :closable="false" data-testid="engine-denied" />
    <template v-else>
      <el-alert v-if="errorKey" :title="t(errorKey)" type="warning" :closable="false" data-testid="engine-error" />
      <template v-if="!state.deploymentID">
        <form class="toolbar" @submit.prevent="filterInput.submit">
          <el-input v-model="search" :placeholder="t('monitor.engines.search')" :aria-label="t('monitor.engines.search')" clearable @input="filterInput.queue" @clear="filterInput.submit" />
        </form>
        <el-table :data="owners" v-loading="loading" :empty-text="t('monitor.engines.empty')" data-testid="engine-list">
          <el-table-column prop="display_name" :label="t('monitor.engines.name')" min-width="190" show-overflow-tooltip />
          <el-table-column :label="t('monitor.resources.associatedHost')" min-width="180" show-overflow-tooltip><template #default="{ row }"><MonitoredHostLink data-testid="engine-host" :node-id="row.node_id" :host="hostReferences[row.node_id]" :allowed="canReadHost" :empty-label="t('monitor.engines.noCurrentHost')" /></template></el-table-column>
          <el-table-column :label="t('monitor.engines.collection')" min-width="180"><template #default="{ row }">{{ collection(row.deployment_id) }}</template></el-table-column>
          <el-table-column v-for="metric in engineSummaryMetrics" :key="metric.key" min-width="170">
            <template #header>{{ t(`monitor.engines.metrics.${metric.name}`) }}</template>
            <template #default="{ row }">{{ value(observation(row.deployment_id)?.series.find(item => item.metric_key === metric.key)?.points[0], metric) }}</template>
          </el-table-column>
          <el-table-column :label="t('monitor.engines.details')" width="130"><template #default="{ row }"><el-button text type="primary" :disabled="loading" @click="navigate({ ...state.query, deployment_id: row.deployment_id }, 'push')">{{ t('monitor.engines.details') }}</el-button></template></el-table-column>
        </el-table>
        <el-pagination v-if="!loading && !errorKey" :current-page="state.page" :page-size="20" :total="total" layout="total, prev, pager, next" @current-change="page => navigate({ ...state.query, page: String(page) })" />
      </template>
      <template v-else>
        <div class="toolbar"><el-button @click="back">{{ t('monitor.engines.back') }}</el-button><h3>{{ owners[0]?.display_name }}</h3><span>{{ collection(state.deploymentID) }}</span></div>
        <div v-if="owners.length" class="toolbar"><span>{{ t('monitor.resources.associatedHost') }}:</span><MonitoredHostLink data-testid="engine-host" :node-id="owners[0].node_id" :host="hostReferences[owners[0].node_id]" :allowed="canReadHost" :empty-label="t('monitor.engines.noCurrentHost')" /></div>
        <div v-loading="loading" class="metric-grid" data-testid="engine-metrics">
          <article v-for="metric in engineMetrics" :key="metric.key" class="metric-card">
            <h3>{{ t(`monitor.engines.metrics.${metric.name}`) }}</h3>
            <strong :data-testid="`engine-value-${metric.name}`">{{ value(observation(state.deploymentID)?.series.find(item => item.metric_key === metric.key)?.points[0], metric) }}</strong>
            <p class="hint">{{ t(`monitor.engines.hints.${metric.name}`) }}</p>
            <p v-if="observation(state.deploymentID)?.series.find(item => item.metric_key === metric.key)?.points[0]?.data_state !== 'valid'" class="hint">{{ t('monitor.engines.missing') }}</p>
            <el-button text type="primary" :disabled="loading" @click="navigate({ ...state.query, metric: metric.key })">{{ t('monitor.engines.viewTrend') }}</el-button>
          </article>
        </div>
        <section class="trend" data-testid="engine-trend">
          <div class="toolbar">
            <h3>{{ t('monitor.engines.trend') }}</h3>
            <el-select :model-value="state.metric" :aria-label="t('monitor.engines.metric')" @update:model-value="metric => navigate({ ...state.query, metric })"><el-option v-for="metric in engineMetrics" :key="metric.key" :value="metric.key" :label="t(`monitor.engines.metrics.${metric.name}`)" /></el-select>
            <el-select :model-value="state.range" :aria-label="t('monitor.resources.range')" @update:model-value="range => navigate({ ...state.query, range })"><el-option v-for="(_, range) in resourceRanges" :key="range" :value="range" :label="t(`monitor.resources.ranges.${range}`)" /></el-select>
          </div>
          <p class="hint">{{ t('monitor.engines.trendScope') }}</p>
          <p class="hint">{{ t(`monitor.engines.hints.${selectedMetric.name}`) }}</p>
          <el-alert v-if="trendError" :title="t(trendError)" type="warning" :closable="false" />
          <template v-if="chart">
            <p class="hint">{{ t('monitor.resources.window', { start: date(history.start), end: date(history.end), step: history.step_seconds }) }}</p>
            <div class="chart" role="img" :aria-label="t(`monitor.engines.metrics.${selectedMetric.name}`)"><ChartRenderer :rows="chart.rows" :config="chart.config" @invalid="invalidChart" /></div>
          </template>
        </section>
      </template>
    </template>
  </section>
</template>

<script setup>
import { computed, ref, watch, onBeforeUnmount } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import AutoRefreshControl from '../../../../common-frontend/basic/src/components/AutoRefreshControl.vue'
import ChartRenderer from '../../../../common-frontend/chart/src/ChartRenderer.vue'
import { createAutoRefresh } from '../../../../common-frontend/basic/src/utils/autoRefresh.mjs'
import { createAutoFilter } from '../../../../common-frontend/basic/src/utils/autoFilter.mjs'
import { createLatestRequestScope } from '../../../../common-frontend/basic/src/utils/latestRequestScope.mjs'
import { formatBytes, scaleByteValue } from '../../../../common-frontend/basic/src/utils/formatters.js'
import { formatFieldPresentationValue } from '../../../../common-frontend/basic/src/utils/fieldPresentation.mjs'
import { useAuthStore } from '../store/auth'
import { engineResourcesAPI as api } from '../api/engineResources'
import { nodeResourcesAPI as hostsAPI } from '../api/nodeResources'
import { loadHostReferences } from '../utils/hostReferences'
import MonitoredHostLink from '../components/MonitoredHostLink.vue'
import { engineMetrics, engineSummaryMetrics, resolveEngineRoute, validateEnginePage, validateEngineDeployment, validateEngineResources, engineTrendParameters } from '../utils/engineResources'
import { currentResourceValue, resourceErrorKey, resourceChartRows, resourceRanges } from '../utils/nodeResources'
import { navigateMonitorRoute } from '../utils/moduleNavigation'

const auth = useAuthStore(), route = useRoute(), router = useRouter(), { t, locale } = useI18n()
const canRead = computed(() => auth.contextType === 'platform' && auth.authContext?.principal?.type === 'user' && !auth.authContext?.delegation && auth.hasPermission('monitor.resource_observation.read') && auth.hasPermission('platform.engine_deployment.read'))
const canReadHost = computed(() => canRead.value && auth.hasPermission('platform.host_node.read'))
const hostReferences = ref({})
const identity = computed(() => JSON.stringify([auth.authContext?.principal, auth.authContext?.context, auth.authContext?.delegation, auth.permissions]))
const state = computed(() => resolveEngineRoute(route.query)), owners = ref([]), total = ref(0), observed = ref(null), history = ref(null)
const loading = ref(false), errorKey = ref(''), trendError = ref(''), search = ref(''), blocked = ref(false), visible = ref(!document.hidden)
const requests = createLatestRequestScope()
let disposed = false
const refreshStatus = computed(() => state.value.refresh === 'off' ? 'manual' : blocked.value ? 'stopped' : !visible.value ? 'paused' : 'automatic')
const selectedMetric = computed(() => engineMetrics.find(item => item.key === state.value.metric))
const observation = id => observed.value?.data.find(row => row.subject.deployment_id === id)
const collection = id => t(`monitor.engines.states.${observation(id)?.collection.state || 'no_sample'}`)
const date = value => formatFieldPresentationValue(value, { temporal_format: 'datetime' }, locale.value)
function value(point, metric) {
  const number = currentResourceValue(point)
  if (number === null) return '—'
  return metric.unit === 'bytes' ? formatBytes(number, null, locale.value) : formatFieldPresentationValue(number, { precision: metric.windowSeconds ? 2 : 0, unit: t(`monitor.engines.units.${metric.unit}`) }, locale.value)
}
const chart = computed(() => {
  const series = history.value?.data[0]?.series[0]
  if (!series) return null
  const metric = selectedMetric.value
  const scale = metric.unit === 'bytes' ? scaleByteValue(Math.max(0, ...series.points.map(point => currentResourceValue(point) ?? 0))) : null
  return { rows: resourceChartRows(series).map(row => ({ ...row, value: row.value === null ? null : row.value / (scale?.divisor || 1) })), config: { chart_type: 'line', dimension: 'evaluated_at', measures: ['value'], field_presentations: [{ field: 'evaluated_at', label: t('monitor.resources.evaluatedAt'), temporal_format: 'datetime' }, { field: 'value', label: t(`monitor.engines.metrics.${metric.name}`), unit: scale?.unit || t(`monitor.engines.units.${metric.unit}`), precision: metric.windowSeconds ? 2 : 0 }] } }
})
const navigate = (query, history = 'replace') => {
  filterInput.cancel()
  const target = { path: '/engine-resources', query: resolveEngineRoute(query).query }
  if (router.resolve(target).fullPath === route.fullPath) { refreshCycle.schedule(); return }
  return navigateMonitorRoute(router, target, { history })
}
const filterInput = createAutoFilter({ apply: () => navigate({ search: search.value.trim(), refresh: state.value.refresh }), enabled: () => !disposed && route.name === 'EngineResources' && !state.value.deploymentID && canRead.value, visible: () => visible.value, pause: () => refreshCycle.stop() })
function back() { const { deployment_id, metric, range, ...query } = state.value.query; return navigate(query) }
function invalidChart() { history.value = null; trendError.value = 'monitor.resources.errors.invalidResponse' }
const refreshCycle = createAutoRefresh({ interval: () => state.value.refresh, enabled: () => canRead.value && route.name === 'EngineResources' && !blocked.value && !loading.value && !state.value.changed && !filterInput.pending, refresh: () => reload({ automatic: true }), pause: () => { requests.invalidate(); loading.value = false; observed.value = null; history.value = null; hostReferences.value = {} }, visibilityChanged: value => { visible.value = value; filterInput.visibilityChanged(value) } })
async function reload({ automatic = false } = {}) {
  if (!automatic) filterInput.cancel()
  if (disposed || route.name !== 'EngineResources') return
  refreshCycle.stop()
  const ticket = requests.invalidate()
  observed.value = null; history.value = null; owners.value = []; hostReferences.value = {}; total.value = 0; errorKey.value = ''; trendError.value = ''
  if (!automatic) blocked.value = false
  if (!canRead.value || !visible.value) { loading.value = false; return }
  if (state.value.changed) { await navigate(state.value.query); return }
  if (!automatic) search.value = state.value.search
  loading.value = true
  try {
    let rows, count
    if (state.value.deploymentID) {
      const owner = validateEngineDeployment(await requests.request(config => api.deployment(state.value.deploymentID, config)))
      if (owner.deployment_id !== state.value.deploymentID) throw new Error('invalid_resource_response')
      rows = [owner]; count = 1
    } else {
      const page = validateEnginePage(await requests.request(config => api.deployments({ page: state.value.page, page_size: 20, ...(state.value.search ? { search: state.value.search } : {}) }, config)), state.value.page)
      rows = page.data; count = page.total
    }
    if (!requests.current(ticket)) return
    owners.value = rows; total.value = count
    void loadHosts(owners.value, ticket)
    if (rows.length) {
      const result = state.value.deploymentID ? await requests.request(config => api.instant({ deployment_id: state.value.deploymentID, metrics: engineMetrics.map(item => item.key).join(',') }, config)) : await requests.request(config => api.summaries(rows.map(item => item.deployment_id), config))
      if (!requests.current(ticket)) return
      observed.value = validateEngineResources(result, rows, state.value.deploymentID ? engineMetrics : engineSummaryMetrics)
      if (state.value.deploymentID) {
        try {
          const params = engineTrendParameters(state.value.deploymentID, state.value.metric, state.value.range, result.queried_at)
          const trend = await requests.request(config => api.trend(params, config))
          if (!requests.current(ticket)) return
          history.value = validateEngineResources(trend, rows, [selectedMetric.value], params)
        } catch (error) {
          if (!requests.current(ticket)) return
          trendError.value = resourceErrorKey(error)
          if ([401, 403, 404].includes(error.response?.status)) throw error
        }
      }
    }
  } catch (error) {
    if (!requests.current(ticket)) return
    errorKey.value = resourceErrorKey(error)
    if ([401, 403, 404].includes(error.response?.status)) blocked.value = true
    observed.value = null; history.value = null; owners.value = []; hostReferences.value = {}; total.value = 0
  } finally { if (requests.current(ticket)) { loading.value = false; refreshCycle.schedule() } }
}
async function loadHosts(rows, ticket) {
  if (!canReadHost.value) return
  const current = () => requests.current(ticket) && canReadHost.value && owners.value === rows && !errorKey.value
  const hosts = await loadHostReferences(rows.map(row => row.node_id), id => requests.request(config => hostsAPI.node(id, config)), current)
  if (current()) hostReferences.value = hosts
}
watch([() => route.fullPath, identity], () => reload(), { immediate: true, flush: 'sync' })
onBeforeUnmount(() => { disposed = true; filterInput.dispose(); refreshCycle.dispose(); requests.dispose() })
</script>

<style scoped>
.engine-resources { padding: 24px; min-width: 0; color: var(--addp-text-primary); }
.toolbar { display: flex; flex-wrap: wrap; align-items: center; gap: 12px; margin-bottom: 16px; }
.toolbar h2 { flex: 1; }
.toolbar .el-input, .toolbar .el-select { width: 240px; max-width: 100%; }
.metric-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(280px, 100%), 1fr)); gap: 16px; }
.metric-card { padding: 20px; border: 1px solid var(--addp-border-color); border-radius: 8px; background: var(--addp-bg-secondary); min-width: 0; }
.metric-card h3 { font-size: 16px; margin: 0 0 12px; }
.metric-card strong { font-size: 24px; overflow-wrap: anywhere; }
.hint { margin: 8px 0; color: var(--addp-text-secondary); line-height: 1.5; font-size: 13px; }
.trend, .el-pagination { margin-top: 20px; }
.chart { display: flex; min-width: 0; }
</style>
