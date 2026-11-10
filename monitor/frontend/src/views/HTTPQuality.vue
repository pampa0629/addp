<template>
  <section class="http-quality" data-testid="http-quality">
    <el-alert v-if="!canRead" :title="t('monitor.http.denied')" type="warning" :closable="false" />
    <template v-else>
      <header class="toolbar">
        <h2>{{ t('monitor.http.title') }}</h2>
        <AutoRefreshControl v-if="!state.stopped" :model-value="state.refresh" :status="refreshStatus" @update:model-value="refresh => navigate({ ...state.query, refresh })" />
        <el-button :loading="loading" @click="refresh">{{ t('common.refresh') }}</el-button>
      </header>
      <div class="filters">
        <ModuleNameSelect :model-value="state.module" :identity="identity" :suspended="blocked" :revision="moduleRevision" @update:model-value="module_name => selectScope({ module_name })" />
        <div class="layer-filter">
          <el-radio-group :model-value="state.role" :aria-label="t('monitor.http.layer')" @update:model-value="role => selectScope({ role })">
            <el-radio-button v-for="role in ['backend', 'ingress']" :key="role" :value="role">{{ t(`monitor.http.layers.${role}`) }}</el-radio-button>
          </el-radio-group>
          <FieldHelp :content="t('monitor.http.boundary')" />
        </div>
        <el-select :model-value="state.params.status" :aria-label="t('monitor.http.instanceScope')" @update:model-value="status => selectScope({ status })"><el-option value="up" :label="t('monitor.http.onlineInstances')" /><el-option value="down" :label="t('monitor.http.stoppedInstances')" /></el-select>
        <el-select :model-value="String(state.window)" :aria-label="t('monitor.http.window')" @update:model-value="window => navigate({ ...state.query, window })"><el-option v-for="window in [60, 300, 900]" :key="window" :value="String(window)" :label="t('monitor.http.minutes', { count: window / 60 })" /></el-select>
        <el-button @click="navigate({ refresh: state.stopped ? undefined : state.refresh })">{{ t('common.reset') }}</el-button>
      </div>
      <el-alert v-if="error" :title="t(error)" type="warning" :closable="false" data-testid="http-error" />
      <div v-loading="loading && !instances.length" data-testid="http-instances">
        <el-empty v-if="!loading && !instances.length && !error" :description="t(state.stopped ? 'monitor.http.noStoppedInstances' : 'monitor.http.empty')" />
        <ModuleInstanceCards :instances="instances" test-prefix="http" v-slot="{ row }">
          <p class="hint collection">
            <strong>{{ t(`monitor.http.states.${state.stopped ? 'not_active' : summary(row)?.collection.state || 'no_sample'}`) }}</strong>
            <span v-if="summary(row)?.collection.sampled_at"> · {{ t('monitor.resources.sampledAt') }} · {{ date(summary(row).collection.sampled_at) }}</span>
          </p>
          <template v-if="!state.stopped">
            <dl class="instance-metrics">
              <div v-for="metric in headlineMetrics" :key="metric.key">
                <dt>{{ t(`monitor.http.metrics.${metric.name}`) }}</dt>
                <dd><strong :data-http-metric="metric.key">{{ display(summary(row)?.observations[0], metric.name) }}</strong></dd>
              </div>
            </dl>
            <details>
              <summary>{{ t('monitor.process.moreMetrics') }}</summary>
              <dl class="instance-metrics">
                <div v-for="metric in moreMetrics" :key="metric.key">
                  <dt>{{ t(`monitor.http.metrics.${metric.name}`) }}</dt>
                  <dd><strong :data-http-metric="metric.key">{{ display(summary(row)?.observations[0], metric.name) }}</strong></dd>
                </div>
              </dl>
              <p class="hint instance-id">{{ t('monitor.http.instance') }} · {{ row.instance_id }}</p>
            </details>
          </template>
          <p v-else class="hint instance-id">{{ t('monitor.http.instance') }} · {{ row.instance_id }}</p>
          <el-button text type="primary" :disabled="state.stopped && !stoppedHTTPWindow(row, state.range)" @click="openDetail(row)">{{ t(state.stopped ? 'monitor.http.historyTrend' : 'monitor.http.details') }}</el-button>
        </ModuleInstanceCards>
      </div>
      <el-pagination v-if="!error && (instances.length || !loading)" layout="total, prev, pager, next" :total="total" :page-size="10" :current-page="state.page" @current-change="page => selectScope({ page: String(page) })" />
      <el-dialog :model-value="Boolean(state.instanceID)" :title="t(state.stopped ? 'monitor.http.historyTrend' : 'monitor.http.details')" width="min(1120px, calc(100vw - 32px))" @close="closeDetail">
        <p v-if="selected" class="subject">{{ selected.module_name }} · {{ selected.instance_id }} <el-button v-if="!state.stopped && auth.hasPermission('monitor.trace.read')" link @click="openTraces()">{{ t('monitor.traces.open') }}</el-button></p>
        <el-alert v-if="detailError" :title="t(detailError)" type="warning" :closable="false" data-testid="http-detail-error" />
        <template v-if="!state.stopped">
        <p class="hint">{{ t('monitor.http.samples') }}</p>
        <div class="filters"><el-select :model-value="state.sort" :aria-label="t('monitor.http.sort')" @update:model-value="sort => navigate({ ...state.query, sort })"><el-option v-for="sort in ['mean', 'qps']" :key="sort" :value="sort" :label="t(`monitor.http.metrics.${sort}`)" /></el-select><el-button v-if="state.route !== null" @click="selectRoute(null)">{{ t('monitor.http.allRoutes') }}</el-button></div>
        <el-table :data="ranked" :empty-text="t('monitor.http.noInterfaces')" data-testid="http-interfaces">
          <el-table-column :label="t('monitor.http.route')" min-width="230"><template #default="{ row }"><el-button link @click="selectRoute(row)">{{ row.method }} {{ row.route || t('monitor.http.unmatched') }}</el-button></template></el-table-column>
          <el-table-column v-for="metric in httpMetrics.slice(0, -1)" :key="metric.key" :label="t(`monitor.http.metrics.${metric.name}`)" min-width="110"><template #default="{ row }">{{ display(row, metric.name) }}</template></el-table-column>
        </el-table>
        </template>
        <div class="filters"><el-select v-model="chartMetric" :aria-label="t('monitor.http.metric')"><el-option v-for="metric in chartMetrics" :key="metric.key" :value="metric.name" :label="t(`monitor.http.metrics.${metric.name}`)" /></el-select><el-select :model-value="state.range" :aria-label="t('monitor.http.range')" @update:model-value="selectRange"><el-option v-for="range in ['5m', '1h', '24h']" :key="range" :value="range" :label="t(`monitor.resources.ranges.${range}`)" /></el-select></div>
        <p v-if="history?.data[0]?.history_state && history.data[0].history_state !== 'available'" class="hint" data-testid="http-history-state">{{ t(`monitor.http.historyStates.${history.data[0].history_state}`) }}</p>
        <p v-if="history" class="hint">{{ t('monitor.resources.window', { start: history.start, end: history.end, step: history.step_seconds }) }}</p>
        <template v-if="canOpenFailureTraces">
          <el-button v-if="failureTraceWindow" type="primary" link @click="openTraces(failureTraceWindow)">{{ t('monitor.traces.openHTTPFailures') }}</el-button>
          <p v-else-if="history && state.range === '24h'" class="hint">{{ t('monitor.traces.shorterHTTPWindow') }}</p>
        </template>
        <div v-if="chart" class="chart" role="img" :aria-label="t('monitor.http.trend')" data-testid="http-trend"><ChartRenderer :rows="chart.rows" :config="chart.config" @invalid="invalidChart" /></div>
      </el-dialog>
    </template>
  </section>
</template>

<script setup>
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import ModuleInstanceCards from '../components/ModuleInstanceCards.vue'
import ModuleNameSelect from '../components/ModuleNameSelect.vue'
import FieldHelp from '../../../../common-frontend/basic/src/components/FieldHelp.vue'
import { formatFieldPresentationValue } from '../../../../common-frontend/basic/src/utils/fieldPresentation.mjs'
import AutoRefreshControl from '../../../../common-frontend/basic/src/components/AutoRefreshControl.vue'
import ChartRenderer from '../../../../common-frontend/chart/src/ChartRenderer.vue'
import { createLatestRequestScope } from '../../../../common-frontend/basic/src/utils/latestRequestScope.mjs'
import { createAutoRefresh } from '../../../../common-frontend/basic/src/utils/autoRefresh.mjs'
import { useAuthStore } from '../store/auth'
import { isTraceHTTPRoute } from '../utils/traces'
import { httpQualityAPI as api } from '../api/httpQuality'
import { httpMetrics, httpValue, resolveHTTPRoute, sortHTTPInterfaces, stoppedHTTPWindow, validateHTTPResponse } from '../utils/httpQuality'
import { validateProcessInstances } from '../utils/processResources'
import { resourceErrorKey, resourceChartRows } from '../utils/nodeResources'
import { navigateMonitorRoute } from '../utils/moduleNavigation'

const { t, locale } = useI18n(), route = useRoute(), router = useRouter(), auth = useAuthStore()
const canRead = computed(() => auth.contextType === 'platform' && auth.authContext?.principal?.type === 'user' && !auth.authContext?.delegation && auth.hasPermission('monitor.http_observation.read') && auth.hasPermission('platform.module.read'))
const identity = computed(() => JSON.stringify([auth.authContext?.principal, auth.authContext?.context, auth.authContext?.delegation, auth.permissions]))
const state = computed(() => resolveHTTPRoute(route.query))
const instances = ref([]), summaries = ref([]), interfaces = ref([]), history = ref(null), total = ref(0), loading = ref(false), error = ref(''), detailError = ref('')
const moduleRevision = ref(0), chartMetric = ref('qps'), blocked = ref(false), visible = ref(!document.hidden), requests = createLatestRequestScope()
const historicalOwner = ref(null)
let listContext = '', detailContext = ''
const headlineMetrics = httpMetrics.filter(metric => ['qps', 'failure', 'p95'].includes(metric.name))
const moreMetrics = httpMetrics.filter(metric => !headlineMetrics.includes(metric))
const date = value => formatFieldPresentationValue(value, { temporal_format: 'datetime' }, locale.value)
const selected = computed(() => state.value.stopped ? historicalOwner.value : instances.value.find(row => row.id === state.value.instanceID))
const canOpenFailureTraces = computed(() => !state.value.stopped && !!selected.value && canRead.value &&
  auth.hasPermission('monitor.trace.read') && isTraceHTTPRoute(state.value.route))
const failureTraceWindow = computed(() => {
  if (!canOpenFailureTraces.value || !history.value) return null
  const { start, end } = history.value
  const duration = Date.parse(end) - Date.parse(start)
  return duration > 0 && duration <= 3_600_000 ? { start, end, http_method: state.value.method, http_route: state.value.route } : null
})
const ranked = computed(() => sortHTTPInterfaces(interfaces.value, state.value.sort))
const chartMetrics = computed(() => state.value.route === null ? httpMetrics : httpMetrics.slice(0, -1))
const refreshStatus = computed(() => state.value.refresh === 'off' ? 'manual' : blocked.value ? 'stopped' : !visible.value ? 'paused' : 'automatic')
const summary = row => summaries.value.find(item => item.subject.id === row.id)
function display(row, name) { const value = httpValue(row, name); return value === null ? '—' : new Intl.NumberFormat(locale.value, { maximumFractionDigits: name === 'active' ? 0 : 3 }).format(value) }
const navigate = (query, history = 'replace') => navigateMonitorRoute(router, { path: '/http-quality', query: resolveHTTPRoute(query).query }, { history })
function refresh() { moduleRevision.value++; return reload() }
function selectScope(changes) {
  const { page, instance_id, range, start, end, route, method, ...query } = state.value.query
  return navigate({ ...query, ...changes })
}
function openDetail(row) { return navigate({ ...state.value.query, instance_id: String(row.id), ...(state.value.stopped ? stoppedHTTPWindow(row, state.value.range) : {}) }, 'push') }
function selectRange(range) {
  const bounds = state.value.stopped && selected.value ? stoppedHTTPWindow(selected.value, range) : null
  return navigate({ ...state.value.query, range, ...bounds })
}
function openTraces(window) {
  if (!selected.value || !canRead.value || !auth.hasPermission('monitor.trace.read')) return
  const end = new Date(), start = new Date(end.getTime() - 900000)
  return navigateMonitorRoute(router, { path: '/traces', query: { instance_id: String(selected.value.id), module_name: selected.value.module_name, role: selected.value.role, ...(window || { start: start.toISOString(), end: end.toISOString() }) } }, { history: 'push' })
}
function closeDetail() { const { instance_id, route, method, range, start, end, ...query } = state.value.query; return navigate(query) }
function selectRoute(row) { const { route, method, ...query } = state.value.query; return navigate({ ...query, ...(row ? { route: row.route, method: row.method } : {}) }) }
function invalidChart() { history.value = null; detailError.value = 'monitor.resources.errors.invalidResponse' }
const chart = computed(() => {
  const metric = chartMetrics.value.find(item => item.name === chartMetric.value) || chartMetrics.value[0]
  const series = history.value?.data[0]?.observations[0]?.series.find(item => item.metric_key === metric.key)
  return series ? { rows: resourceChartRows(series), config: { chart_type: 'line', dimension: 'evaluated_at', measures: ['value'], field_presentations: [{ field: 'evaluated_at', label: t('monitor.resources.evaluatedAt'), temporal_format: 'datetime' }, { field: 'value', label: t(`monitor.http.metrics.${metric.name}`), unit: metric.unit === 'seconds' ? 's' : metric.unit === 'percent' ? '%' : '', precision: 3 }] } } : null
})
const cycle = createAutoRefresh({ interval: () => state.value.refresh, enabled: () => canRead.value && route.name === 'HTTPQuality' && !blocked.value && !loading.value, refresh: () => reload(true), pause: () => { requests.invalidate(); loading.value = false }, visibilityChanged: value => { visible.value = value } })
async function reload(automatic = false) {
  cycle.stop()
  const ticket = requests.invalidate()
  if (!automatic) blocked.value = false
  const nextList = JSON.stringify([state.value.params, state.value.window, identity.value])
  if (nextList !== listContext) {
    listContext = nextList
    instances.value = []; summaries.value = []; total.value = 0
  }
  const nextDetail = JSON.stringify([{ ...state.value.query, refresh: undefined }, identity.value])
  if (nextDetail !== detailContext) {
    detailContext = nextDetail
    interfaces.value = []; historicalOwner.value = null; history.value = null
  }
  error.value = ''; detailError.value = ''; loading.value = false
  if (!canRead.value || route.name !== 'HTTPQuality') return
  if (state.value.changed) { await navigate(state.value.query); return }
  loading.value = true
  try {
    const list = await requests.request(config => api.instances(state.value.params, config))
    if (!requests.current(ticket)) return
    validateProcessInstances(list, state.value.page, state.value.params, 10)
    if (list.data.some(row => row.role !== state.value.role || (state.value.module && row.module_name !== state.value.module))) throw new Error('invalid_http_quality_response')
    let quality = { data: [] }
    if (!state.value.stopped && list.data.length) quality = validateHTTPResponse(await requests.request(config => api.summaries(list.data.map(row => row.id), state.value.window, config)), list.data, state.value.window)
    if (!requests.current(ticket)) return
    instances.value = list.data; summaries.value = quality.data; total.value = list.total
    if (state.value.instanceID) {
      if (state.value.stopped) {
        if (!state.value.history) { historicalOwner.value = null; history.value = null; detailError.value = 'monitor.process.trend.reselect'; return }
        const filters = { ...state.value.params, ids: String(state.value.instanceID), page: 1, page_size: 1 }
        const exact = await requests.request(config => api.instances(filters, config))
        if (!requests.current(ticket)) return
        validateProcessInstances(exact, 1, filters, 1)
        const owner = exact.data[0]
        if (!owner || exact.total !== 1 || owner.id !== state.value.instanceID || owner.role !== filters.role || (filters.module_name && owner.module_name !== filters.module_name)) {
          historicalOwner.value = null; history.value = null; detailError.value = 'monitor.process.trend.reselect'; return
        }
        historicalOwner.value = owner
        const filtered = state.value.route === null ? null : { route: state.value.route, method: state.value.method }
        const bounds = state.value.history
        const params = { instance_ids: owner.id, window_seconds: state.value.window, ...bounds, ...filtered }
        const trend = validateHTTPResponse(await requests.request(config => api.trend(params, config)), [owner], state.value.window, false, filtered, bounds)
        if (requests.current(ticket)) history.value = trend
        return
      }
      const owner = list.data.find(row => row.id === state.value.instanceID)
      if (!owner) { interfaces.value = []; history.value = null; detailError.value = 'monitor.process.trend.reselect'; return }
      let rankedObservations = []
      try {
        const detail = validateHTTPResponse(await requests.request(config => api.interfaces(owner.id, state.value.window, state.value.sort, config)), [owner], state.value.window, true)
        if (!requests.current(ticket)) return
        rankedObservations = detail.data[0].observations
        const end = new Date(detail.end), span = { '5m': 300, '1h': 3600, '24h': 86400 }[state.value.range]
        const params = { instance_ids: owner.id, window_seconds: state.value.window, start: new Date(end.getTime() - span * 1000).toISOString(), end: end.toISOString(), ...(state.value.route === null ? {} : { route: state.value.route, method: state.value.method }) }
        const trend = validateHTTPResponse(await requests.request(config => api.trend(params, config)), [owner], state.value.window, false, state.value.route === null ? null : { route: state.value.route, method: state.value.method })
        if (!requests.current(ticket)) return
        interfaces.value = rankedObservations; history.value = trend
      } catch (cause) {
        if (!requests.current(ticket)) return
        interfaces.value = rankedObservations; history.value = null
        detailError.value = cause.message === 'invalid_http_quality_response' ? 'monitor.resources.errors.invalidResponse' : resourceErrorKey(cause)
        if ([401, 403, 404].includes(cause.response?.status)) throw cause
      }
    }
  } catch (cause) {
    if (!requests.current(ticket)) return
    error.value = cause.message === 'invalid_http_quality_response' ? 'monitor.resources.errors.invalidResponse' : resourceErrorKey(cause)
    instances.value = []; summaries.value = []; interfaces.value = []; historicalOwner.value = null; history.value = null; total.value = 0
    if ([401, 403, 404].includes(cause.response?.status)) blocked.value = true
  } finally { if (requests.current(ticket)) { loading.value = false; cycle.schedule() } }
}
watch([() => route.fullPath, identity], () => reload(), { immediate: true, flush: 'sync' })
onBeforeUnmount(() => { cycle.dispose(); requests.dispose() })
</script>

<style scoped>
.http-quality { --instance-metric-columns: 3; padding: 24px; min-width: 0; color: var(--addp-text-primary); }
.toolbar, .filters { display: flex; align-items: center; flex-wrap: wrap; gap: 12px; margin-bottom: 16px; }
.toolbar h2 { flex: 1; }
.filters .el-select { width: 200px; max-width: 100%; }
.layer-filter { display: flex; align-items: center; gap: 4px; max-width: 100%; }
@media (max-width: 600px) { .toolbar h2 { flex-basis: 100%; } }
.hint { color: var(--addp-text-secondary); font-size: 13px; margin: 12px 0; }
.http-quality details { --instance-metric-columns: 2; }
@media (max-width: 420px) { .http-quality { --instance-metric-columns: 2; } }
.collection, .instance-id { margin: 4px 0; font-size: 12px; }
.subject { overflow-wrap: anywhere; }
.el-pagination { margin-top: 16px; }
.chart { display: flex; min-width: 0; margin-top: 16px; }
</style>
