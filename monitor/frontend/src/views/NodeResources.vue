<template>
  <main v-if="canRead" class="node-resources" data-testid="node-resources">
    <header class="resource-toolbar">
      <h2>{{ t('monitor.resources.title') }}</h2>
      <el-button v-if="nodeID" @click="navigate('/node-resources', listQuery)">{{ t('monitor.resources.back') }}</el-button>
      <el-select v-if="nodeID" :model-value="state.refresh" :aria-label="t('monitor.resources.refreshInterval')" data-testid="resource-refresh" @update:model-value="value => selectTrend('refresh', value)">
        <el-option v-for="value in resourceRefreshOptions" :key="value" :value="value" :label="value === 'off' ? t('monitor.resources.refreshOff') : t('monitor.resources.refreshSeconds', { seconds: value })" />
      </el-select>
      <span v-if="nodeID" class="resource-hint" data-testid="resource-refresh-status">{{ t(refreshStatus) }}</span>
      <el-button :loading="loading" @click="reload()">{{ t('common.refresh') }}</el-button>
    </header>
    <el-alert v-if="errorKey" :title="t(errorKey)" type="error" show-icon :closable="false" data-testid="resource-error" />
    <template v-if="!nodeID">
      <el-form class="resource-search" @submit.prevent="search">
        <el-input v-model="searchText" :placeholder="t('monitor.resources.search')" :aria-label="t('monitor.resources.search')" maxlength="200" clearable data-testid="resource-search" />
        <el-button native-type="submit">{{ t('common.search') }}</el-button>
      </el-form>
      <el-table :data="nodes" v-loading="loading" :empty-text="t('monitor.resources.empty')">
        <el-table-column prop="display_name" :label="t('monitor.resources.node')" min-width="160" show-overflow-tooltip />
        <el-table-column prop="node_id" :label="t('monitor.targets.node')" min-width="280" show-overflow-tooltip />
        <el-table-column :label="t('monitor.resources.addresses')" min-width="180"><template #default="{ row }">{{ row.addresses.join(', ') }}</template></el-table-column>
        <el-table-column :label="t('monitor.resources.nodeState')" width="120"><template #default="{ row }">{{ t(row.enabled ? 'common.enabled' : 'common.disabled') }}</template></el-table-column>
        <el-table-column width="150"><template #default="{ row }"><el-button text type="primary" @click="navigate(`/node-resources/${row.node_id}`, state.query)">{{ t('monitor.resources.view') }}</el-button></template></el-table-column>
      </el-table>
      <div class="resource-pagination"><el-pagination :current-page="state.page" :page-size="state.pageSize" :total="total" layout="total, prev, pager, next" @current-change="changePage" /></div>
    </template>
    <template v-else-if="node">
      <section class="node-header">
        <h3>{{ node.display_name }}</h3>
        <p>{{ node.node_id }} · {{ t(node.enabled ? 'common.enabled' : 'common.disabled') }}</p>
        <p v-if="instant">{{ t('monitor.resources.queriedAt') }}: {{ date(instant.queried_at) }}</p>
      </section>
      <p v-if="loading" role="status">{{ t('common.loading') }}</p>
      <section v-if="instant" class="resource-cards" :aria-label="t('monitor.resources.current')">
        <article v-for="metric in resourceMetrics" :key="metric.key" class="resource-card" :data-testid="`resource-${metric.name}`">
          <h4>{{ t(`monitor.resources.metrics.${metric.name}`) }}</h4>
          <strong>{{ formatValue(point(metric.key), metric) }}</strong>
          <p>{{ t(`monitor.resources.states.${point(metric.key).data_state}`) }}</p>
          <p>{{ t('monitor.resources.sampledAt') }}: {{ date(point(metric.key).sampled_at) }}</p>
          <p>{{ t('monitor.resources.evaluatedAt') }}: {{ date(point(metric.key).evaluated_at) }}</p>
        </article>
      </section>
      <section v-if="instant" class="resource-trend">
        <div class="resource-toolbar">
          <h3>{{ t('monitor.resources.trend') }}</h3>
          <el-select :model-value="state.metric" :aria-label="t('monitor.resources.metric')" data-testid="resource-metric" @update:model-value="value => selectTrend('metric', value)">
            <el-option v-for="metric in resourceMetrics" :key="metric.key" :value="metric.key" :label="t(`monitor.resources.metrics.${metric.name}`)" />
          </el-select>
          <el-select :model-value="state.range" :aria-label="t('monitor.resources.range')" data-testid="resource-range" @update:model-value="value => selectTrend('range', value)">
            <el-option v-for="(_, key) in resourceRanges" :key="key" :value="key" :label="t(`monitor.resources.ranges.${key}`)" />
          </el-select>
        </div>
        <el-alert v-if="trendErrorKey" :title="t(trendErrorKey)" type="error" show-icon :closable="false" data-testid="resource-trend-error" />
        <template v-if="trend">
          <p class="resource-hint">{{ t('monitor.resources.window', { start: date(trend.start), end: date(trend.end), step: trend.step_seconds }) }}</p>
          <p class="resource-hint">{{ t('monitor.resources.gaps') }}</p>
          <div class="resource-chart" role="img" :aria-label="t('monitor.resources.trend')" data-testid="resource-chart">
            <ChartRenderer :rows="chartRows" :config="chartConfig" @invalid="invalidChart" />
          </div>
          <el-collapse>
            <el-collapse-item :title="t('monitor.resources.evidence')" name="evidence">
              <el-table :data="evidenceRows" data-testid="resource-evidence">
                <el-table-column :label="t('monitor.resources.evaluatedAt')" min-width="190"><template #default="{ row }">{{ date(row.evaluated_at) }}</template></el-table-column>
                <el-table-column :label="t('monitor.resources.sampledAt')" min-width="190"><template #default="{ row }">{{ date(row.sampled_at) }}</template></el-table-column>
                <el-table-column :label="t('monitor.resources.value')" min-width="160"><template #default="{ row }">{{ formatValue(row, selectedMetric) }}</template></el-table-column>
                <el-table-column :label="t('monitor.resources.dataState')" min-width="120"><template #default="{ row }">{{ t(`monitor.resources.states.${row.data_state}`) }}</template></el-table-column>
              </el-table>
              <div class="resource-pagination"><el-pagination v-model:current-page="evidencePage" :page-size="20" :total="trend.series[0].points.length" layout="total, prev, pager, next" /></div>
            </el-collapse-item>
          </el-collapse>
        </template>
      </section>
    </template>
  </main>
  <el-alert v-else :title="t('monitor.resources.errors.denied')" type="error" :closable="false" show-icon />
</template>

<script setup>
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useConsolePageDescriptor } from '@common-ui'
import ChartRenderer from '../../../../common-frontend/chart/src/ChartRenderer.vue'
import { formatFieldPresentationValue } from '../../../../common-frontend/basic/src/utils/fieldPresentation.mjs'
import { useAuthStore } from '../store/auth'
import { nodeResourcesAPI as api } from '../api/nodeResources'
import { isTargetUUID } from '../utils/monitoringTargets'
import { currentResourceValue, resourceChartRows, resourceErrorKey, resourceMetrics, resourceRanges, resourceRefreshOptions, resolveResourceRoute, trendParameters, validateResourceResponse } from '../utils/nodeResources'
import { navigateMonitorRoute } from '../utils/moduleNavigation'

const { t, locale } = useI18n(), auth = useAuthStore(), route = useRoute(), router = useRouter()
const canRead = computed(() => auth.contextType === 'platform' && auth.authContext?.principal?.type === 'user' && !auth.authContext?.delegation && auth.hasPermission('monitor.resource_observation.read') && auth.hasPermission('platform.host_node.read'))
const identity = computed(() => JSON.stringify([auth.authContext?.principal, auth.authContext?.context, auth.authContext?.delegation, auth.permissions]))
const nodeID = computed(() => typeof route.params.node_id === 'string' ? route.params.node_id : '')
const state = computed(() => resolveResourceRoute(route.query, Boolean(nodeID.value)))
const listQuery = computed(() => { const { range, metric, refresh, ...query } = state.value.query; return query })
const nodes = ref([]), total = ref(0), node = ref(null), instant = ref(null), trend = ref(null)
const loading = ref(false), errorKey = ref(''), trendErrorKey = ref(''), searchText = ref(''), evidencePage = ref(1)
const selectedMetric = computed(() => resourceMetrics.find(item => item.key === state.value.metric))
const chartRows = computed(() => trend.value ? resourceChartRows(trend.value.series[0]) : [])
const presentation = metric => ({ field: 'value', label: t(`monitor.resources.metrics.${metric.name}`), unit: t(`monitor.resources.units.${metric.unit}`), precision: metric.precision })
const chartConfig = computed(() => ({ chart_type: 'line', dimension: 'evaluated_at', measures: ['value'], field_presentations: [{ field: 'evaluated_at', label: t('monitor.resources.evaluatedAt'), temporal_format: 'datetime' }, presentation(selectedMetric.value)] }))
const evidenceRows = computed(() => trend.value?.series[0].points.slice((evidencePage.value - 1) * 20, evidencePage.value * 20) || [])
const pageVisible = ref(!document.hidden), refreshBlocked = ref(false)
const refreshStatus = computed(() => state.value.refresh === 'off' ? 'monitor.resources.refreshManual' : refreshBlocked.value ? 'monitor.resources.refreshStopped' : !pageVisible.value ? 'monitor.resources.refreshPaused' : 'monitor.resources.refreshAutomatic')
let epoch = 0, refreshTimer = null, disposed = false
const pending = new Set()
useConsolePageDescriptor(router, 'monitor', { title: computed(() => t('monitor.resources.detailTitle')), subject: computed(() => node.value?.display_name || ''), ready: computed(() => Boolean(node.value)) })
function date(value) { return formatFieldPresentationValue(value, { temporal_format: 'datetime' }, locale.value) }
function point(key) { return instant.value.series.find(series => series.metric_key === key).points[0] }
function formatValue(value, metric) { return formatFieldPresentationValue(currentResourceValue(value), presentation(metric), locale.value) }
function navigate(path, query, history = 'push') { return navigateMonitorRoute(router, { path, query }, { history }) }
function changePage(page) { return navigate(route.path, { ...state.value.query, page: page === 1 ? undefined : String(page) }, 'replace') }
function search() { return navigate('/node-resources', { ...state.value.query, search: searchText.value.trim() || undefined, page: undefined }, 'replace') }
function selectTrend(key, value) { return navigate(route.path, { ...state.value.query, [key]: value }, 'replace') }
function invalidChart() { trend.value = null; trendErrorKey.value = 'monitor.resources.errors.invalidResponse' }
function stopRefresh() { window.clearTimeout(refreshTimer); refreshTimer = null }
function invalidate() { stopRefresh(); epoch++; for (const controller of pending) controller.abort(); pending.clear() }
async function request(work) {
  const controller = new AbortController(); pending.add(controller)
  try { return await work({ signal: controller.signal }) }
  finally { pending.delete(controller) }
}
function scheduleRefresh() {
  stopRefresh()
  if (disposed || !canRead.value || !pageVisible.value || !nodeID.value || !isTargetUUID(nodeID.value) || state.value.refresh === 'off' || refreshBlocked.value || state.value.changed || loading.value) return
  refreshTimer = window.setTimeout(() => { refreshTimer = null; void reload({ automatic: true }) }, Number(state.value.refresh) * 1000)
}
function visibilityChanged() {
  pageVisible.value = !document.hidden
  if (!pageVisible.value && nodeID.value) { invalidate(); loading.value = false }
  else if (nodeID.value && state.value.refresh !== 'off' && !refreshBlocked.value) void reload({ automatic: true })
}
async function reload({ automatic = false } = {}) {
  if (automatic && (loading.value || disposed || !canRead.value || !pageVisible.value || refreshBlocked.value)) return
  invalidate()
  if (!automatic) refreshBlocked.value = false
  const ticket = epoch
  if (!automatic) { nodes.value = []; total.value = 0; node.value = null; instant.value = null; trend.value = null }
  errorKey.value = ''; trendErrorKey.value = ''; evidencePage.value = 1; loading.value = false
  if (!canRead.value) return
  if (state.value.changed) { await navigate(route.path, state.value.query, 'replace'); return }
  searchText.value = state.value.search
  const id = nodeID.value
  if (id && !isTargetUUID(id)) { errorKey.value = 'monitor.resources.errors.invalidID'; return }
  loading.value = true
  try {
    if (!id) {
      const value = await request(config => api.nodes({ page: state.value.page, page_size: state.value.pageSize, ...(state.value.search ? { search: state.value.search } : {}) }, config))
      if (ticket !== epoch) return
      nodes.value = value.data; total.value = value.total
    } else {
      const value = await request(config => api.node(id, config))
      if (ticket !== epoch) return
      if (value.node_id !== id) throw new Error('invalid_resource_response')
      node.value = value
      const keys = resourceMetrics.map(metric => metric.key)
      const current = await request(config => api.instant({ node_id: id, metrics: keys.join(',') }, config))
      if (ticket !== epoch) return
      instant.value = validateResourceResponse(current, id, keys)
      trend.value = null
      try {
        const params = trendParameters(id, state.value.metric, state.value.range, current.end)
        const history = await request(config => api.trend(params, config))
        if (ticket !== epoch) return
        trend.value = validateResourceResponse(history, id, [state.value.metric], true)
      } catch (error) {
        if (ticket !== epoch) return
        trend.value = null
        trendErrorKey.value = resourceErrorKey(error)
        if ([401, 403, 404].includes(error.response?.status)) { refreshBlocked.value = true; node.value = null; instant.value = null; errorKey.value = trendErrorKey.value }
      }
    }
  } catch (error) {
    if (ticket !== epoch) return
    instant.value = null; trend.value = null
    errorKey.value = resourceErrorKey(error)
    if ([401, 403, 404].includes(error.response?.status)) { refreshBlocked.value = true; node.value = null }
  } finally { if (ticket === epoch) { loading.value = false; scheduleRefresh() } }
}
watch([() => route.fullPath, identity], reload, { immediate: true, flush: 'sync' })
onMounted(() => document.addEventListener('visibilitychange', visibilityChanged))
onBeforeUnmount(() => { disposed = true; document.removeEventListener('visibilitychange', visibilityChanged); invalidate() })
</script>

<style scoped>
.node-resources { padding: 24px; color: var(--addp-text-primary); min-width: 0; }
.resource-toolbar { display: flex; align-items: center; flex-wrap: wrap; gap: 12px; margin-bottom: 16px; }
.resource-toolbar h2, .resource-toolbar h3 { flex: 1; }
.resource-toolbar .el-select { width: 220px; max-width: 100%; }
.resource-search { display: flex; gap: 8px; margin: 16px 0; max-width: 520px; }
.resource-pagination { margin-top: 16px; overflow-x: auto; }
.node-header { margin: 16px 0; overflow-wrap: anywhere; }
.node-header p, .resource-card p, .resource-hint { color: var(--addp-text-secondary); margin-top: 8px; font-size: 13px; }
.resource-cards { display: grid; grid-template-columns: repeat(auto-fit, minmax(250px, 1fr)); gap: 16px; }
.resource-card { min-width: 0; border: 1px solid var(--addp-border-color); border-radius: 8px; padding: 16px; background: var(--addp-bg-primary); overflow-wrap: anywhere; }
.resource-card strong { display: block; margin-top: 12px; font-size: 22px; }
.resource-trend { margin-top: 24px; }
.resource-chart { display: flex; min-width: 0; }
.el-alert { margin: 16px 0; }
@media (max-width: 600px) { .node-resources { padding: 12px; } .resource-cards { grid-template-columns: 1fr; } }
</style>
