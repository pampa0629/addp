<template>
  <section class="resource-overview" data-testid="resource-overview">
    <header class="overview-toolbar">
      <h2>{{ t('monitor.overview.title') }}</h2>
      <div class="overview-actions">
        <template v-if="canRead">
          <AutoRefreshControl :model-value="state.refresh" :status="refreshStatus" test-id="overview-refresh" @update:model-value="selectRefresh" />
          <el-button :disabled="loading" @click="reload">{{ t('common.refresh') }}</el-button>
        </template>
      </div>
    </header>
    <el-alert v-if="!canRead" :title="t('monitor.resources.denied')" type="warning" :closable="false" data-testid="overview-denied" />
    <template v-else>
      <p class="overview-hint">{{ t('monitor.overview.scope') }}</p>
      <el-alert v-if="errorKey" :title="t(errorKey)" type="warning" :closable="false" data-testid="overview-error" />
      <div v-loading="loading && !overview" :aria-busy="loading">
        <template v-if="overview">
          <p class="overview-hint" data-testid="overview-time">{{ t('monitor.overview.evaluated', { time: date(overview.evaluated_at) }) }} · {{ t('monitor.resources.queriedAt') }}: {{ date(overview.queried_at) }}</p>
          <div class="overview-statistics" data-testid="overview-statistics">
            <el-card v-for="stat in hostStatistics" :key="stat.key" shadow="never" :data-testid="`overview-stat-${stat.key}`">
              <el-statistic :title="t(stat.label)" :value="stat.value" />
              <p class="overview-hint">{{ stat.detail }}</p>
            </el-card>
          </div>
          <el-card class="overview-section" shadow="never" data-testid="overview-collection">
            <template #header>
              <div class="overview-card-header">
                <h3>{{ t('monitor.overview.collection') }}</h3>
                <el-button v-if="canOpen('/monitoring-targets')" text type="primary" @click="navigate('/monitoring-targets')">{{ t('monitor.targets.title') }}</el-button>
              </div>
            </template>
            <div v-if="overview.inventory.managed" class="collection-distribution" aria-hidden="true">
              <span v-for="item in collectionStates.filter(item => item.count)" :key="item.key" class="collection-swatch" :data-state="item.key" :style="{ flexGrow: item.count }" />
            </div>
            <dl class="collection-legend">
              <div v-for="item in collectionStates" :key="item.key" :data-testid="`overview-collection-${item.key}`">
                <dt><span class="collection-swatch" :data-state="item.key" aria-hidden="true" />{{ t(item.label) }}</dt>
                <dd>{{ item.count }}</dd>
              </div>
            </dl>
            <p class="overview-hint">{{ t('monitor.overview.collectionHint') }}</p>
          </el-card>
          <h3 class="overview-section-title">{{ t('monitor.overview.pressure') }}</h3>
          <div class="overview-rankings">
            <el-card v-for="(rank, index) in overview.rankings" :key="rank.metric_key" shadow="never" :data-testid="`overview-rank-${index}`">
              <template #header>
                <div class="overview-card-header">
                  <h4>{{ t(index === 0 ? 'monitor.overview.cpuTitle' : 'monitor.overview.memoryTitle') }}</h4>
                  <el-tag size="small" :type="rank.coverage.complete ? 'success' : 'warning'">{{ t(rank.coverage.complete ? 'monitor.overview.complete' : 'monitor.overview.incomplete') }}</el-tag>
                </div>
              </template>
              <p class="overview-hint">{{ t(index === 0 ? 'monitor.overview.cpuHint' : 'monitor.overview.memoryHint') }}</p>
              <p class="overview-coverage">{{ t('monitor.overview.coverage', { valid: rank.coverage.valid, expected: rank.coverage.expected }) }}</p>
              <p class="overview-hint">{{ t('monitor.overview.missing', { missing: rank.coverage.no_data, stale: rank.coverage.stale, disconnected: rank.coverage.not_connected }) }}</p>
              <el-table v-if="rank.top.length" :data="rank.top" row-key="node.node_id">
                <el-table-column :label="t('monitor.overview.rank')" width="64"><template #default="{ $index }">{{ $index + 1 }}</template></el-table-column>
                <el-table-column :label="t('monitor.resources.node')" min-width="140"><template #default="{ row }"><el-button class="overview-node" text type="primary" @click="navigate(`/node-resources/${row.node.node_id}`)">{{ row.node.display_name }}</el-button><p class="overview-hint">{{ t(`monitor.overview.kind.${row.node.node_kind}`) }}</p></template></el-table-column>
                <el-table-column :label="t('monitor.overview.usage')" min-width="115">
                  <template #default="{ row }">
                    <strong>{{ percent(row.value) }}</strong>
                    <el-progress :percentage="row.value" :show-text="false" :stroke-width="5" aria-hidden="true" />
                  </template>
                </el-table-column>
                <el-table-column :label="t('monitor.resources.sampledAt')" min-width="150"><template #default="{ row }">{{ date(row.sampled_at) }}</template></el-table-column>
              </el-table>
              <el-empty v-else :description="t(overview.inventory.managed ? 'monitor.overview.noValid' : 'monitor.overview.noNodes')" :image-size="60" />
            </el-card>
          </div>
        </template>
      </div>
      <section class="overview-section" aria-labelledby="overview-shortcuts">
        <h3 id="overview-shortcuts" class="overview-section-title">{{ t('monitor.overview.shortcuts.title') }}</h3>
        <div class="overview-shortcuts">
          <el-card v-for="item in availableShortcuts" :key="item.path" shadow="never">
            <el-button text type="primary" @click="navigate(item.path)">{{ t(item.title) }}</el-button>
            <p class="overview-hint">{{ t(`monitor.overview.shortcuts.${item.key}`) }}</p>
          </el-card>
        </div>
      </section>
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
import { allowsConsoleRoute } from '../../../../common-frontend/basic/src/authorization/consoleRouteAccess.js'
import { formatFieldPresentationValue } from '../../../../common-frontend/basic/src/utils/fieldPresentation.mjs'
import { useAuthStore } from '../store/auth'
import { resourceOverviewAPI as api } from '../api/resourceOverview'
import { resolveOverviewRoute, validateResourceOverview } from '../utils/resourceOverview'
import { resourceErrorKey } from '../utils/nodeResources'
import { navigateMonitorRoute } from '../utils/moduleNavigation'

const { t, locale } = useI18n(), auth = useAuthStore(), route = useRoute(), router = useRouter()
const eligible = computed(() => auth.contextType === 'platform' && auth.authContext?.principal?.type === 'user' && !auth.authContext?.delegation)
const canOpen = path => eligible.value && allowsConsoleRoute(`/monitor${path}`, auth.contextType, auth.permissions)
const canRead = computed(() => canOpen('/resource-overview'))
const shortcuts = [
  { path: '/node-resources', key: 'nodes', title: 'monitor.resources.title' },
  { path: '/engine-resources', key: 'engines', title: 'monitor.engines.title' },
  { path: '/service-resources', key: 'services', title: 'monitor.process.title' },
  { path: '/http-quality', key: 'http', title: 'monitor.http.title' },
  { path: '/traces', key: 'traces', title: 'monitor.traces.title' }
]
const availableShortcuts = computed(() => shortcuts.filter(item => canOpen(item.path)))
const identity = computed(() => JSON.stringify([auth.authContext?.principal, auth.authContext?.context, auth.authContext?.delegation, auth.permissions]))
const state = computed(() => resolveOverviewRoute(route.query))
const overview = ref(null), loading = ref(false), errorKey = ref(''), visible = ref(!document.hidden), blocked = ref(false)
const collectionStates = computed(() => {
  const { collection, enabled, connected, disabled } = overview.value.inventory
  return [
    ...['collecting', 'failed', 'stale', 'no_sample'].map(key => ({ key, count: collection[key], label: `monitor.resources.overview.collection.${key}` })),
    { key: 'unconfigured', count: enabled - connected, label: 'monitor.overview.inventory.unconfigured' },
    { key: 'disabled', count: disabled, label: 'monitor.overview.inventory.disabled' }
  ]
})
const hostStatistics = computed(() => {
  const { managed, enabled, disabled, connected, collection } = overview.value.inventory
  return [
    { key: 'managed', label: 'monitor.overview.inventory.managed', value: managed, detail: t('monitor.overview.inventory.enabledAndDisabled', { enabled, disabled }) },
    { key: 'connected', label: 'monitor.overview.inventory.connected', value: connected, detail: t('monitor.overview.inventory.ofEnabled', { count: enabled }) },
    { key: 'collecting', label: 'monitor.resources.overview.collection.collecting', value: collection.collecting, detail: t('monitor.overview.inventory.ofConnected', { count: connected }) },
    { key: 'attention', label: 'monitor.overview.inventory.attention', value: collection.failed + collection.stale + collection.no_sample, detail: t('monitor.overview.inventory.attentionDetail') }
  ]
})
const refreshStatus = computed(() => state.value.refresh === 'off' ? 'manual' : blocked.value ? 'stopped' : !visible.value ? 'paused' : 'automatic')
const requests = createLatestRequestScope()
let disposed = false
const date = value => formatFieldPresentationValue(value, { temporal_format: 'datetime' }, locale.value)
const percent = value => formatFieldPresentationValue(value, { unit: '%', precision: 2 }, locale.value)
const navigate = path => navigateMonitorRoute(router, { path })
function selectRefresh(refresh) { return navigateMonitorRoute(router, { path: '/resource-overview', query: resolveOverviewRoute({ refresh }).query }, { history: 'replace' }) }
const refreshCycle = createAutoRefresh({
  interval: () => state.value.refresh,
  enabled: () => canRead.value && route.name === 'ResourceOverview' && !state.value.changed && !blocked.value && !loading.value,
  refresh: () => reload({ automatic: true }),
  pause: () => { requests.invalidate(); loading.value = false },
  visibilityChanged: value => { visible.value = value }
})
async function reload({ automatic = false } = {}) {
  if (disposed || route.name !== 'ResourceOverview') return
  if (automatic && (loading.value || !canRead.value || !visible.value || blocked.value)) return
  refreshCycle.stop()
  const ticket = requests.invalidate()
  if (!automatic) blocked.value = false
  errorKey.value = ''; loading.value = false
  if (!canRead.value) { overview.value = null; return }
  if (state.value.changed) { await navigateMonitorRoute(router, { path: '/resource-overview', query: state.value.query }, { history: 'replace' }); return }
  loading.value = true
  try {
    const value = await requests.request(config => api.overview(config))
    if (!requests.current(ticket)) return
    overview.value = validateResourceOverview(value)
  } catch (error) {
    if (!requests.current(ticket)) return
    overview.value = null
    errorKey.value = error.response?.data?.error_code === 'observability_query_budget_exceeded' ? 'monitor.overview.budget' : resourceErrorKey(error)
    if ([401, 403, 404].includes(error.response?.status)) blocked.value = true
  } finally {
    if (requests.current(ticket)) { loading.value = false; refreshCycle.schedule() }
  }
}
watch([() => route.fullPath, identity], ([, currentIdentity], [, previousIdentity]) => {
  if (currentIdentity !== previousIdentity || route.name !== 'ResourceOverview') overview.value = null
  if (route.name === 'ResourceOverview') void reload()
  else { refreshCycle.stop(); requests.invalidate() }
}, { immediate: true })
onBeforeUnmount(() => { disposed = true; refreshCycle.dispose(); requests.invalidate() })
</script>

<style scoped>
.resource-overview { padding: 24px; min-height: 100vh; color: var(--addp-text-primary); background: var(--addp-bg-secondary); }
.overview-toolbar, .overview-actions { display: flex; align-items: center; flex-wrap: wrap; gap: 12px; }
.overview-toolbar { justify-content: space-between; margin-bottom: 16px; }
.overview-statistics { display: grid; grid-template-columns: repeat(auto-fit, minmax(140px, 1fr)); gap: 16px; margin: 16px 0; }
.overview-rankings { min-width: 0; display: grid; grid-template-columns: repeat(auto-fit, minmax(min(100%, 480px), 1fr)); gap: 16px; margin-top: 16px; }
.overview-hint { color: var(--addp-text-secondary); font-size: 13px; margin: 8px 0; overflow-wrap: anywhere; }
.overview-section { margin: 20px 0; }
.overview-section-title { margin: 0 0 12px; }
.overview-card-header { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 8px; }
.overview-card-header h3, .overview-card-header h4 { margin: 0; }
.collection-distribution { display: flex; height: 12px; border-radius: 6px; overflow: hidden; }
.collection-distribution .collection-swatch { flex-basis: 0; }
.collection-swatch { background: var(--el-color-info); }
.collection-swatch[data-state="collecting"] { background: var(--el-color-success); }
.collection-swatch[data-state="failed"] { background: var(--el-color-danger); }
.collection-swatch[data-state="stale"] { background: var(--el-color-warning); }
.collection-swatch[data-state="no_sample"] { background: var(--el-color-primary); }
.collection-swatch[data-state="disabled"] { background: var(--addp-border-color); }
.collection-legend { display: grid; grid-template-columns: repeat(auto-fit, minmax(132px, 1fr)); gap: 12px; margin: 16px 0; }
.collection-legend dt { display: flex; align-items: center; gap: 6px; font-size: 13px; }
.collection-legend dt .collection-swatch { flex: 0 0 10px; height: 10px; border-radius: 50%; }
.collection-legend dd { font-weight: 600; margin: 4px 0 0 16px; }
.overview-shortcuts { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(100%, 172px), 1fr)); gap: 12px; }
.overview-shortcuts .el-button { padding: 0; }
.overview-shortcuts .overview-hint { margin-bottom: 0; }
.overview-coverage { font-size: 14px; margin: 12px 0 8px; }
.overview-rankings > * { min-width: 0; }
.overview-node { max-width: 100%; white-space: normal; height: auto; overflow-wrap: anywhere; text-align: left; }
@media (max-width: 700px) { .resource-overview { padding: 16px; } .overview-actions { width: 100%; } }
</style>
