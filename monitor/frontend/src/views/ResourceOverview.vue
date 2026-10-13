<template>
  <section class="resource-overview" data-testid="resource-overview">
    <header class="overview-toolbar">
      <h2>{{ t('monitor.overview.title') }}</h2>
      <div class="overview-actions">
        <el-button v-if="canRead" @click="navigate('/node-resources')">{{ t('monitor.resources.title') }}</el-button>
        <el-button v-if="canReadServices" @click="navigate('/service-resources')">{{ t('monitor.process.title') }}</el-button>
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
      <div v-loading="loading">
        <template v-if="overview">
          <p class="overview-hint" data-testid="overview-time">{{ t('monitor.overview.evaluated', { time: date(overview.evaluated_at) }) }} · {{ t('monitor.resources.queriedAt') }}: {{ date(overview.queried_at) }}</p>
          <div class="overview-statistics" data-testid="overview-statistics">
            <el-card v-for="key in ['managed', 'enabled', 'disabled', 'connected', 'not_connected']" :key="key" shadow="never">
              <el-statistic :title="t(`monitor.overview.inventory.${key}`)" :value="overview.inventory[key]" />
            </el-card>
          </div>
          <p class="overview-hint" data-testid="overview-collection">{{ t('monitor.overview.collection') }}:
            <span v-for="(count, key) in overview.inventory.collection" :key="key" class="overview-count">{{ t(`monitor.resources.overview.collection.${key}`) }} {{ count }}</span>
          </p>
          <p class="overview-hint">{{ t('monitor.overview.collectionHint') }}</p>
          <div class="overview-rankings">
            <el-card v-for="(rank, index) in overview.rankings" :key="rank.metric_key" shadow="never" :data-testid="`overview-rank-${index}`">
              <template #header><h3>{{ t(index === 0 ? 'monitor.overview.cpuTitle' : 'monitor.overview.memoryTitle') }}</h3></template>
              <p class="overview-hint">{{ t(index === 0 ? 'monitor.overview.cpuHint' : 'monitor.overview.memoryHint') }}</p>
              <p class="overview-coverage">{{ t('monitor.overview.coverage', { valid: rank.coverage.valid, expected: rank.coverage.expected }) }} · {{ t(rank.coverage.complete ? 'monitor.overview.complete' : 'monitor.overview.incomplete') }}</p>
              <p class="overview-hint">{{ t('monitor.overview.missing', { missing: rank.coverage.no_data, stale: rank.coverage.stale, disconnected: rank.coverage.not_connected }) }}</p>
              <el-table v-if="rank.top.length" :data="rank.top" row-key="node.node_id">
                <el-table-column :label="t('monitor.overview.rank')" width="64"><template #default="{ $index }">{{ $index + 1 }}</template></el-table-column>
                <el-table-column :label="t('monitor.resources.node')" min-width="140"><template #default="{ row }"><el-button class="overview-node" text type="primary" @click="navigate(`/node-resources/${row.node.node_id}`)">{{ row.node.display_name }}</el-button><p class="overview-hint">{{ t(`monitor.overview.kind.${row.node.node_kind}`) }}</p></template></el-table-column>
                <el-table-column :label="t('monitor.overview.usage')" width="100"><template #default="{ row }">{{ percent(row.value) }}</template></el-table-column>
                <el-table-column :label="t('monitor.resources.sampledAt')" min-width="150"><template #default="{ row }">{{ date(row.sampled_at) }}</template></el-table-column>
              </el-table>
              <el-empty v-else :description="t(overview.inventory.managed ? 'monitor.overview.noValid' : 'monitor.overview.noNodes')" :image-size="60" />
            </el-card>
          </div>
        </template>
      </div>
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
import { formatFieldPresentationValue } from '../../../../common-frontend/basic/src/utils/fieldPresentation.mjs'
import { useAuthStore } from '../store/auth'
import { resourceOverviewAPI as api } from '../api/resourceOverview'
import { resolveOverviewRoute, validateResourceOverview } from '../utils/resourceOverview'
import { resourceErrorKey } from '../utils/nodeResources'
import { navigateMonitorRoute } from '../utils/moduleNavigation'

const { t, locale } = useI18n(), auth = useAuthStore(), route = useRoute(), router = useRouter()
const eligible = computed(() => auth.contextType === 'platform' && auth.authContext?.principal?.type === 'user' && !auth.authContext?.delegation)
const canRead = computed(() => eligible.value && auth.hasPermission('monitor.resource_observation.read') && auth.hasPermission('platform.host_node.read'))
const canReadServices = computed(() => eligible.value && auth.hasPermission('monitor.resource_observation.read') && auth.hasPermission('platform.module.read'))
const identity = computed(() => JSON.stringify([auth.authContext?.principal, auth.authContext?.context, auth.authContext?.delegation, auth.permissions]))
const state = computed(() => resolveOverviewRoute(route.query))
const overview = ref(null), loading = ref(false), errorKey = ref(''), visible = ref(!document.hidden), blocked = ref(false)
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
  if (!automatic) { overview.value = null; blocked.value = false }
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
watch(() => [route.fullPath, identity.value], () => { refreshCycle.stop(); requests.invalidate(); overview.value = null; errorKey.value = ''; loading.value = false; blocked.value = false; if (route.name === 'ResourceOverview') void reload() }, { immediate: true })
onBeforeUnmount(() => { disposed = true; refreshCycle.dispose(); requests.invalidate() })
</script>

<style scoped>
.resource-overview { padding: 24px; min-height: 100vh; color: var(--addp-text-primary); background: var(--addp-bg-secondary); }
.overview-toolbar, .overview-actions { display: flex; align-items: center; flex-wrap: wrap; gap: 12px; }
.overview-toolbar { justify-content: space-between; margin-bottom: 16px; }
.overview-statistics { display: grid; grid-template-columns: repeat(auto-fit, minmax(140px, 1fr)); gap: 16px; margin: 16px 0; }
.overview-rankings { min-width: 0; display: grid; grid-template-columns: repeat(auto-fit, minmax(min(100%, 480px), 1fr)); gap: 16px; margin-top: 16px; }
.overview-hint { color: var(--addp-text-secondary); font-size: 13px; margin: 8px 0; overflow-wrap: anywhere; }
.overview-count { display: inline-block; margin-right: 16px; }
.overview-coverage { font-size: 14px; margin: 12px 0 8px; }
.overview-rankings > * { min-width: 0; }
.overview-node { max-width: 100%; white-space: normal; height: auto; overflow-wrap: anywhere; text-align: left; }
@media (max-width: 700px) { .resource-overview { padding: 16px; } .overview-actions { width: 100%; } }
</style>
