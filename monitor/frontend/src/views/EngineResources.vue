<template>
  <section class="engine-resources" :aria-busy="loading">
    <header class="toolbar">
      <h2>{{ t('monitor.engines.title') }}</h2>
      <AutoRefreshControl v-if="canRead" :model-value="state.refresh" :status="refreshStatus" test-id="engine-refresh" @update:model-value="refresh => navigate({ ...state.query, refresh })" />
      <el-button :disabled="!canRead" :loading="loading" @click="reload()">{{ t('common.refresh') }}</el-button>
    </header>
    <el-alert v-if="!canRead" :title="t('monitor.engines.denied')" type="warning" :closable="false" data-testid="engine-denied" />
    <template v-else>
      <el-alert v-if="errorKey" :title="t(errorKey)" type="warning" :closable="false" data-testid="engine-error" />
      <template v-if="!state.deploymentID">
        <el-tabs :model-value="state.scope" @update:model-value="scope => navigate({...state.query,scope,page:'1'})" data-testid="engine-scopes">
          <el-tab-pane v-for="scope in ['infra','business']" :key="scope" :name="scope" :label="t(`monitor.engines.scopes.${scope}`)" />
        </el-tabs>
        <form class="toolbar" @submit.prevent="filterInput.submit">
          <el-input v-model="search" :placeholder="t('monitor.engines.search')" :aria-label="t('monitor.engines.search')" clearable @input="filterInput.queue" @keydown.enter="filterInput.enter" @clear="filterInput.submit" />
          <FieldHelp :content="t('monitor.engines.scopeHint')" />
        </form>
        <div class="engine-grid" v-loading="loading && !observed" data-testid="engine-list">
          <article v-for="row in owners" :key="row.deployment_id" class="engine-card" :aria-label="row.display_name" :data-testid="`engine-card-${row.deployment_id}`">
            <header class="engine-card-heading"><h3>{{ row.display_name }}</h3><el-tag effect="plain">{{ engineNames[row.engine_type] }}</el-tag></header>
            <div class="engine-card-host"><span>{{ t('monitor.resources.associatedHost') }}:</span><MonitoredHostLink data-testid="engine-host" :node-id="row.node_id" :host="hostReferences[row.node_id]" :allowed="canReadHost" :empty-label="t('monitor.engines.noCurrentHost')" /></div>
            <p class="engine-card-state"><span>{{ t('monitor.engines.collection') }}:</span><span>{{ collection(row.deployment_id) }}</span></p>
            <dl class="engine-summary">
              <div v-for="metric in engineSummaryFor(row.engine_type)" :key="metric.key">
                <dt class="metric-heading"><span>{{ t(`monitor.engines.metrics.${metric.name}`) }}</span><FieldHelp :content="t(`monitor.engines.hints.${metric.name}`)" /></dt>
                <dd>{{ value(observation(row.deployment_id)?.series.find(item => item.metric_key === metric.key)?.points[0], metric) }}</dd>
              </div>
            </dl>
            <el-button type="primary" plain :disabled="loading" @click="navigate({ ...state.query, engine_type: row.engine_type, deployment_id: row.deployment_id }, 'push')">{{ t('monitor.engines.details') }}</el-button>
          </article>
        </div>
        <el-empty v-if="!loading && !errorKey && !owners.length" :description="t('monitor.engines.empty')" />
        <el-pagination v-if="!errorKey && (observed || !loading)" :current-page="state.page" :page-size="20" :total="total" layout="total, prev, pager, next" @current-change="page => navigate({ ...state.query, page: String(page) })" />
      </template>
      <template v-else>
        <div class="toolbar"><el-button @click="back">{{ t('monitor.engines.back') }}</el-button><h3>{{ owners[0]?.display_name }}</h3><el-tag effect="plain">{{ t(`monitor.engines.scopes.${state.scope}`) }}</el-tag><span>{{ collection(state.deploymentID) }}</span></div>
        <div v-if="owners.length" class="toolbar"><span>{{ t('monitor.resources.associatedHost') }}:</span><MonitoredHostLink data-testid="engine-host" :node-id="owners[0].node_id" :host="hostReferences[owners[0].node_id]" :allowed="canReadHost" :empty-label="t('monitor.engines.noCurrentHost')" /></div>
        <el-tabs v-if="groups.length" :model-value="selectedMetric.group" @update:model-value="group=>navigate({...state.query,metric:engineMetricsFor(state.engineType).find(m=>m.group===group).key})"><el-tab-pane v-for="group in groups" :key="group" :name="group" :label="t(`monitor.engines.groups.${group}`)"/></el-tabs>
        <div v-loading="loading && !observed" class="metric-grid" data-testid="engine-metrics">
          <article v-for="metric in detailMetrics" :key="metric.key" class="metric-card" :data-testid="`engine-metric-${metric.name}`">
            <h3 class="metric-heading" :aria-label="t(`monitor.engines.metrics.${metric.name}`)"><span>{{ t(`monitor.engines.metrics.${metric.name}`) }}</span><FieldHelp :content="t(`monitor.engines.hints.${metric.name}`)" /></h3>
            <strong :data-testid="`engine-value-${metric.name}`">{{ value(observation(state.deploymentID)?.series.find(item => item.metric_key === metric.key)?.points[0], metric) }}</strong>
            <p v-if="observation(state.deploymentID)?.series.find(item => item.metric_key === metric.key)?.points[0]?.data_state !== 'valid'" class="hint" :data-testid="`engine-missing-${metric.name}`">{{ missingMessage(metric) }}</p>
            <el-button text type="primary" :disabled="loading" @click="navigate({ ...state.query, metric: metric.key })">{{ t('monitor.engines.viewTrend') }}</el-button>
          </article>
        </div>
        <section class="trend" data-testid="engine-trend">
          <div class="toolbar">
            <h3>{{ t('monitor.engines.trend') }}</h3>
            <el-select :model-value="state.metric" :aria-label="t('monitor.engines.metric')" @update:model-value="metric => navigate({ ...state.query, metric })"><el-option v-for="metric in detailMetrics" :key="metric.key" :value="metric.key" :label="t(`monitor.engines.metrics.${metric.name}`)" /></el-select>
            <span class="metric-heading" data-testid="engine-trend-help"><FieldHelp :content="t(`monitor.engines.hints.${selectedMetric.name}`) + ' ' + t('monitor.resources.zoomHint')" /></span>
            <el-select :model-value="state.range" :aria-label="t('monitor.resources.range')" @update:model-value="range => navigate({ ...state.query, range })"><el-option v-for="(_, range) in resourceRanges" :key="range" :value="range" :label="t(`monitor.resources.ranges.${range}`)" /></el-select>
            <span class="metric-heading" data-testid="engine-range-help"><FieldHelp :content="t('monitor.engines.trendScope')" /></span>
          </div>
          <p v-if="currentMissingReason" class="hint" data-testid="engine-current-missing">{{ t(`monitor.engines.missingReasons.${currentMissingReason}`) }}</p>
          <el-alert v-if="trendError" :title="t(trendError)" type="warning" :closable="false" />
          <template v-if="chart">
            <p class="hint">{{ t('monitor.resources.window', { start: date(history.start), end: date(history.end), step: history.step_seconds }) }}</p>
            <div class="chart" role="img" :aria-label="t(`monitor.engines.metrics.${selectedMetric.name}`)"><ChartRenderer :rows="chart.rows" :config="chart.config" zoomable :initial-zoom="trendZoom" @zoom-change="trendZoom = $event" @invalid="invalidChart" /></div>
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
import FieldHelp from '../../../../common-frontend/basic/src/components/FieldHelp.vue'
import AutoRefreshControl from '../../../../common-frontend/basic/src/components/AutoRefreshControl.vue'
import ChartRenderer from '../../../../common-frontend/chart/src/ChartRenderer.vue'
import { createAutoRefresh } from '../../../../common-frontend/basic/src/utils/autoRefresh.mjs'
import { createAutoFilter } from '../../../../common-frontend/basic/src/utils/autoFilter.mjs'
import { createLatestRequestScope } from '../../../../common-frontend/basic/src/utils/latestRequestScope.mjs'
import { buildMonitorEngineResourcesLocation } from '../../../../common-frontend/basic/src/utils/engineResourceRoute.mjs'
import { formatBytes, scaleByteValue, formatDurationSeconds } from '../../../../common-frontend/basic/src/utils/formatters.js'
import { formatFieldPresentationValue } from '../../../../common-frontend/basic/src/utils/fieldPresentation.mjs'
import { useAuthStore } from '../store/auth'
import { engineResourcesAPI as api } from '../api/engineResources'
import { nodeResourcesAPI as hostsAPI } from '../api/nodeResources'
import { loadHostReferences } from '../utils/hostReferences'
import MonitoredHostLink from '../components/MonitoredHostLink.vue'
import { engineGroups, engineMetricsFor, engineSummaryFor, engineDetailMetrics, engineMetricMissingReason, defaultEngineMetric, resolveEngineRoute, validateEnginePage, validateEngineDeployment, validateEngineResources, engineTrendParameters } from '../utils/engineResources'
import { currentResourceValue, resourceErrorKey, resourceChartRows, resourceRanges } from '../utils/nodeResources'
import { navigateMonitorRoute } from '../utils/moduleNavigation'

const auth = useAuthStore(), route = useRoute(), router = useRouter(), { t, locale } = useI18n()
const canRead = computed(() => auth.contextType === 'platform' && auth.authContext?.principal?.type === 'user' && !auth.authContext?.delegation && auth.hasPermission('monitor.resource_observation.read') && auth.hasPermission('platform.engine_deployment.read'))
const canReadHost = computed(() => canRead.value && auth.hasPermission('platform.host_node.read'))
const hostReferences = ref({})
const identity = computed(() => JSON.stringify([auth.authContext?.principal, auth.authContext?.context, auth.authContext?.delegation, auth.permissions]))
const state = computed(() => resolveEngineRoute(route.query)), owners = ref([]), total = ref(0), observed = ref(null), history = ref(null)
const loading = ref(false), errorKey = ref(''), trendError = ref(''), search = ref(''), blocked = ref(false), visible = ref(!document.hidden)
const trendZoom = ref(null)
const requests = createLatestRequestScope()
let disposed = false, dataContext = ''
const refreshStatus = computed(() => state.value.refresh === 'off' ? 'manual' : blocked.value ? 'stopped' : !visible.value ? 'paused' : 'automatic')
const groups = computed(() => engineGroups(state.value.engineType))
const detailMetrics = computed(() => engineDetailMetrics(state.value.engineType, state.value.metric))
const engineNames = { postgresql: 'PostgreSQL', redis: 'Redis', minio: 'MinIO' }
const selectedMetric = computed(() => engineMetricsFor(state.value.engineType).find(item => item.key === state.value.metric))
const observation = id => observed.value?.data.find(row => row.subject.deployment_id === id)
const collection = id => t(`monitor.engines.states.${observation(id)?.collection.state || 'no_sample'}`)
const currentMissingReason = computed(() => engineMetricMissingReason(observation(state.value.deploymentID), state.value.metric))
function missingMessage(metric) {
  const reason = engineMetricMissingReason(observation(state.value.deploymentID), metric.key)
  return t(reason ? `monitor.engines.missingReasons.${reason}` : 'monitor.engines.missing')
}
const date = value => formatFieldPresentationValue(value, { temporal_format: 'datetime' }, locale.value)
function value(point, metric) {
  const number = currentResourceValue(point)
  if (number === null) return '—'
  if (metric.unit === 'status') return t(`monitor.engines.statusValues.${metric.name}.${number}`)
  if (metric.name === 'redisLimit' && number === 0) return t('monitor.engines.noLimit')
  if (metric.unit === 'seconds') return formatDurationSeconds(number, locale.value)
  return ['bytes','bytes_per_second'].includes(metric.unit) ? formatBytes(number, null, locale.value) + (metric.unit==='bytes_per_second'?'/s':'') : formatFieldPresentationValue(number, { precision: metric.windowSeconds || metric.unit==='percent' ? 2 : 0, unit: t(`monitor.engines.units.${metric.unit}`) }, locale.value)
}
const chart = computed(() => {
  const series = history.value?.data[0]?.series[0]
  if (!series) return null
  const metric = selectedMetric.value
  const scale = ['bytes','bytes_per_second'].includes(metric.unit) ? scaleByteValue(Math.max(0, ...series.points.map(point => currentResourceValue(point) ?? 0))) : null
  return { rows: resourceChartRows(series).map(row => ({ ...row, value: row.value === null ? null : row.value / (scale?.divisor || 1) })), config: { chart_type: 'line', dimension: 'evaluated_at', measures: ['value'], field_presentations: [{ field: 'evaluated_at', label: t('monitor.resources.evaluatedAt'), temporal_format: 'datetime' }, { field: 'value', label: t(`monitor.engines.metrics.${metric.name}`), unit: scale ? scale.unit+(metric.unit==='bytes_per_second'?'/s':'') : t(`monitor.engines.units.${metric.unit}`), precision: metric.windowSeconds || metric.unit==='percent' ? 2 : 0 }] } }
})
const navigate = (query, history = 'replace') => {
  filterInput.cancel()
  const target = buildMonitorEngineResourcesLocation(resolveEngineRoute(query).query)
  if (router.resolve(target).fullPath === route.fullPath) { refreshCycle.schedule(); return }
  return navigateMonitorRoute(router, target, { history })
}
const filterInput = createAutoFilter({ apply: () => navigate({ scope:state.value.scope, search: search.value.trim(), refresh: state.value.refresh }), enabled: () => !disposed && route.name === 'EngineResources' && !state.value.deploymentID && canRead.value, visible: () => visible.value, pause: () => refreshCycle.stop() })
function back() { const { deployment_id, engine_type, metric, range, ...query } = state.value.query; return navigate(query) }
function invalidChart() { history.value = null; trendError.value = 'monitor.resources.errors.invalidResponse' }
const refreshCycle = createAutoRefresh({ interval: () => state.value.refresh, enabled: () => canRead.value && route.name === 'EngineResources' && !blocked.value && !loading.value && !state.value.changed && !filterInput.pending, refresh: () => reload({ automatic: true }), pause: () => { requests.invalidate(); loading.value = false; observed.value = null; history.value = null; hostReferences.value = {} }, visibilityChanged: value => { visible.value = value; filterInput.visibilityChanged(value) } })
async function reload({ automatic = false } = {}) {
  if (!automatic) filterInput.cancel()
  if (disposed || route.name !== 'EngineResources') return
  refreshCycle.stop()
  const ticket = requests.invalidate()
  const context = JSON.stringify([identity.value, state.value.scope, state.value.engineType, state.value.deploymentID, state.value.metric, state.value.range, state.value.search, state.value.page])
  if (context !== dataContext) {
    observed.value = null; history.value = null; owners.value = []; hostReferences.value = {}; total.value = 0
    dataContext = context
  }
  errorKey.value = ''; trendError.value = ''
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
      if (!requests.current(ticket)) return
      if (owner.engine_type !== state.value.engineType || owner.scope !== state.value.scope) {
        await navigate({ ...state.value.query, scope: owner.scope, engine_type: owner.engine_type, metric: owner.engine_type === state.value.engineType ? state.value.metric : defaultEngineMetric(owner.engine_type) })
        return
      }
      rows = [owner]; count = 1
    } else {
      const page = validateEnginePage(await requests.request(config => api.deployments({ scope: state.value.scope, page: state.value.page, page_size: 20, ...(state.value.search ? { search: state.value.search } : {}) }, config)), state.value.page)
      if(page.data.some(row=>row.scope!==state.value.scope))throw new Error('invalid_resource_response')
      rows = page.data; count = page.total
    }
    if (!requests.current(ticket)) return
    if (rows.length !== owners.value.length || rows.some((row, index) =>
      ['deployment_id', 'scope', 'engine_type', 'display_name', 'version', 'enabled', 'node_id', 'host_binding_state'].some(key => row[key] !== owners.value[index]?.[key]))) {
      observed.value = null; history.value = null; trendZoom.value = null
    }
    owners.value = rows; total.value = count
    hostReferences.value = Object.fromEntries(Object.entries(hostReferences.value).filter(([id]) => rows.some(row => row.node_id === id)))
    if (!rows.length) { observed.value = null; history.value = null }
    void loadHosts(owners.value, ticket)
    if (rows.length) {
      const result = state.value.deploymentID ? await requests.request(config => api.instant({ deployment_id: state.value.deploymentID, metrics: detailMetrics.value.map(item => item.key).join(',') }, config)) : await requests.request(config => api.summaries(rows.map(item => item.deployment_id), config))
      if (!requests.current(ticket)) return
      observed.value = validateEngineResources(result, rows, owner => state.value.deploymentID ? detailMetrics.value : engineSummaryFor(owner.engine_type))
      if (state.value.deploymentID) {
        try {
          const params = engineTrendParameters(state.value.deploymentID, state.value.metric, state.value.range, result.queried_at)
          const trend = await requests.request(config => api.trend(params, config))
          if (!requests.current(ticket)) return
          history.value = validateEngineResources(trend, rows, () => [selectedMetric.value], params)
        } catch (error) {
          if (!requests.current(ticket)) return
          history.value = null
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
watch([() => JSON.stringify([state.value.scope, state.value.engineType, state.value.deploymentID, state.value.metric, state.value.range]), identity], () => { trendZoom.value = null }, { flush: 'sync' })
watch([errorKey, trendError], ([error, trend]) => { if (error || trend) trendZoom.value = null }, { flush: 'sync' })
watch([() => route.fullPath, identity], () => reload(), { immediate: true, flush: 'sync' })
onBeforeUnmount(() => { disposed = true; filterInput.dispose(); refreshCycle.dispose(); requests.dispose() })
</script>

<style scoped>
.engine-resources { padding: 24px; min-width: 0; color: var(--addp-text-primary); }
.toolbar { display: flex; flex-wrap: wrap; align-items: center; gap: 12px; margin-bottom: 16px; }
.toolbar h2 { flex: 1; }
.toolbar .el-input, .toolbar .el-select { width: 240px; max-width: 100%; }
.engine-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(min(100%, 360px), 1fr)); gap: 16px; }
.engine-card { padding: 20px; border: 1px solid var(--addp-border-color); border-radius: 8px; background: var(--addp-bg-secondary); min-width: 0; }
.engine-card-heading { display: flex; align-items: flex-start; justify-content: space-between; gap: 12px; }
.engine-card-heading h3 { margin: 0; overflow-wrap: anywhere; font-size: 18px; }
.engine-card-host { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 14px; overflow-wrap: anywhere; color: var(--addp-text-secondary); }
.engine-card-state { display: flex; gap: 6px; flex-wrap: wrap; color: var(--addp-text-secondary); }
.engine-summary { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 18px 12px; margin: 20px 0; }
.engine-summary dt { color: var(--addp-text-secondary); font-size: 13px; }
.engine-summary dd { margin: 6px 0 0; font-size: 20px; overflow-wrap: anywhere; }
.metric-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(280px, 100%), 1fr)); gap: 16px; }
.metric-card { padding: 20px; border: 1px solid var(--addp-border-color); border-radius: 8px; background: var(--addp-bg-secondary); min-width: 0; }
.metric-card h3 { font-size: 16px; margin: 0 0 12px; }
.metric-heading { display: flex; align-items: center; gap: 4px; min-width: 0; overflow-wrap: anywhere; }
.metric-card strong { font-size: 24px; overflow-wrap: anywhere; }
.hint { margin: 8px 0; color: var(--addp-text-secondary); line-height: 1.5; font-size: 13px; }
.trend, .el-pagination { margin-top: 20px; }
.chart { display: flex; min-width: 0; }
</style>
