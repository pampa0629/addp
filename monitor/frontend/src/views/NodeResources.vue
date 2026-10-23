<template>
  <main v-if="canRead" class="node-resources" data-testid="node-resources">
    <header class="resource-toolbar">
      <h2>{{ t('monitor.resources.title') }}</h2>
      <el-button v-if="nodeID" @click="navigate('/node-resources', listQuery)">{{ t('monitor.resources.back') }}</el-button>
      <AutoRefreshControl v-if="!state.end" :model-value="state.refresh" :status="refreshStatus" test-id="resource-refresh" @update:model-value="value => selectTrend('refresh', value)" />
      <el-button :loading="loading" @click="reload()">{{ t('common.refresh') }}</el-button>
    </header>
    <el-alert v-if="errorKey" :title="t(errorKey)" type="error" show-icon :closable="false" data-testid="resource-error" />
    <template v-if="!nodeID">
      <el-form class="resource-search" @submit.prevent="search">
        <el-input v-model="searchText" :placeholder="t('monitor.resources.search')" :aria-label="t('monitor.resources.search')" maxlength="200" clearable data-testid="resource-search" />
        <el-button native-type="submit">{{ t('common.search') }}</el-button>
      </el-form>
      <el-alert v-if="summaryErrorKey" :title="t(summaryErrorKey)" type="warning" show-icon :closable="false" data-testid="resource-summary-error" />
      <el-table :data="nodes" v-loading="loading" :empty-text="t('monitor.resources.empty')" data-testid="resource-node-list">
        <el-table-column :label="t('monitor.resources.node')" min-width="180" show-overflow-tooltip><template #default="{ row }"><span :title="row.node_id">{{ row.display_name }}</span></template></el-table-column>
        <el-table-column :label="t('monitor.resources.addresses')" min-width="180"><template #default="{ row }">{{ row.addresses.join(', ') }}</template></el-table-column>
        <el-table-column v-for="metric in summaryMetrics" :key="metric.key" :label="t(`monitor.resources.metrics.${metric.name}`)" min-width="160">
          <template #default="{ row }"><strong>{{ formatValue(summaryPoint(row, metric), metric) }}</strong><p v-if="summaryPoint(row, metric) && summaryPoint(row, metric).data_state !== 'valid'" class="resource-hint">{{ t(`monitor.resources.states.${summaryPoint(row, metric).data_state}`) }}</p></template>
        </el-table-column>
        <el-table-column :label="t('monitor.resources.overview.collectionTitle')" min-width="150"><template #default="{ row }"><span :title="summaries.get(row.node_id) ? t('monitor.resources.sampledAt') + ': ' + date(summaries.get(row.node_id).collection.sampled_at) : ''">{{ t(!row.enabled ? 'monitor.resources.overview.disabled' : summaries.get(row.node_id) ? `monitor.resources.overview.collection.${summaries.get(row.node_id).collection.state}` : loading ? 'common.loading' : 'monitor.resources.overview.unavailable') }}</span></template></el-table-column>
        <el-table-column :label="t('monitor.resources.nodeState')" width="120"><template #default="{ row }">{{ t(row.enabled ? 'common.enabled' : 'common.disabled') }}</template></el-table-column>
        <el-table-column width="150"><template #default="{ row }"><el-button text type="primary" @click="navigate(`/node-resources/${row.node_id}`, state.query)">{{ t('monitor.resources.view') }}</el-button></template></el-table-column>
      </el-table>
      <div class="resource-pagination"><el-pagination :current-page="state.page" :page-size="state.pageSize" :page-sizes="[10, 20, 50]" :total="total" layout="total, sizes, prev, pager, next" @current-change="changePage" @size-change="changeSize" /></div>
    </template>
    <template v-else-if="node">
      <section class="node-header">
        <h3>{{ node.display_name }}</h3>
        <p>{{ node.node_id }} · {{ t(node.enabled ? 'common.enabled' : 'common.disabled') }}</p>
        <p v-if="instant">{{ t('monitor.resources.queriedAt') }}: {{ date(instant.queried_at) }}</p>
      </section>
      <el-alert v-if="instant && collectionMessage" :title="t(collectionMessage)" :type="instant.collection.state === 'failed' ? 'error' : 'warning'" show-icon :closable="false" data-testid="resource-collection-status" />
      <el-button v-if="canConfigure && instant" @click="configureCollection">{{ t('monitor.resources.configure') }}</el-button>
      <el-button v-if="node && canReadAlerts" @click="openRelatedAlerts" data-testid="host-related-alerts">{{ t('monitor.platformAlerts.related') }}</el-button>
      <p v-if="loading" role="status">{{ t('common.loading') }}</p>
      <h3 v-if="instant && state.end">{{ t('monitor.resources.current') }}</h3>
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
        <p v-if="mountRows.length" class="resource-hint">{{ t('monitor.resources.filesystem.hint') }}</p>
        <p v-if="mountRows.length && filesystems" class="resource-hint">{{ t('monitor.resources.queriedAt') }}: {{ date(filesystems.queried_at) }}</p>
        <el-alert v-if="filesystemErrorKey" :title="t(filesystemErrorKey)" type="error" show-icon :closable="false" data-testid="resource-filesystem-error" />
        <el-alert v-if="inodeErrorKey" :title="`${t('monitor.resources.filesystem.inode')}: ${t(inodeErrorKey)}`" type="error" show-icon :closable="false" data-testid="resource-inode-error" />
        <p v-if="mountRows.length && inodes" class="resource-hint">{{ t('monitor.resources.filesystem.inode') }} · {{ t('monitor.resources.queriedAt') }}: {{ date(inodes.queried_at) }}</p>
        <div class="resource-empty-status" data-testid="resource-filesystem-status">
          <el-alert v-if="!loading && filesystemMissingMessage && ['not_collected', 'failed'].includes(filesystems?.collection.filesystem)" :title="t(filesystemMissingMessage)" type="info" :closable="false" data-testid="resource-filesystem-capability" />
          <p v-else-if="!loading && filesystemMissingMessage" class="resource-hint" role="status" data-testid="resource-filesystem-empty">{{ t(filesystemMissingMessage) }}</p>
          <el-button v-if="!loading && !mountRows.length && !filesystemErrorKey && !inodeErrorKey && canConfigure" text type="primary" @click="configureCollection">{{ t(instant.target_id ? 'monitor.resources.viewCollection' : 'monitor.resources.configure') }}</el-button>
        </div>
        <el-table v-if="mountRows.length" :data="mountRows" data-testid="resource-filesystem-table">
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
          <p v-if="family.name === 'network' && family.rows.length" class="resource-hint">{{ t('monitor.resources.network.hint') }}</p>
          <p v-if="family.name === 'disk' && family.rows.length" class="resource-hint">{{ t('monitor.resources.disk.orderHint') }}</p>
          <div v-if="family.name === 'disk' && family.rows.length" class="resource-toolbar" data-testid="resource-disk-display">
            <span class="resource-hint" role="status">{{ t('monitor.resources.disk.deviceCount', { shown: family.visibleRows.length, total: family.rows.length }) }}</span>
            <el-button v-if="family.collapsedCount" :aria-expanded="showAllDisks" data-testid="resource-disk-toggle" @click="showAllDisks = !showAllDisks">{{ t(showAllDisks ? 'monitor.resources.disk.collapseIdle' : 'monitor.resources.disk.showAll', { total: family.rows.length }) }}</el-button>
          </div>
          <p v-if="family.name === 'disk' && family.rows.length && !family.visibleRows.length" class="resource-hint" role="status" data-testid="resource-disk-idle">{{ t('monitor.resources.disk.allIdle') }}</p>
          <el-alert v-if="family.errorKey" :title="t(family.errorKey)" type="error" :closable="false" :data-testid="`resource-${family.name}-error`" />
          <div v-if="!loading && !family.errorKey && family.missingMessage" class="resource-empty-status" :data-testid="`resource-${family.name}-status`">
            <p class="resource-hint" role="status" :data-testid="`resource-${family.name}-empty`">{{ t(family.missingMessage) }}</p>
            <el-button v-if="!family.rows.length && canConfigure" text type="primary" @click="configureCollection">{{ t(instant.target_id ? 'monitor.resources.viewCollection' : 'monitor.resources.configure') }}</el-button>
          </div>
          <el-table v-if="!family.errorKey && family.visibleRows.length" :data="family.visibleRows" row-key="key" :data-testid="`resource-${family.name}-table`">
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
      <section v-if="instant" ref="trendSection" tabindex="-1" class="resource-trend">
        <div class="resource-toolbar">
          <h3>{{ t('monitor.resources.trend') }}</h3>
          <el-button v-if="state.end" @click="resumeLiveTrend" data-testid="resource-live-trend">{{ t('monitor.resources.history.live') }}</el-button>
          <span v-if="state.dimensions.mountpoint" class="resource-hint" data-testid="resource-selected-mount">{{ state.dimensions.mountpoint }} · {{ state.dimensions.fstype }} · {{ state.dimensions.device }}</span>
          <span v-if="state.dimensions.device && !state.dimensions.mountpoint" class="resource-hint" data-testid="resource-selected-device">{{ state.dimensions.device }}</span>
          <el-select :model-value="state.metric" :aria-label="t('monitor.resources.metric')" data-testid="resource-metric" @update:model-value="value => selectTrend('metric', value)">
            <el-option v-for="metric in trendMetrics" :key="metric.key" :value="metric.key" :label="t(`monitor.resources.metrics.${metric.name}`)" />
          </el-select>
          <el-select :model-value="state.range" :aria-label="t('monitor.resources.range')" data-testid="resource-range" @update:model-value="value => selectTrend('range', value)">
            <el-option v-for="(_, key) in resourceRanges" :key="key" :value="key" :label="t(`monitor.resources.ranges.${key}`)" />
          </el-select>
        </div>
        <div v-if="summaryMetrics.some(metric => metric.key === state.metric)" class="resource-compare-control">
          <el-checkbox :model-value="state.compare" :disabled="!supportsResourceComparison(state.metric, state.range)" data-testid="resource-compare" @change="value => selectTrend('compare', value ? 'previous' : undefined)">{{ t('monitor.resources.comparison.enable') }}</el-checkbox>
          <p v-if="state.range === '7d'" class="resource-hint" data-testid="resource-compare-retention">{{ t('monitor.resources.comparison.retention') }}</p>
        </div>
        <p v-if="state.end" class="resource-hint" data-testid="resource-history">{{ t('monitor.resources.history.fixed', { time: date(state.end) }) }}</p>
        <el-alert v-if="trendErrorKey" :title="t(trendErrorKey)" type="error" show-icon :closable="false" data-testid="resource-trend-error" />
        <section v-if="comparison" class="resource-comparison" data-testid="resource-comparison" :aria-label="t('monitor.resources.comparison.title')">
          <h4>{{ t(`monitor.resources.comparison.averages.${selectedMetric.name}`) }}</h4>
          <div class="resource-cards">
            <article v-for="period in ['current', 'previous']" :key="period" class="resource-card" :data-testid="`resource-compare-${period}`">
              <h4>{{ t(`monitor.resources.comparison.${period}`) }}</h4>
              <strong>{{ formatValue({ data_state: 'valid', value: comparison[period].mean }, selectedMetric) }}</strong>
              <p>{{ date(comparison[period].start) }} — {{ date(comparison[period].end) }}</p>
              <p>{{ t('monitor.resources.comparison.coverage', { valid: comparison[period].valid, total: comparison[period].total }) }}</p>
            </article>
          </div>
          <p v-if="!comparison.complete" role="status" data-testid="resource-compare-incomplete">{{ t('monitor.resources.comparison.incomplete') }}</p>
          <p v-else role="status" data-testid="resource-compare-change"><strong>{{ comparisonChange }}</strong></p>
          <p class="resource-hint">{{ t('monitor.resources.comparison.method', { step: comparison.stepSeconds }) }}</p>
          <p v-if="selectedMetric.windowSeconds" class="resource-hint">{{ t('monitor.resources.comparison.cpuWindow') }}</p>
        </section>
        <template v-if="trend">
          <p class="resource-hint" data-testid="resource-trend-window">{{ t('monitor.resources.window', { start: date(comparison?.current.start || trend.start), end: date(trend.end), step: trend.step_seconds }) }}</p>
          <p class="resource-hint">{{ t('monitor.resources.gaps') }}</p>
          <template v-if="chartMetrics.length > 1">
            <p class="resource-hint" data-testid="resource-paired-hint">{{ memoryTrend ? t('monitor.resources.memoryAxes', { unit: chartScale.unit }) : t(networkTrend ? 'monitor.resources.networkBars' : 'monitor.resources.pairedThroughput', { unit: chartScale.unit + '/s' }) }}</p>
            <div class="resource-toolbar" data-testid="resource-paired-values">
              <p v-for="metric in chartMetrics" :key="metric.key" :data-testid="`resource-trend-${metric.name}`" :title="date(trendPoint(metric.key)?.evaluated_at)">{{ t(`monitor.resources.metrics.${metric.name}`) }}: <strong>{{ formatValue(trendPoint(metric.key), metric) }}</strong></p>
            </div>
          </template>
          <p class="resource-hint" data-testid="resource-zoom-hint">{{ t('monitor.resources.zoomHint') }}</p>
          <div class="resource-chart" role="img" :aria-label="t('monitor.resources.trend')" data-testid="resource-chart">
            <ChartRenderer :rows="chartRows" :config="chartConfig" :value-axes="chartAxes" :max-rows="networkTrend ? 1000 : undefined" :show-value-labels="!networkTrend" zoomable :initial-zoom="trendZoom" @zoom-change="trendZoom = $event" @invalid="invalidChart" />
          </div>
          <el-collapse>
            <el-collapse-item :title="t('monitor.resources.evidence')" name="evidence">
              <el-table :data="evidenceRows" data-testid="resource-evidence">
                <el-table-column :label="t('monitor.resources.evaluatedAt')" min-width="190"><template #default="{ row }">{{ date(row.evaluated_at) }}</template></el-table-column>
                <el-table-column :label="t('monitor.resources.sampledAt')" min-width="190"><template #default="{ row }">{{ date(row.sampled_at) }}</template></el-table-column>
                <el-table-column :label="t('monitor.resources.value')" min-width="160"><template #default="{ row }">{{ formatValue(row, selectedMetric) }}</template></el-table-column>
                <el-table-column :label="t('monitor.resources.dataState')" min-width="120"><template #default="{ row }">{{ t(`monitor.resources.states.${row.data_state}`) }}</template></el-table-column>
              </el-table>
              <div class="resource-pagination"><el-pagination v-model:current-page="evidencePage" :page-size="20" :total="trendPoints.length" layout="total, prev, pager, next" /></div>
            </el-collapse-item>
          </el-collapse>
        </template>
      </section>
    </template>
  </main>
  <el-alert v-else :title="t('monitor.resources.errors.denied')" type="error" :closable="false" show-icon />
</template>

<script setup>
import AutoRefreshControl from '../../../../common-frontend/basic/src/components/AutoRefreshControl.vue'
import { createAutoRefresh } from '../../../../common-frontend/basic/src/utils/autoRefresh.mjs'
import { createLatestRequestScope } from '../../../../common-frontend/basic/src/utils/latestRequestScope.mjs'
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
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
import { summaryMetrics, supportsResourceComparison, resourceTrendComparison, validateResourceSummaries, validateCollection, resourceMissingMessage, currentResourceValue, resourceTrendMetrics, resourceTrendChartRows, resourceErrorKey, resourceMetrics, filesystemMetrics, inodeMetrics, mountMetrics, diskMetrics, networkMetrics, allResourceMetrics, resourceDimensionRows, sortDiskResourceRows, visibleDiskResourceRows, resourceRanges, resolveResourceRoute, trendParameters, validateResourceResponse, validateResourceTrendWindow } from '../utils/nodeResources'
import { navigateMonitorRoute } from '../utils/moduleNavigation'

const { t, locale } = useI18n(), auth = useAuthStore(), route = useRoute(), router = useRouter()
const canRead = computed(() => auth.contextType === 'platform' && auth.authContext?.principal?.type === 'user' && !auth.authContext?.delegation && auth.hasPermission('monitor.resource_observation.read') && auth.hasPermission('platform.host_node.read'))
const canReadAlerts = computed(() => canRead.value && auth.hasPermission('monitor.platform_alert.read'))
const canConfigure = computed(() => canRead.value && auth.hasPermission('monitor.monitoring_target.read') && (instant.value?.target_id || auth.hasPermission('monitor.monitoring_target.create')))
const collectionMessage = computed(() => !instant.value ? '' : instant.value.collection.state !== 'collecting' ? `monitor.resources.collection.${instant.value.collection.state}` : instant.value.series.some(series => series.points[0].data_state === 'no_data') ? 'monitor.resources.collection.waiting' : '')
const identity = computed(() => JSON.stringify([auth.authContext?.principal, auth.authContext?.context, auth.authContext?.delegation, auth.permissions]))
const nodeID = computed(() => typeof route.params.node_id === 'string' ? route.params.node_id : '')
const state = computed(() => resolveResourceRoute(route.query, Boolean(nodeID.value)))
const listQuery = computed(() => { const { range, metric, compare, end, device, mountpoint, fstype, ...query } = state.value.query; return query })
const nodes = ref([]), total = ref(0), node = ref(null), instant = ref(null), trend = ref(null), filesystems = ref(null), inodes = ref(null), disks = ref(null), networks = ref(null)
const loading = ref(false), errorKey = ref(''), trendErrorKey = ref(''), filesystemErrorKey = ref(''), inodeErrorKey = ref(''), diskErrorKey = ref(''), networkErrorKey = ref(''), searchText = ref(''), evidencePage = ref(1)
const showAllDisks = ref(false), trendSection = ref(null)
const summaries = ref(new Map()), summaryErrorKey = ref('')
const comparison = ref(null)
const trendZoom = ref(null)
const selectedMetric = computed(() => allResourceMetrics.find(item => item.key === state.value.metric))
const mountRows = computed(() => resourceDimensionRows(filesystems.value, inodes.value))
const filesystemMissingMessage = computed(() => !filesystemErrorKey.value && !inodeErrorKey.value && (['not_collected', 'failed'].includes(filesystems.value?.collection.filesystem) || !mountRows.value.some(row => Object.values(row.metrics).some(point => currentResourceValue(point) !== null))) ? resourceMissingMessage(filesystems.value?.collection, 'filesystem') : '')
const deviceFamilies = [{ name: 'disk', metrics: diskMetrics, value: disks, error: diskErrorKey }, { name: 'network', metrics: networkMetrics, value: networks, error: networkErrorKey }]
const deviceSections = computed(() => deviceFamilies.map(family => {
  const sourceRows = resourceDimensionRows(family.value.value)
  const rows = family.name === 'disk' ? sortDiskResourceRows(sourceRows) : sourceRows
  const compactRows = family.name === 'disk' ? visibleDiskResourceRows(rows, state.value.metric.startsWith('node.disk.') ? state.value.dimensions.device : '') : rows
  return { name: family.name, metrics: family.metrics, rows, visibleRows: family.name === 'disk' && showAllDisks.value ? rows : compactRows, collapsedCount: rows.length - compactRows.length, errorKey: family.error.value, missingMessage: (['not_collected', 'failed'].includes(family.value.value?.collection[family.name]) || !rows.some(row => Object.values(row.metrics).some(point => currentResourceValue(point) !== null))) ? resourceMissingMessage(family.value.value?.collection, family.name) : '' }
}))
const trendMetrics = computed(() => [...resourceMetrics, ...(state.value.dimensions.mountpoint ? mountMetrics : state.value.dimensions.device ? state.value.metric.startsWith('node.network.') ? networkMetrics : diskMetrics : [])])
const chartMetrics = computed(() => resourceTrendMetrics(state.value.metric))
const trendPoints = computed(() => (trend.value?.series.find(line => line.metric_key === state.value.metric)?.points || []).filter(point => !comparison.value || Date.parse(point.evaluated_at) > Date.parse(comparison.value.current.start)))
const rawChartRows = computed(() => (trend.value ? resourceTrendChartRows(trend.value, state.value.metric) : []).filter(row => !comparison.value || Date.parse(row.evaluated_at) > Date.parse(comparison.value.current.start)))
const byteMetrics = computed(() => chartMetrics.value.filter(metric => ['bytes', 'bytes_per_second'].includes(metric.unit)))
const memoryTrend = computed(() => state.value.metric.startsWith('node.memory.'))
const networkTrend = computed(() => state.value.metric.startsWith('node.network.'))
const chartScale = computed(() => byteMetrics.value.length ? scaleByteValue(Math.max(0, ...rawChartRows.value.flatMap(row => byteMetrics.value.map(metric => row[metric.name] ?? 0)))) : null)
const chartRows = computed(() => rawChartRows.value.map(row => ({ evaluated_at: row.evaluated_at, ...Object.fromEntries(chartMetrics.value.map(metric => [metric.name, row[metric.name] === null ? null : row[metric.name] / (byteMetrics.value.includes(metric) ? chartScale.value.divisor : 1)])) })))
const presentation = metric => ({ field: 'value', label: t(`monitor.resources.metrics.${metric.name}`), unit: t(`monitor.resources.units.${metric.unit}`), precision: metric.precision })
const chartAxes = computed(() => memoryTrend.value ? [
  { measures: byteMetrics.value.map(metric => metric.name), label: t('monitor.resources.memoryCapacity'), min: 0 },
  { measures: ['memoryUsed'], min: 0, max: 100 },
] : networkTrend.value ? [{ measures: chartMetrics.value.map(metric => metric.name), label: t('monitor.resources.networkAxis'), min: 0 }] : [])
const chartConfig = computed(() => ({ chart_type: networkTrend.value ? 'bar' : 'line', dimension: 'evaluated_at', measures: chartMetrics.value.map(metric => metric.name), field_presentations: [{ field: 'evaluated_at', label: t('monitor.resources.evaluatedAt'), temporal_format: 'datetime' }, ...chartMetrics.value.map(metric => ({ ...presentation(metric), field: metric.name, ...(byteMetrics.value.includes(metric) ? { unit: chartScale.value.unit + (metric.unit === 'bytes_per_second' ? '/s' : ''), precision: 2 } : {}) }))] }))
const evidenceRows = computed(() => trendPoints.value.slice((evidencePage.value - 1) * 20, evidencePage.value * 20))
const comparisonChange = computed(() => {
  const delta = comparison.value?.delta
  if (delta === null || delta === undefined) return ''
  if (delta === 0) return t('monitor.resources.comparison.unchanged')
  if (Math.round(Math.abs(delta) * 100) === 0) return t('monitor.resources.comparison.smallChange')
  return t(`monitor.resources.comparison.${delta > 0 ? 'increased' : 'decreased'}`, { value: formatFieldPresentationValue(Math.abs(delta), { precision: 2 }, locale.value) })
})
const pageVisible = ref(!document.hidden), refreshBlocked = ref(false)
const refreshStatus = computed(() => state.value.refresh === 'off' ? 'manual' : refreshBlocked.value ? 'stopped' : !pageVisible.value ? 'paused' : 'automatic')
let epoch = 0, disposed = false
const requestScope = createLatestRequestScope()
useConsolePageDescriptor(router, 'monitor', { title: computed(() => t('monitor.resources.detailTitle')), subject: computed(() => node.value?.display_name || ''), ready: computed(() => Boolean(node.value)) })
function date(value) { return formatFieldPresentationValue(value, { temporal_format: 'datetime' }, locale.value) }
function point(key) { return instant.value.series.find(series => series.metric_key === key).points[0] }
function trendPoint(key) { return trend.value?.series.find(series => series.metric_key === key)?.points.at(-1) }
function summaryPoint(row, metric) { return summaries.value.get(row.node_id)?.series.find(series => series.metric_key === metric.key)?.points[0] }
function formatValue(value, metric) {
  const numeric = currentResourceValue(value)
  if (numeric === null) return '—'
  if (['bytes', 'bytes_per_second'].includes(metric.unit)) return formatBytes(numeric, 2, locale.value) + (metric.unit === 'bytes_per_second' ? '/s' : '')
  if (metric.unit === 'seconds') return formatDurationSeconds(numeric, locale.value)
  const formatted = formatFieldPresentationValue(numeric, presentation(metric), locale.value)
  return metric.unit === 'load' ? t('monitor.resources.loadValue', { value: formatted }) : formatted
}
function navigate(path, query, history = 'push') { return navigateMonitorRoute(router, { path, query }, { history }) }
function openRelatedAlerts() {
  if (node.value && canReadAlerts.value) navigate('/platform-alerts', { category: 'resource', subject_id: nodeID.value })
}
function resumeLiveTrend() {
  const { end, refresh, ...query } = state.value.query
  return navigate(route.path, query, 'replace')
}
function configureCollection() {
  return instant.value.target_id ? navigate(`/monitoring-targets/${instant.value.target_id}`, {}) : navigate('/monitoring-targets/new', { node_id: nodeID.value })
}
function changePage(page) { return navigate(route.path, { ...state.value.query, page: page === 1 ? undefined : String(page) }, 'replace') }
function changeSize(size) { return navigate('/node-resources', { ...state.value.query, page: undefined, page_size: size === 20 ? undefined : String(size) }, 'replace') }
function search() { return navigate('/node-resources', { ...state.value.query, search: searchText.value.trim() || undefined, page: undefined }, 'replace') }
function selectTrend(key, value) {
  const query = { ...state.value.query, [key]: value }
  if (key === 'metric' && !value.startsWith('node.filesystem.') && !value.startsWith('node.disk.') && !value.startsWith('node.network.')) for (const dimension of ['device', 'mountpoint', 'fstype']) delete query[dimension]
  return navigate(route.path, query, 'replace')
}
function selectFilesystem(row) { return navigate(route.path, { ...state.value.query, ...row.dimensions, metric: 'node.filesystem.used_percent' }, 'replace') }
function selectDeviceObservation(row, metric) { const { mountpoint, fstype, ...query } = state.value.query; return navigate(route.path, { ...query, ...row.dimensions, metric }, 'replace') }
function invalidChart() { trend.value = null; comparison.value = null; trendErrorKey.value = 'monitor.resources.errors.invalidResponse' }
const refreshCycle = createAutoRefresh({
  interval: () => state.value.refresh,
  enabled: () => canRead.value && (!nodeID.value || isTargetUUID(nodeID.value)) && !refreshBlocked.value && !state.value.changed && !loading.value,
  refresh: () => reload({ automatic: true }),
  pause: () => { invalidate(); loading.value = false },
  visibilityChanged: visible => { pageVisible.value = visible }
})
function invalidate() { refreshCycle.stop(); epoch = requestScope.invalidate() }
const request = work => requestScope.request(work)
async function reload({ automatic = false } = {}) {
  if (disposed || !['NodeResources', 'NodeResourceDetail'].includes(route.name)) { invalidate(); return }
  if (automatic && (loading.value || disposed || !canRead.value || !pageVisible.value || refreshBlocked.value)) return
  invalidate()
  if (!automatic) refreshBlocked.value = false
  const ticket = epoch
  comparison.value = null
  if (state.value.compare) trend.value = null
  if (!automatic) { summaries.value = new Map(); summaryErrorKey.value = '' }
  if (!automatic) { nodes.value = []; total.value = 0; node.value = null; instant.value = null; trend.value = null; filesystems.value = null; inodes.value = null; disks.value = null; networks.value = null }
  errorKey.value = ''; trendErrorKey.value = ''; filesystemErrorKey.value = ''; inodeErrorKey.value = ''; diskErrorKey.value = ''; networkErrorKey.value = '';  evidencePage.value = 1; loading.value = false
  if (!canRead.value) return
  if (state.value.changed) { await navigate(route.path, state.value.query, 'replace'); return }
  if (!automatic) searchText.value = state.value.search
  const id = nodeID.value
  if (state.value.invalidEnd) { errorKey.value = 'monitor.resources.history.invalid'; return }
  if (state.value.invalidDimensions) { errorKey.value = 'monitor.resources.errors.invalidDimensions'; return }
  if (id && !isTargetUUID(id)) { errorKey.value = 'monitor.resources.errors.invalidID'; return }
  loading.value = true
  try {
    if (!id) {
      const value = await request(config => api.nodes({ page: state.value.page, page_size: state.value.pageSize, ...(state.value.search ? { search: state.value.search } : {}) }, config))
      if (!requestScope.current(ticket)) return
      let nextSummaries = new Map(), nextSummaryError = ''
      if (value.data.length) {
        try {
          const overview = await request(config => api.summaries(value.data.map(row => row.node_id), config))
          if (!requestScope.current(ticket)) return
          nextSummaries = validateResourceSummaries(overview, value.data)
        } catch (error) {
          if (!requestScope.current(ticket)) return
          if ([401, 403, 404].includes(error.response?.status)) throw error
          nextSummaryError = error.response?.data?.error_code === 'observability_query_budget_exceeded' ? 'monitor.resources.overview.budget' : resourceErrorKey(error)
        }
      }
      nodes.value = value.data; total.value = value.total
      summaries.value = nextSummaries; summaryErrorKey.value = nextSummaryError
    } else {
      const value = await request(config => api.node(id, config))
      if (!requestScope.current(ticket)) return
      if (value.node_id !== id) throw new Error('invalid_resource_response')
      node.value = value
      const keys = resourceMetrics.map(metric => metric.key)
      const current = await request(config => api.instant({ node_id: id, metrics: keys.join(',') }, config))
      if (!requestScope.current(ticket)) return
      validateCollection(current.collection, current.queried_at)
      instant.value = validateResourceResponse(current, id, keys)
      if (state.value.end && Date.parse(state.value.end) > Date.parse(current.end)) { errorKey.value = 'monitor.resources.history.invalid'; return }
      filesystems.value = null; inodes.value = null; disks.value = null; networks.value = null
      try {
        const filesystemKeys = filesystemMetrics.map(metric => metric.key)
        const mounts = await request(config => api.instant({ node_id: id, metrics: filesystemKeys.join(',') }, config))
        if (!requestScope.current(ticket)) return
        validateCollection(mounts.collection, mounts.queried_at)
        filesystems.value = validateResourceResponse(mounts, id, filesystemKeys)
      } catch (error) {
        if (!requestScope.current(ticket)) return
        filesystemErrorKey.value = resourceErrorKey(error)
        if ([401, 403, 404].includes(error.response?.status)) throw error
      }
      inodes.value = null; disks.value = null; networks.value = null
      try {
        const inodeKeys = inodeMetrics.map(metric => metric.key)
        const observed = await request(config => api.instant({ node_id: id, metrics: inodeKeys.join(',') }, config))
        if (!requestScope.current(ticket)) return
        validateCollection(observed.collection, observed.queried_at)
        inodes.value = validateResourceResponse(observed, id, inodeKeys)
      } catch (error) {
        if (!requestScope.current(ticket)) return
        inodeErrorKey.value = resourceErrorKey(error)
        if ([401, 403, 404].includes(error.response?.status)) throw error
      }
      for (const family of deviceFamilies) {
        try {
          const keys = family.metrics.map(metric => metric.key)
          const observed = await request(config => api.instant({ node_id: id, metrics: keys.join(',') }, config))
          if (!requestScope.current(ticket)) return
          validateCollection(observed.collection, observed.queried_at)
          family.value.value = validateResourceResponse(observed, id, keys)
        } catch (error) {
          if (!requestScope.current(ticket)) return
          family.error.value = resourceErrorKey(error)
          if ([401, 403, 404].includes(error.response?.status)) throw error
        }
      }
      trend.value = null
      try {
        const params = trendParameters(id, state.value.metric, state.value.range, state.value.end || current.end, state.value.dimensions, state.value.compare)
        const history = await request(config => api.trend(params, config))
        if (!requestScope.current(ticket)) return
        validateResourceResponse(history, id, chartMetrics.value.map(metric => metric.key), true, state.value.dimensions)
        validateResourceTrendWindow(history, params)
        resourceTrendChartRows(history, state.value.metric)
        const compared = state.value.compare ? resourceTrendComparison(history, state.value.range) : null
        trend.value = history; comparison.value = compared
        if (state.value.end) {
          await nextTick()
          if (!requestScope.current(ticket)) return
          trendSection.value?.scrollIntoView({ block: 'start' })
          trendSection.value?.focus({ preventScroll: true })
        }
      } catch (error) {
        if (!requestScope.current(ticket)) return
        trend.value = null
        comparison.value = null
        trendErrorKey.value = resourceErrorKey(error)
        if ([401, 403, 404].includes(error.response?.status)) { refreshBlocked.value = true; node.value = null; instant.value = null; filesystems.value = null; inodes.value = null; disks.value = null; networks.value = null; errorKey.value = trendErrorKey.value }
      }
    }
  } catch (error) {
    if (!requestScope.current(ticket)) return
    instant.value = null; trend.value = null; filesystems.value = null; inodes.value = null; disks.value = null; networks.value = null
    errorKey.value = resourceErrorKey(error)
    if (!id) { nodes.value = []; total.value = 0; summaries.value = new Map() }
    if ([401, 403, 404].includes(error.response?.status)) { refreshBlocked.value = true; node.value = null; nodes.value = []; total.value = 0; summaries.value = new Map() }
  } finally { if (requestScope.current(ticket)) { loading.value = false; refreshCycle.schedule() } }
}
watch([() => JSON.stringify([nodeID.value, state.value.range, state.value.end, state.value.dimensions, state.value.compare, chartMetrics.value.map(metric => metric.key)]), identity], () => { trendZoom.value = null }, { flush: 'sync' })
watch([errorKey, trendErrorKey], ([error, trendError]) => { if (error || trendError) trendZoom.value = null }, { flush: 'sync' })
watch([nodeID, identity], () => { showAllDisks.value = false }, { flush: 'sync' })
watch([() => route.fullPath, identity], reload, { immediate: true, flush: 'sync' })
onBeforeUnmount(() => { disposed = true; refreshCycle.dispose(); invalidate(); requestScope.dispose() })
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
.resource-empty-status { display: flex; align-items: center; flex-wrap: wrap; gap: 12px; }
.resource-empty-status > .el-alert { flex: 1; min-width: 0; margin: 0; }
.resource-empty-status > .resource-hint { margin: 0; }
.resource-metric-heading { display: flex; align-items: center; gap: 4px; }
.resource-metric-heading .el-button { flex-shrink: 0; }
.resource-column-summary { font-weight: 400; line-height: 1.4; margin: 4px 0 0; }
.resource-chart { display: flex; min-width: 0; }
.resource-compare-control, .resource-comparison { margin: 16px 0; }
.resource-compare-control .el-checkbox { height: auto; max-width: 100%; }
.resource-compare-control :deep(.el-checkbox__label) { white-space: normal; }
.resource-comparison h4 { margin: 12px 0; }
.el-alert { margin: 16px 0; }
@media (max-width: 600px) { .node-resources { padding: 12px; } .resource-cards { grid-template-columns: 1fr; } }
</style>
