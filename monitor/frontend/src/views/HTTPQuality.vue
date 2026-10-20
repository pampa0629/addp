<template>
  <section class="http-quality" data-testid="http-quality">
    <el-alert v-if="!canRead" :title="t('monitor.http.denied')" type="warning" :closable="false" />
    <template v-else>
      <header><h2>{{ t('monitor.http.title') }}</h2><el-button @click="reload()">{{ t('common.refresh') }}</el-button><AutoRefreshControl :model-value="state.refresh" :status="refreshStatus" @update:model-value="refresh => navigate({ ...state.query, refresh })" /></header>
      <p class="hint">{{ t('monitor.http.boundary') }}</p>
      <form class="filters" @submit.prevent="filter">
        <el-input v-model="moduleDraft" :placeholder="t('monitor.process.module')" :aria-label="t('monitor.process.module')" />
        <el-select v-model="roleDraft" :aria-label="t('monitor.http.layer')"><el-option v-for="role in ['backend', 'ingress']" :key="role" :value="role" :label="t(`monitor.http.layers.${role}`)" /></el-select>
        <el-select :model-value="String(state.window)" :aria-label="t('monitor.http.window')" @update:model-value="window => navigate({ ...state.query, window })"><el-option v-for="window in [60, 300, 900]" :key="window" :value="String(window)" :label="t('monitor.http.minutes', { count: window / 60 })" /></el-select>
        <el-button native-type="submit">{{ t('common.search') }}</el-button>
      </form>
      <el-alert v-if="error" :title="t(error)" type="warning" :closable="false" data-testid="http-error" />
      <el-table :data="instances" v-loading="loading" :empty-text="t('monitor.http.empty')" data-testid="http-instances">
        <el-table-column prop="module_name" :label="t('monitor.process.module')" min-width="140" />
        <el-table-column prop="instance_id" :label="t('monitor.http.instance')" min-width="180" show-overflow-tooltip />
        <el-table-column :label="t('monitor.http.collection')" min-width="145"><template #default="{ row }">{{ t(`monitor.http.states.${summary(row)?.collection.state || 'no_sample'}`) }}</template></el-table-column>
        <el-table-column v-for="metric in httpMetrics" :key="metric.key" :label="t(`monitor.http.metrics.${metric.name}`)" min-width="115"><template #default="{ row }">{{ display(summary(row)?.observations[0], metric.name) }}</template></el-table-column>
        <el-table-column :label="t('common.actions')" min-width="120"><template #default="{ row }"><el-button link @click="navigate({ ...state.query, instance_id: String(row.id) }, 'push')">{{ t('monitor.http.details') }}</el-button></template></el-table-column>
      </el-table>
      <el-pagination layout="total, prev, pager, next" :total="total" :page-size="10" :current-page="state.page" @current-change="page => navigate({ ...state.query, page: String(page) })" />
      <el-dialog :model-value="Boolean(state.instanceID)" :title="t('monitor.http.details')" width="90%" @close="closeDetail">
        <p v-if="selected" class="subject">{{ selected.module_name }} · {{ selected.instance_id }}</p>
        <el-alert v-if="detailError" :title="t(detailError)" type="warning" :closable="false" data-testid="http-detail-error" />
        <p class="hint">{{ t('monitor.http.samples') }}</p>
        <div class="filters"><el-select :model-value="state.sort" :aria-label="t('monitor.http.sort')" @update:model-value="sort => navigate({ ...state.query, sort })"><el-option v-for="sort in ['mean', 'qps']" :key="sort" :value="sort" :label="t(`monitor.http.metrics.${sort}`)" /></el-select><el-button v-if="state.route !== null" @click="selectRoute(null)">{{ t('monitor.http.allRoutes') }}</el-button></div>
        <el-table :data="ranked" :empty-text="t('monitor.http.noInterfaces')" data-testid="http-interfaces">
          <el-table-column :label="t('monitor.http.route')" min-width="230"><template #default="{ row }"><el-button link @click="selectRoute(row)">{{ row.method }} {{ row.route || t('monitor.http.unmatched') }}</el-button></template></el-table-column>
          <el-table-column v-for="metric in httpMetrics.slice(0, -1)" :key="metric.key" :label="t(`monitor.http.metrics.${metric.name}`)" min-width="110"><template #default="{ row }">{{ display(row, metric.name) }}</template></el-table-column>
        </el-table>
        <div class="filters"><el-select v-model="chartMetric" :aria-label="t('monitor.http.metric')"><el-option v-for="metric in chartMetrics" :key="metric.key" :value="metric.name" :label="t(`monitor.http.metrics.${metric.name}`)" /></el-select><el-select :model-value="state.range" :aria-label="t('monitor.http.range')" @update:model-value="range => navigate({ ...state.query, range })"><el-option v-for="range in ['5m', '1h', '24h']" :key="range" :value="range" :label="t(`monitor.resources.ranges.${range}`)" /></el-select></div>
        <p v-if="history" class="hint">{{ t('monitor.resources.window', { start: history.start, end: history.end, step: history.step_seconds }) }}</p>
        <div v-if="chart" class="chart" role="img" :aria-label="t('monitor.http.trend')" data-testid="http-trend"><ChartRenderer :rows="chart.rows" :config="chart.config" @invalid="invalidChart" /></div>
      </el-dialog>
    </template>
  </section>
</template>

<script setup>
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import AutoRefreshControl from '../../../../common-frontend/basic/src/components/AutoRefreshControl.vue'
import ChartRenderer from '../../../../common-frontend/chart/src/ChartRenderer.vue'
import { createLatestRequestScope } from '../../../../common-frontend/basic/src/utils/latestRequestScope.mjs'
import { createAutoRefresh } from '../../../../common-frontend/basic/src/utils/autoRefresh.mjs'
import { useAuthStore } from '../store/auth'
import { httpQualityAPI as api } from '../api/httpQuality'
import { httpMetrics, httpValue, resolveHTTPRoute, sortHTTPInterfaces, validateHTTPResponse } from '../utils/httpQuality'
import { validateProcessInstances } from '../utils/processResources'
import { resourceErrorKey, resourceChartRows } from '../utils/nodeResources'
import { navigateMonitorRoute } from '../utils/moduleNavigation'

const { t, locale } = useI18n(), route = useRoute(), router = useRouter(), auth = useAuthStore()
const canRead = computed(() => auth.contextType === 'platform' && auth.authContext?.principal?.type === 'user' && !auth.authContext?.delegation && auth.hasPermission('monitor.http_observation.read') && auth.hasPermission('platform.module.read'))
const identity = computed(() => JSON.stringify([auth.authContext?.principal, auth.authContext?.context, auth.authContext?.delegation, auth.permissions]))
const state = computed(() => resolveHTTPRoute(route.query))
const instances = ref([]), summaries = ref([]), interfaces = ref([]), history = ref(null), total = ref(0), loading = ref(false), error = ref(''), detailError = ref('')
const moduleDraft = ref(''), roleDraft = ref('backend'), chartMetric = ref('qps'), blocked = ref(false), visible = ref(!document.hidden), requests = createLatestRequestScope()
const selected = computed(() => instances.value.find(row => row.id === state.value.instanceID))
const ranked = computed(() => sortHTTPInterfaces(interfaces.value, state.value.sort))
const chartMetrics = computed(() => state.value.route === null ? httpMetrics : httpMetrics.slice(0, -1))
const refreshStatus = computed(() => state.value.refresh === 'off' ? 'manual' : blocked.value ? 'stopped' : !visible.value ? 'paused' : 'automatic')
const summary = row => summaries.value.find(item => item.subject.id === row.id)
function display(row, name) { const value = httpValue(row, name); return value === null ? '—' : new Intl.NumberFormat(locale.value, { maximumFractionDigits: name === 'active' ? 0 : 3 }).format(value) }
const navigate = (query, history = 'replace') => navigateMonitorRoute(router, { path: '/http-quality', query: resolveHTTPRoute(query).query }, { history })
function filter() { return navigate({ ...state.value.query, page: undefined, instance_id: undefined, route: undefined, method: undefined, module_name: moduleDraft.value.trim(), role: roleDraft.value }) }
function closeDetail() { const { instance_id, route, method, range, ...query } = state.value.query; return navigate(query) }
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
  if (!automatic) { moduleDraft.value = state.value.module; roleDraft.value = state.value.role; blocked.value = false }
  instances.value = []; summaries.value = []; interfaces.value = []; history.value = null; total.value = 0; error.value = ''; detailError.value = ''; loading.value = false
  if (!canRead.value || route.name !== 'HTTPQuality') return
  if (state.value.changed) { await navigate(state.value.query); return }
  loading.value = true
  try {
    const list = await requests.request(config => api.instances(state.value.params, config))
    if (!requests.current(ticket)) return
    validateProcessInstances(list, state.value.page, state.value.params, 10)
    if (list.data.some(row => row.role !== state.value.role || (state.value.module && row.module_name !== state.value.module))) throw new Error('invalid_http_quality_response')
    let quality = { data: [] }
    if (list.data.length) quality = validateHTTPResponse(await requests.request(config => api.summaries(list.data.map(row => row.id), state.value.window, config)), list.data, state.value.window)
    if (!requests.current(ticket)) return
    instances.value = list.data; summaries.value = quality.data; total.value = list.total
    if (state.value.instanceID) {
      const owner = list.data.find(row => row.id === state.value.instanceID)
      if (!owner) { detailError.value = 'monitor.process.trend.reselect'; return }
      try {
        const detail = validateHTTPResponse(await requests.request(config => api.interfaces(owner.id, state.value.window, state.value.sort, config)), [owner], state.value.window, true)
        if (!requests.current(ticket)) return
        interfaces.value = detail.data[0].observations
        const end = new Date(detail.end), span = { '5m': 300, '1h': 3600, '24h': 86400 }[state.value.range]
        const params = { instance_ids: owner.id, window_seconds: state.value.window, start: new Date(end.getTime() - span * 1000).toISOString(), end: end.toISOString(), ...(state.value.route === null ? {} : { route: state.value.route, method: state.value.method }) }
        const trend = validateHTTPResponse(await requests.request(config => api.trend(params, config)), [owner], state.value.window, false, state.value.route === null ? null : { route: state.value.route, method: state.value.method })
        if (!requests.current(ticket)) return
        history.value = trend
      } catch (cause) { if (!requests.current(ticket)) return; detailError.value = cause.message === 'invalid_http_quality_response' ? 'monitor.resources.errors.invalidResponse' : resourceErrorKey(cause); if ([401, 403, 404].includes(cause.response?.status)) throw cause }
    }
  } catch (cause) {
    if (!requests.current(ticket)) return
    error.value = cause.message === 'invalid_http_quality_response' ? 'monitor.resources.errors.invalidResponse' : resourceErrorKey(cause)
    instances.value = []; summaries.value = []; interfaces.value = []; history.value = null; total.value = 0
    if ([401, 403, 404].includes(cause.response?.status)) blocked.value = true
  } finally { if (requests.current(ticket)) { loading.value = false; cycle.schedule() } }
}
watch([() => route.fullPath, identity], () => reload(), { immediate: true, flush: 'sync' })
onBeforeUnmount(() => { cycle.dispose(); requests.dispose() })
</script>

<style scoped>
.http-quality { padding: 24px; min-width: 0; color: var(--addp-text-primary); }
header, .filters { display: flex; align-items: center; flex-wrap: wrap; gap: 12px; margin-bottom: 16px; }
header h2 { margin-right: auto; }
.filters .el-input, .filters .el-select { width: 200px; max-width: 100%; }
.hint { color: var(--addp-text-secondary); font-size: 13px; margin: 12px 0; }
.subject { overflow-wrap: anywhere; }
.el-pagination { margin-top: 16px; }
.chart { display: flex; min-width: 0; margin-top: 16px; }
</style>
