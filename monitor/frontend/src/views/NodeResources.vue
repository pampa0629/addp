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
      <el-alert v-if="instant && collectionMessage" :title="t(collectionMessage)" :type="instant.collection.state === 'failed' ? 'error' : 'warning'" show-icon :closable="false" data-testid="resource-collection-status" />
      <el-button v-if="canConfigure && instant" @click="configureCollection">{{ t('monitor.resources.configure') }}</el-button>
      <p v-if="loading" role="status">{{ t('common.loading') }}</p>
      <section v-if="instant" class="resource-cards" :aria-label="t('monitor.resources.current')">
        <article v-for="metric in resourceMetrics" :key="metric.key" class="resource-card" :data-testid="`resource-${metric.name}`">
          <h4>{{ t(`monitor.resources.metrics.${metric.name}`) }}</h4>
          <strong>{{ formatValue(point(metric.key), metric) }}</strong>
          <p v-if="metric.unit === 'load'" class="resource-hint">{{ t('monitor.resources.loadSummary') }}</p>
          <p>{{ t(`monitor.resources.states.${point(metric.key).data_state}`) }}</p>
          <el-collapse>
            <el-collapse-item :title="t('monitor.resources.sampling')" name="sampling">
              <p v-if="metric.windowSeconds" class="resource-hint">{{ t('monitor.resources.cpuBusyHint') }}</p>
              <p v-if="metric.unit === 'load'" class="resource-hint">{{ t('monitor.resources.loadHint') }}</p>
              <p>{{ t('monitor.resources.sampledAt') }}: {{ date(point(metric.key).sampled_at) }}</p>
              <p>{{ t('monitor.resources.evaluatedAt') }}: {{ date(point(metric.key).evaluated_at) }}</p>
            </el-collapse-item>
          </el-collapse>
        </article>
      </section>
      <section v-if="instant" class="resource-filesystems" data-testid="resource-filesystems">
        <h3>{{ t('monitor.resources.filesystem.title') }}</h3>
        <p class="resource-hint">{{ t('monitor.resources.filesystem.hint') }}</p>
        <p v-if="filesystems" class="resource-hint">{{ t('monitor.resources.queriedAt') }}: {{ date(filesystems.queried_at) }}</p>
        <el-alert v-if="filesystemErrorKey" :title="t(filesystemErrorKey)" type="error" show-icon :closable="false" data-testid="resource-filesystem-error" />
        <el-alert v-if="inodeErrorKey" :title="`${t('monitor.resources.filesystem.inode')}: ${t(inodeErrorKey)}`" type="error" show-icon :closable="false" data-testid="resource-inode-error" />
        <p v-if="inodes" class="resource-hint">{{ t('monitor.resources.filesystem.inode') }} · {{ t('monitor.resources.queriedAt') }}: {{ date(inodes.queried_at) }}</p>
        <el-alert v-if="instant.collection.filesystem === 'not_collected' || instant.collection.filesystem === 'failed'" :title="t(instant.collection.filesystem === 'not_collected' ? 'monitor.resources.collection.not_collected' : 'monitor.resources.collection.filesystem_failed')" type="info" :closable="false" data-testid="resource-filesystem-capability" />
        <el-table :data="mountRows" :empty-text="filesystems ? t(`monitor.resources.states.${filesystems.series[0].points[0].data_state}`) : t('monitor.resources.filesystem.empty')" data-testid="resource-filesystem-table">
          <el-table-column prop="dimensions.device" :label="t('monitor.resources.filesystem.device')" min-width="160" show-overflow-tooltip />
          <el-table-column prop="dimensions.mountpoint" :label="t('monitor.resources.filesystem.mountpoint')" min-width="180" show-overflow-tooltip />
          <el-table-column prop="dimensions.fstype" :label="t('monitor.resources.filesystem.type')" min-width="100" show-overflow-tooltip />
          <el-table-column v-for="metric in mountMetrics" :key="metric.key" :label="t(`monitor.resources.metrics.${metric.name}`)" min-width="170">
            <template #default="{ row }"><strong>{{ formatValue(row.metrics[metric.key], metric) }}</strong><p v-if="row.metrics[metric.key]" class="resource-hint">{{ t(`monitor.resources.states.${row.metrics[metric.key].data_state}`) }}</p><p v-if="row.metrics[metric.key]" class="resource-hint">{{ date(row.metrics[metric.key].sampled_at) }}</p></template>
          </el-table-column>
          <el-table-column :label="t('monitor.resources.trend')" width="140" fixed="right"><template #default="{ row }"><el-button text type="primary" @click="selectFilesystem(row)">{{ t('monitor.resources.filesystem.viewTrend') }}</el-button></template></el-table-column>
        </el-table>
      </section>
      <template v-if="instant">
        <section v-for="family in deviceSections" :key="family.name" class="resource-filesystems" :data-testid="`resource-${family.name}s`">
          <h3>{{ t(`monitor.resources.${family.name}.title`) }}</h3>
          <p v-if="family.name === 'network'" class="resource-hint">{{ t('monitor.resources.network.hint') }}</p>
          <p v-if="family.name === 'disk' && family.rows.length" class="resource-hint">{{ t('monitor.resources.disk.orderHint') }}</p>
          <el-alert v-if="family.errorKey" :title="t(family.errorKey)" type="error" :closable="false" :data-testid="`resource-${family.name}-error`" />
          <el-table v-if="!family.errorKey" :data="family.rows" row-key="key" :empty-text="t(`monitor.resources.${family.name}.empty`)" :data-testid="`resource-${family.name}-table`">
            <el-table-column prop="dimensions.device" :label="t(family.name === 'network' ? 'monitor.resources.network.interface' : 'monitor.resources.filesystem.device')" min-width="160" show-overflow-tooltip />
            <el-table-column v-for="metric in family.metrics" :key="metric.key" :label="t(`monitor.resources.metrics.${metric.name}`)" min-width="170">
              <template #header>
                <div class="resource-metric-heading">
                  <span>{{ t(`monitor.resources.metrics.${metric.name}`) }}</span>
                  <el-popover v-if="family.name === 'disk'" trigger="click" placement="top" :width="280" :content="t(`monitor.resources.disk.columns.${metric.name}.detail`)">
                    <template #reference>
                      <el-button text circle size="small" :aria-label="t('monitor.resources.metricHelp', { metric: t(`monitor.resources.metrics.${metric.name}`) })"><el-icon><InfoFilled /></el-icon></el-button>
                    </template>
                  </el-popover>
                </div>
                <p v-if="family.name === 'disk'" class="resource-hint resource-column-summary">{{ t(`monitor.resources.disk.columns.${metric.name}.summary`) }}</p>
              </template>
              <template #default="{ row }"><strong>{{ formatValue(row.metrics[metric.key], metric) }}</strong><p v-if="row.metrics[metric.key]" class="resource-hint">{{ t(`monitor.resources.states.${row.metrics[metric.key].data_state}`) }}</p></template>
            </el-table-column>
            <el-table-column :label="t('monitor.resources.trend')" width="140"><template #default="{ row }"><el-button text type="primary" @click="selectDeviceObservation(row, family.metrics[0].key)">{{ t('monitor.resources.filesystem.viewTrend') }}</el-button></template></el-table-column>
          </el-table>
        </section>
      </template>
      <section v-if="instant" class="resource-trend">
        <div class="resource-toolbar">
          <h3>{{ t('monitor.resources.trend') }}</h3>
          <span v-if="state.dimensions.mountpoint" class="resource-hint" data-testid="resource-selected-mount">{{ state.dimensions.mountpoint }} · {{ state.dimensions.fstype }} · {{ state.dimensions.device }}</span>
          <span v-if="state.dimensions.device && !state.dimensions.mountpoint" class="resource-hint" data-testid="resource-selected-device">{{ state.dimensions.device }}</span>
          <el-select :model-value="state.metric" :aria-label="t('monitor.resources.metric')" data-testid="resource-metric" @update:model-value="value => selectTrend('metric', value)">
            <el-option v-for="metric in trendMetrics" :key="metric.key" :value="metric.key" :label="t(`monitor.resources.metrics.${metric.name}`)" />
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
import { InfoFilled } from '@element-plus/icons-vue'
import { useConsolePageDescriptor } from '@common-ui'
import ChartRenderer from '../../../../common-frontend/chart/src/ChartRenderer.vue'
import { formatBytes, scaleByteValue, formatDurationSeconds } from '../../../../common-frontend/basic/src/utils/formatters.js'
import { formatFieldPresentationValue } from '../../../../common-frontend/basic/src/utils/fieldPresentation.mjs'
import { useAuthStore } from '../store/auth'
import { nodeResourcesAPI as api } from '../api/nodeResources'
import { isTargetUUID } from '../utils/monitoringTargets'
import { validateCollection, currentResourceValue, resourceChartRows, resourceErrorKey, resourceMetrics, filesystemMetrics, inodeMetrics, mountMetrics, diskMetrics, networkMetrics, allResourceMetrics, resourceDimensionRows, sortDiskResourceRows, resourceRanges, resourceRefreshOptions, resolveResourceRoute, trendParameters, validateResourceResponse } from '../utils/nodeResources'
import { navigateMonitorRoute } from '../utils/moduleNavigation'

const { t, locale } = useI18n(), auth = useAuthStore(), route = useRoute(), router = useRouter()
const canRead = computed(() => auth.contextType === 'platform' && auth.authContext?.principal?.type === 'user' && !auth.authContext?.delegation && auth.hasPermission('monitor.resource_observation.read') && auth.hasPermission('platform.host_node.read'))
const canConfigure = computed(() => canRead.value && auth.hasPermission('monitor.monitoring_target.read') && (instant.value?.target_id || auth.hasPermission('monitor.monitoring_target.create')))
const collectionMessage = computed(() => !instant.value ? '' : instant.value.collection.state !== 'collecting' ? `monitor.resources.collection.${instant.value.collection.state}` : instant.value.series.some(series => series.points[0].data_state === 'no_data') ? 'monitor.resources.collection.waiting' : '')
const identity = computed(() => JSON.stringify([auth.authContext?.principal, auth.authContext?.context, auth.authContext?.delegation, auth.permissions]))
const nodeID = computed(() => typeof route.params.node_id === 'string' ? route.params.node_id : '')
const state = computed(() => resolveResourceRoute(route.query, Boolean(nodeID.value)))
const listQuery = computed(() => { const { range, metric, refresh, device, mountpoint, fstype, ...query } = state.value.query; return query })
const nodes = ref([]), total = ref(0), node = ref(null), instant = ref(null), trend = ref(null), filesystems = ref(null), inodes = ref(null), disks = ref(null), networks = ref(null)
const loading = ref(false), errorKey = ref(''), trendErrorKey = ref(''), filesystemErrorKey = ref(''), inodeErrorKey = ref(''), diskErrorKey = ref(''), networkErrorKey = ref(''), searchText = ref(''), evidencePage = ref(1)
const selectedMetric = computed(() => allResourceMetrics.find(item => item.key === state.value.metric))
const mountRows = computed(() => resourceDimensionRows(filesystems.value, inodes.value))
const deviceFamilies = [{ name: 'disk', metrics: diskMetrics, value: disks, error: diskErrorKey }, { name: 'network', metrics: networkMetrics, value: networks, error: networkErrorKey }]
const deviceSections = computed(() => deviceFamilies.map(family => {
  const rows = resourceDimensionRows(family.value.value)
  return { name: family.name, metrics: family.metrics, rows: family.name === 'disk' ? sortDiskResourceRows(rows) : rows, errorKey: family.error.value }
}))
const trendMetrics = computed(() => [...resourceMetrics, ...(state.value.dimensions.mountpoint ? mountMetrics : state.value.dimensions.device ? state.value.metric.startsWith('node.network.') ? networkMetrics : diskMetrics : [])])
const chartScale = computed(() => ['bytes', 'bytes_per_second'].includes(selectedMetric.value.unit) ? scaleByteValue(Math.max(0, ...(trend.value?.series[0].points.map(point => currentResourceValue(point) ?? 0) || []))) : null)
const chartRows = computed(() => trend.value ? resourceChartRows(trend.value.series[0]).map(row => ({ ...row, value: row.value === null ? null : row.value / (chartScale.value?.divisor || 1) })) : [])
const presentation = metric => ({ field: 'value', label: t(`monitor.resources.metrics.${metric.name}`), unit: t(`monitor.resources.units.${metric.unit}`), precision: metric.precision })
const chartConfig = computed(() => ({ chart_type: 'line', dimension: 'evaluated_at', measures: ['value'], field_presentations: [{ field: 'evaluated_at', label: t('monitor.resources.evaluatedAt'), temporal_format: 'datetime' }, { ...presentation(selectedMetric.value), ...(chartScale.value ? { unit: chartScale.value.unit + (selectedMetric.value.unit === 'bytes_per_second' ? '/s' : ''), precision: 2 } : {}) }] }))
const evidenceRows = computed(() => trend.value?.series[0].points.slice((evidencePage.value - 1) * 20, evidencePage.value * 20) || [])
const pageVisible = ref(!document.hidden), refreshBlocked = ref(false)
const refreshStatus = computed(() => state.value.refresh === 'off' ? 'monitor.resources.refreshManual' : refreshBlocked.value ? 'monitor.resources.refreshStopped' : !pageVisible.value ? 'monitor.resources.refreshPaused' : 'monitor.resources.refreshAutomatic')
let epoch = 0, refreshTimer = null, disposed = false
const pending = new Set()
useConsolePageDescriptor(router, 'monitor', { title: computed(() => t('monitor.resources.detailTitle')), subject: computed(() => node.value?.display_name || ''), ready: computed(() => Boolean(node.value)) })
function date(value) { return formatFieldPresentationValue(value, { temporal_format: 'datetime' }, locale.value) }
function point(key) { return instant.value.series.find(series => series.metric_key === key).points[0] }
function formatValue(value, metric) {
  const numeric = currentResourceValue(value)
  if (numeric === null) return '—'
  if (['bytes', 'bytes_per_second'].includes(metric.unit)) return formatBytes(numeric, 2, locale.value) + (metric.unit === 'bytes_per_second' ? '/s' : '')
  if (metric.unit === 'seconds') return formatDurationSeconds(numeric, locale.value)
  const formatted = formatFieldPresentationValue(numeric, presentation(metric), locale.value)
  return metric.unit === 'load' ? t('monitor.resources.loadValue', { value: formatted }) : formatted
}
function navigate(path, query, history = 'push') { return navigateMonitorRoute(router, { path, query }, { history }) }
function configureCollection() {
  return instant.value.target_id ? navigate(`/monitoring-targets/${instant.value.target_id}`, {}) : navigate('/monitoring-targets/new', { node_id: nodeID.value })
}
function changePage(page) { return navigate(route.path, { ...state.value.query, page: page === 1 ? undefined : String(page) }, 'replace') }
function search() { return navigate('/node-resources', { ...state.value.query, search: searchText.value.trim() || undefined, page: undefined }, 'replace') }
function selectTrend(key, value) {
  const query = { ...state.value.query, [key]: value }
  if (key === 'metric' && !value.startsWith('node.filesystem.') && !value.startsWith('node.disk.') && !value.startsWith('node.network.')) for (const dimension of ['device', 'mountpoint', 'fstype']) delete query[dimension]
  return navigate(route.path, query, 'replace')
}
function selectFilesystem(row) { return navigate(route.path, { ...state.value.query, ...row.dimensions, metric: 'node.filesystem.used_percent' }, 'replace') }
function selectDeviceObservation(row, metric) { const { mountpoint, fstype, ...query } = state.value.query; return navigate(route.path, { ...query, ...row.dimensions, metric }, 'replace') }
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
  if (!automatic) { nodes.value = []; total.value = 0; node.value = null; instant.value = null; trend.value = null; filesystems.value = null; inodes.value = null; disks.value = null; networks.value = null }
  errorKey.value = ''; trendErrorKey.value = ''; filesystemErrorKey.value = ''; inodeErrorKey.value = ''; diskErrorKey.value = ''; networkErrorKey.value = '';  evidencePage.value = 1; loading.value = false
  if (!canRead.value) return
  if (state.value.changed) { await navigate(route.path, state.value.query, 'replace'); return }
  searchText.value = state.value.search
  const id = nodeID.value
  if (state.value.invalidDimensions) { errorKey.value = 'monitor.resources.errors.invalidDimensions'; return }
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
      validateCollection(current.collection, current.queried_at)
      instant.value = validateResourceResponse(current, id, keys)
      filesystems.value = null; inodes.value = null; disks.value = null; networks.value = null
      try {
        const filesystemKeys = filesystemMetrics.map(metric => metric.key)
        const mounts = await request(config => api.instant({ node_id: id, metrics: filesystemKeys.join(',') }, config))
        if (ticket !== epoch) return
        filesystems.value = validateResourceResponse(mounts, id, filesystemKeys)
      } catch (error) {
        if (ticket !== epoch) return
        filesystemErrorKey.value = resourceErrorKey(error)
        if ([401, 403, 404].includes(error.response?.status)) throw error
      }
      inodes.value = null; disks.value = null; networks.value = null
      try {
        const inodeKeys = inodeMetrics.map(metric => metric.key)
        const observed = await request(config => api.instant({ node_id: id, metrics: inodeKeys.join(',') }, config))
        if (ticket !== epoch) return
        inodes.value = validateResourceResponse(observed, id, inodeKeys)
      } catch (error) {
        if (ticket !== epoch) return
        inodeErrorKey.value = resourceErrorKey(error)
        if ([401, 403, 404].includes(error.response?.status)) throw error
      }
      for (const family of deviceFamilies) {
        try {
          const keys = family.metrics.map(metric => metric.key)
          const observed = await request(config => api.instant({ node_id: id, metrics: keys.join(',') }, config))
          if (ticket !== epoch) return
          family.value.value = validateResourceResponse(observed, id, keys)
        } catch (error) {
          if (ticket !== epoch) return
          family.error.value = resourceErrorKey(error)
          if ([401, 403, 404].includes(error.response?.status)) throw error
        }
      }
      trend.value = null
      try {
        const params = trendParameters(id, state.value.metric, state.value.range, current.end, state.value.dimensions)
        const history = await request(config => api.trend(params, config))
        if (ticket !== epoch) return
        trend.value = validateResourceResponse(history, id, [state.value.metric], true, state.value.dimensions)
      } catch (error) {
        if (ticket !== epoch) return
        trend.value = null
        trendErrorKey.value = resourceErrorKey(error)
        if ([401, 403, 404].includes(error.response?.status)) { refreshBlocked.value = true; node.value = null; instant.value = null; filesystems.value = null; inodes.value = null; disks.value = null; networks.value = null; errorKey.value = trendErrorKey.value }
      }
    }
  } catch (error) {
    if (ticket !== epoch) return
    instant.value = null; trend.value = null; filesystems.value = null; inodes.value = null; disks.value = null; networks.value = null
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
.resource-trend, .resource-filesystems { margin-top: 24px; }
.resource-metric-heading { display: flex; align-items: center; gap: 4px; }
.resource-metric-heading .el-button { flex-shrink: 0; }
.resource-column-summary { font-weight: 400; line-height: 1.4; margin: 4px 0 0; }
.resource-chart { display: flex; min-width: 0; }
.el-alert { margin: 16px 0; }
@media (max-width: 600px) { .node-resources { padding: 12px; } .resource-cards { grid-template-columns: 1fr; } }
</style>
