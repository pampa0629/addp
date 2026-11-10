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
      <div v-loading="loading && !overview && !errorKey" :aria-busy="loading">
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
      <div v-if="!blocked && (canReadEngines || canReadServices)" class="coverage-grid" :aria-busy="loading">
        <el-card v-if="canReadEngines" shadow="never" data-testid="overview-engine-coverage">
          <template #header>
            <div class="overview-card-header">
              <h3>{{ t('monitor.overview.engineCoverage') }}</h3>
              <el-button text type="primary" @click="navigate('/engine-resources')">{{ t('monitor.engines.title') }}</el-button>
            </div>
          </template>
          <el-alert v-if="coverageErrors.engines" :title="t(coverageErrors.engines)" type="warning" :closable="false" />
          <template v-else-if="coverage.engines">
            <p class="overview-hint">{{ t('monitor.overview.engineCoverageHint') }}</p>
            <p v-if="coverage.engines.limited" class="overview-hint">{{ t('monitor.overview.sampleBudget') }}</p>
            <dl class="coverage-facts">
              <div v-for="scope in coverage.engines.scopes" :key="scope.key" :data-testid="`overview-engine-${scope.key}`">
                <dt><el-button link type="primary" @click="navigate('/engine-resources', { scope: scope.key })">{{ t(`monitor.engines.scopes.${scope.key}`) }}</el-button></dt>
                <dd>{{ t('monitor.overview.engineInventory', scope) }}</dd>
                <dd>{{ t('monitor.overview.engineSamples', { sampled: scope.sampled ?? '—', missing: scope.missing ?? '—' }) }}</dd>
              </div>
            </dl>
          </template>
          <el-skeleton v-else :rows="2" />
        </el-card>
        <el-card v-if="canReadServices" shadow="never" data-testid="overview-service-coverage">
          <template #header>
            <div class="overview-card-header">
              <h3>{{ t('monitor.overview.serviceCoverage') }}</h3>
              <el-button text type="primary" @click="navigate('/service-resources', { process_metrics_declared: 'all' })">{{ t('monitor.process.coverage.action') }}</el-button>
            </div>
          </template>
          <el-alert v-if="coverageErrors.services" :title="t(coverageErrors.services)" type="warning" :closable="false" />
          <template v-else-if="coverage.services">
            <p class="overview-hint">{{ t('monitor.overview.serviceCoverageHint') }}</p>
            <p v-if="coverage.services.limited" class="overview-hint">{{ t('monitor.overview.sampleBudget') }}</p>
            <dl class="coverage-facts">
              <div v-for="key in ['online', 'undeclared', 'sampled', 'missing']" :key="key" :data-testid="`overview-service-${key}`">
                <dt>
                  <el-button v-if="key === 'undeclared' && coverage.services.undeclared" link type="primary" @click="navigate('/service-resources', { process_metrics_declared: 'false' })">{{ t('monitor.overview.serviceCounts.undeclared') }}</el-button>
                  <template v-else>{{ t(`monitor.overview.serviceCounts.${key}`) }}</template>
                </dt>
                <dd>{{ coverage.services[key] ?? '—' }}</dd>
              </div>
            </dl>
          </template>
          <el-skeleton v-else :rows="2" />
        </el-card>
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
import { engineResourcesAPI as enginesAPI } from '../api/engineResources'
import { processResourcesAPI as processesAPI } from '../api/processResources'
import { engineSummaryFor, validateEnginePage, validateEngineResources } from '../utils/engineResources'
import { validateProcessInstances, validateProcessSummaries } from '../utils/processResources'
import { resolveOverviewRoute, validateResourceOverview } from '../utils/resourceOverview'
import { resourceErrorKey } from '../utils/nodeResources'
import { navigateMonitorRoute } from '../utils/moduleNavigation'

const { t, locale } = useI18n(), auth = useAuthStore(), route = useRoute(), router = useRouter()
const eligible = computed(() => auth.contextType === 'platform' && auth.authContext?.principal?.type === 'user' && !auth.authContext?.delegation)
const canOpen = path => eligible.value && allowsConsoleRoute(`/monitor${path}`, auth.contextType, auth.permissions)
const canRead = computed(() => canOpen('/resource-overview'))
const canReadEngines = computed(() => canRead.value && canOpen('/engine-resources'))
const canReadServices = computed(() => canRead.value && canOpen('/service-resources'))
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
const coverage = ref({}), coverageErrors = ref({})
const coverageBlocked = new Set()
const inventoryPage = { page: 1, page_size: 100 }
let disposed = false
const date = value => formatFieldPresentationValue(value, { temporal_format: 'datetime' }, locale.value)
const percent = value => formatFieldPresentationValue(value, { unit: '%', precision: 2 }, locale.value)
const navigate = (path, query) => navigateMonitorRoute(router, { path, query })
function selectRefresh(refresh) { return navigateMonitorRoute(router, { path: '/resource-overview', query: resolveOverviewRoute({ refresh }).query }, { history: 'replace' }) }
const refreshCycle = createAutoRefresh({
  interval: () => state.value.refresh,
  enabled: () => canRead.value && route.name === 'ResourceOverview' && !state.value.changed && !blocked.value && !loading.value,
  refresh: () => reload({ automatic: true }),
  pause: () => { requests.invalidate(); loading.value = false },
  visibilityChanged: value => { visible.value = value }
})
function completeInventory(page) {
  if (page.total > inventoryPage.page_size) throw new Error('overview_inventory_limit')
  if (page.data.length !== page.total) throw new Error('invalid_resource_response')
  return page.data
}

async function readCoverageSamples(load) {
  try {
    return await load()
  } catch (error) {
    if (error.response?.data?.error_code === 'observability_query_budget_exceeded') return null
    throw error
  }
}
const hasValidSample = row => row.series.some(series => series.points[0].data_state === 'valid')

async function engineCoverage(read) {
  const page = await read(config => enginesAPI.deployments(inventoryPage, config))
  const owners = completeInventory(validateEnginePage(page, inventoryPage.page, inventoryPage.page_size))
  const eligible = owners.filter(row => row.enabled && row.host_binding_state === 'bound')
  const samples = eligible.length ? await readCoverageSamples(async () => {
    const result = await read(config => enginesAPI.summaries(eligible.map(row => row.deployment_id), config))
    return validateEngineResources(result, eligible, row => engineSummaryFor(row.engine_type)).data
  }) : []
  const sampled = new Set(samples?.filter(hasValidSample).map(row => row.subject.deployment_id))
  return {
    limited: samples === null,
    scopes: ['infra', 'business'].map(key => {
      const scoped = eligible.filter(row => row.scope === key)
      const count = scoped.filter(row => sampled.has(row.deployment_id)).length
      const unknown = samples === null && scoped.length > 0
      return { key, managed: owners.filter(row => row.scope === key).length, eligible: scoped.length,
        sampled: unknown ? null : count, missing: unknown ? null : scoped.length - count }
    })
  }
}

async function serviceCoverage(read) {
  const params = { ...inventoryPage, status: 'up' }
  const page = await read(config => processesAPI.instances(params, config))
  const owners = completeInventory(validateProcessInstances(page, params.page, params, params.page_size))
  const declared = owners.filter(row => row.process_metrics_declared)
  const samples = declared.length ? await readCoverageSamples(async () => {
    const result = await read(config => processesAPI.summaries(declared.map(row => row.id), config))
    return [...validateProcessSummaries(result, declared).values()]
  }) : []
  const sampled = samples?.filter(hasValidSample).length ?? null
  return { online: owners.length, undeclared: owners.length - declared.length, limited: samples === null,
    sampled, missing: sampled === null ? null : declared.length - sampled }
}

async function loadCoverage(ticket, key, load) {
  if (!requests.current(ticket) || coverageBlocked.has(key)) return
  const read = callback => {
    if (!requests.current(ticket)) throw new DOMException('Request cancelled', 'AbortError')
    return requests.request(callback)
  }
  try {
    const value = await load(read)
    if (!requests.current(ticket)) return
    coverage.value[key] = value
    delete coverageErrors.value[key]
  } catch (error) {
    if (!requests.current(ticket)) return
    delete coverage.value[key]
    coverageErrors.value[key] = error.message === 'overview_inventory_limit' ? 'monitor.overview.inventoryLimit' : resourceErrorKey(error)
    if ([401, 403, 404].includes(error.response?.status)) coverageBlocked.add(key)
  }
}

async function reload({ automatic = false } = {}) {
  if (disposed || route.name !== 'ResourceOverview') return
  if (automatic && (loading.value || !canRead.value || !visible.value || blocked.value)) return
  refreshCycle.stop()
  const ticket = requests.invalidate()
  if (!automatic) {
    blocked.value = false
    coverageBlocked.clear()
  }
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
    if ([401, 403, 404].includes(error.response?.status)) {
      blocked.value = true
      coverage.value = {}
      coverageErrors.value = {}
    }
  }
  try {
    if (blocked.value || !requests.current(ticket)) return
    if (canReadEngines.value) await loadCoverage(ticket, 'engines', engineCoverage)
    if (requests.current(ticket) && canReadServices.value) await loadCoverage(ticket, 'services', serviceCoverage)
  } finally {
    if (requests.current(ticket)) { loading.value = false; refreshCycle.schedule() }
  }
}
watch([() => route.fullPath, identity], ([, currentIdentity], [, previousIdentity]) => {
  if (currentIdentity !== previousIdentity || route.name !== 'ResourceOverview') {
    overview.value = null
    coverage.value = {}
    coverageErrors.value = {}
  }
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
.coverage-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(100%, 400px), 1fr)); gap: 16px; margin-top: 20px; }
.coverage-grid > * { min-width: 0; }
.coverage-facts { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(100%, 128px), 1fr)); gap: 12px; margin: 12px 0 0; }
.coverage-facts dt { font-size: 13px; color: var(--addp-text-secondary); }
.coverage-facts dd { margin: 5px 0 0; }
.coverage-facts .el-button { height: auto; white-space: normal; text-align: left; }
.overview-shortcuts { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(100%, 172px), 1fr)); gap: 12px; }
.overview-shortcuts .el-button { padding: 0; }
.overview-shortcuts .overview-hint { margin-bottom: 0; }
.overview-coverage { font-size: 14px; margin: 12px 0 8px; }
.overview-rankings > * { min-width: 0; }
.overview-node { max-width: 100%; white-space: normal; height: auto; overflow-wrap: anywhere; text-align: left; }
@media (max-width: 700px) { .resource-overview { padding: 16px; } .overview-actions { width: 100%; } }
</style>
