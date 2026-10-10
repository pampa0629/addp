<template>
  <section class="process-resources">
    <div class="process-toolbar">
      <h2>{{ t('monitor.process.title') }}</h2>
      <el-button :disabled="!canRead || loading" @click="reload">{{ t('common.refresh') }}</el-button>
    </div>
    <el-alert v-if="!canRead" :title="t('monitor.process.denied')" type="warning" :closable="false" />
    <template v-else>
      <form class="process-filters" @submit.prevent="filter">
        <el-input v-model="moduleName" :placeholder="t('monitor.process.moduleFilter')" :aria-label="t('monitor.process.moduleFilter')" clearable />
        <el-select v-model="role" :placeholder="t('monitor.process.role')" :aria-label="t('monitor.process.role')" clearable>
          <el-option v-for="item in processRoles" :key="item" :value="item" :label="t(`monitor.process.roles.${item}`)" />
        </el-select>
        <el-select v-model="status" :placeholder="t('monitor.process.online')" :aria-label="t('monitor.process.online')" clearable>
          <el-option v-for="item in ['up', 'down']" :key="item" :value="item" :label="t(`monitor.process.status.${item}`)" />
        </el-select>
        <el-button native-type="submit">{{ t('common.search') }}</el-button>
      </form>
      <el-alert v-if="errorKey || summaryErrorKey" :title="t(errorKey || summaryErrorKey)" type="warning" :closable="false" data-testid="process-error" />
      <el-table :data="instances" v-loading="loading" :empty-text="t('monitor.process.empty')" data-testid="process-list">
        <el-table-column :label="t('monitor.process.module')" prop="module_name" min-width="120" />
        <el-table-column :label="t('monitor.process.instance')" min-width="170" show-overflow-tooltip>
          <template #default="{ row }">{{ row.runtime_hostname || row.registered_host || row.instance_id }}<p class="process-hint" :title="row.instance_id">{{ t(`monitor.process.roles.${row.role}`) }}</p></template>
        </el-table-column>
        <el-table-column :label="t('monitor.process.online')" min-width="100"><template #default="{ row }">{{ t(`monitor.process.status.${row.status}`) }}</template></el-table-column>
        <el-table-column :label="t('monitor.process.host')" min-width="160" show-overflow-tooltip><template #default="{ row }">{{ row.node_id ? row.host_node_name || row.node_id : t('monitor.process.unbound') }}</template></el-table-column>
        <el-table-column v-for="metric in processMetrics" :key="metric.key" min-width="200">
          <template #header><span>{{ t(`monitor.process.metrics.${metric.name}`) }}</span><p class="process-hint">{{ t(`monitor.process.hints.${metric.name}`) }}</p></template>
          <template #default="{ row }"><strong>{{ value(row, metric) }}</strong><p v-if="point(row, metric)?.data_state !== 'valid'" class="process-hint">{{ pointState(row, metric) }}</p></template>
        </el-table-column>
        <el-table-column :label="t('monitor.process.collection')" min-width="175"><template #default="{ row }"><span>{{ collectionState(row) }}</span><p v-if="sampledAt(row)" class="process-hint">{{ sampledAt(row) }}</p></template></el-table-column>
      </el-table>
      <el-pagination v-if="!loading && !errorKey" :current-page="state.page" :page-size="20" :total="total" layout="total, prev, pager, next" @current-change="changePage" />
    </template>
  </section>
</template>

<script setup>
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { createLatestRequestScope } from '../../../../common-frontend/basic/src/utils/latestRequestScope.mjs'
import { formatBytes, formatDurationSeconds } from '../../../../common-frontend/basic/src/utils/formatters.js'
import { formatFieldPresentationValue } from '../../../../common-frontend/basic/src/utils/fieldPresentation.mjs'
import { useAuthStore } from '../store/auth'
import { processResourcesAPI as api } from '../api/processResources'
import { processMetrics, processRoles, resolveProcessRoute, validateProcessInstances, validateProcessSummaries } from '../utils/processResources'
import { currentResourceValue, resourceErrorKey } from '../utils/nodeResources'
import { navigateMonitorRoute } from '../utils/moduleNavigation'

const { t, locale } = useI18n(), auth = useAuthStore(), route = useRoute(), router = useRouter()
const canRead = computed(() => auth.contextType === 'platform' && auth.authContext?.principal?.type === 'user' && !auth.authContext?.delegation && auth.hasPermission('monitor.resource_observation.read') && auth.hasPermission('platform.module.read'))
const identity = computed(() => JSON.stringify([auth.authContext?.principal, auth.authContext?.context, auth.authContext?.delegation, auth.permissions]))
const state = computed(() => resolveProcessRoute(route.query))
const instances = ref([]), summaries = ref(new Map()), total = ref(0), loading = ref(false), errorKey = ref(''), summaryErrorKey = ref('')
const moduleName = ref(''), role = ref(''), status = ref(''), requests = createLatestRequestScope()
const point = (row, metric) => summaries.value.get(row.id)?.series.find(item => item.metric_key === metric.key)?.points[0]
function value(row, metric) {
  const numeric = currentResourceValue(point(row, metric))
  if (numeric === null) return '—'
  if (metric.unit === 'bytes') return formatBytes(numeric, 2, locale.value)
  if (metric.unit === 'seconds') return formatDurationSeconds(numeric, locale.value)
  return t('monitor.process.cores', { value: new Intl.NumberFormat(locale.value, { minimumFractionDigits: 2, maximumFractionDigits: 2 }).format(numeric) })
}
function pointState(row, metric) {
  const data = point(row, metric)
  if (!data) return ''
  return t(data.data_state === 'no_data' && metric.name === 'cpu' && summaries.value.get(row.id)?.collection.state === 'collecting' ? 'monitor.process.waiting' : `monitor.process.states.${data.data_state}`)
}
function collectionState(row) {
  return t(summaries.value.has(row.id) ? `monitor.process.collectionStates.${summaries.value.get(row.id).collection.state}` : !row.process_metrics_declared ? 'monitor.process.collectionStates.not_connected' : 'monitor.process.unavailable')
}
function sampledAt(row) {
  const stamp = summaries.value.get(row.id)?.collection.sampled_at
  return stamp ? `${t('monitor.resources.sampledAt')}: ${formatFieldPresentationValue(stamp, { temporal_format: 'datetime' }, locale.value)}` : ''
}
const navigate = query => navigateMonitorRoute(router, { path: '/service-resources', query }, { history: 'replace' })
function filter() { return navigate({ ...(moduleName.value.trim() ? { module_name: moduleName.value.trim() } : {}), ...(role.value ? { role: role.value } : {}), ...(status.value ? { status: status.value } : {}) }) }
function changePage(page) { if (loading.value || page === state.value.page) return; return navigate({ ...state.value.query, page: page === 1 ? undefined : String(page) }) }
function processErrorKey(error) {
 const key = resourceErrorKey(error)
 return key.endsWith('.denied') ? 'monitor.process.denied' : key.endsWith('.notFound') ? 'monitor.process.notFound' : key
}
async function reload() {
  const ticket = requests.invalidate()
  instances.value = []; summaries.value = new Map(); total.value = 0; errorKey.value = ''; summaryErrorKey.value = ''; loading.value = false
  if (!canRead.value || route.name !== 'ProcessResources') return
  if (state.value.changed) { await navigate(state.value.query); return }
  moduleName.value = state.value.module; role.value = state.value.role; status.value = state.value.status
  loading.value = true
  try {
    const list = await requests.request(config => api.instances({ ...state.value.query, page: state.value.page, page_size: 20 }, config))
    if (!requests.current(ticket)) return
    validateProcessInstances(list, state.value.page)
    instances.value = list.data; total.value = list.total
    if (list.data.length) {
      try {
        const observed = await requests.request(config => api.summaries(list.data.map(row => row.id), config))
        if (!requests.current(ticket)) return
        summaries.value = validateProcessSummaries(observed, list.data)
      } catch (error) {
        if (!requests.current(ticket)) return
        if ([401, 403, 404].includes(error.response?.status)) throw error
        summaryErrorKey.value = processErrorKey(error)
      }
    }
  } catch (error) {
    if (!requests.current(ticket)) return
    errorKey.value = processErrorKey(error)
    instances.value = []; summaries.value = new Map(); total.value = 0
  } finally { if (requests.current(ticket)) loading.value = false }
}
watch([() => route.fullPath, identity], reload, { immediate: true, flush: 'sync' })
onBeforeUnmount(() => requests.dispose())
</script>

<style scoped>
.process-resources { padding: 24px; color: var(--addp-text-primary); min-width: 0; }
.process-toolbar, .process-filters { display: flex; align-items: center; flex-wrap: wrap; gap: 12px; margin-bottom: 16px; }
.process-toolbar h2 { flex: 1; }
.process-filters .el-input, .process-filters .el-select { width: 200px; max-width: 100%; }
.process-hint { margin: 4px 0; font-size: 12px; line-height: 1.5; color: var(--addp-text-secondary); }
.el-pagination { margin-top: 16px; }
</style>
