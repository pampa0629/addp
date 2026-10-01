<template>
  <div class="module-instances">
    <div class="query-toolbar">
      <el-select v-model="moduleName" :placeholder="t('system.module.query.module')" clearable filterable class="query-control" @change="search">
        <el-option v-for="module in modules" :key="module.module_name"
          :label="resolveIAMModuleName(module.module_name, t, te)" :value="module.module_name" />
      </el-select>
      <el-input v-model.trim="registeredHost" :placeholder="t('system.module.query.host')" clearable class="query-control"
        @input="scheduleSearch" @clear="search" @keyup.enter="search" />
      <el-input v-model.trim="nodeName" :placeholder="t('system.module.query.node')" clearable class="query-control"
        @input="scheduleSearch" @clear="search" @keyup.enter="search" />
      <el-input v-model.trim="nodeIP" :placeholder="t('system.module.query.nodeIP')" clearable class="query-control"
        @input="scheduleSearch" @clear="search" @keyup.enter="search" />
      <el-select v-model="role" :placeholder="t('system.module.query.role')" clearable class="query-control" @change="search">
        <el-option v-for="item in roles" :key="item" :label="roleLabel(item)" :value="item" />
      </el-select>
      <el-select v-model="status" :empty-values="[null, undefined]" :aria-label="t('system.module.query.status')" class="query-control" @change="search">
        <el-option :label="t('system.module.query.allStatuses')" value="" />
        <el-option :label="t('system.module.status.up')" value="up" />
        <el-option :label="t('system.module.status.down')" value="down" />
      </el-select>
      <el-select v-model="stopReason" :empty-values="[null, undefined]" :aria-label="t('system.module.instances.stopReason')" class="query-control" @change="search">
        <el-option :label="t('system.module.query.allStopReasons')" value="" />
        <el-option v-for="reason in stopReasons" :key="reason" :label="t(`system.module.stopReasons.${reason}`)" :value="reason" />
      </el-select>
      <el-select v-model="timeBasis" :aria-label="t('system.module.query.timeBasis')" class="query-control" @change="search">
        <el-option :label="t('system.module.query.registeredTime')" value="registered" />
        <el-option :label="t('system.module.query.offlineTime')" value="offline" />
      </el-select>
      <el-select v-model="period" :aria-label="t('system.module.query.timeRange')" class="query-control" @change="search">
        <el-option :label="t('system.module.query.allTime')" value="all" />
        <el-option v-for="item in periods" :key="item.value" :label="t(item.label)" :value="item.value" />
        <el-option :label="t('system.module.query.custom')" value="custom" />
      </el-select>
      <el-date-picker v-if="period === 'custom'" v-model="customRange" type="datetimerange"
        :start-placeholder="t('system.module.query.from')" :end-placeholder="t('system.module.query.to')"
        class="query-range" @change="search" />
      <el-button @click="reset">{{ t('system.module.query.reset') }}</el-button>
      <el-button :icon="Refresh" :loading="loading" :disabled="period === 'custom' && !validCustomRange" @click="refresh">{{ t('system.module.refresh') }}</el-button>
    </div>
    <p class="query-hint">{{ t('system.module.query.timeHint') }}</p>
    <p class="query-freshness" data-testid="instance-query-freshness">
      {{ lastSuccessAt === null ? t('system.module.query.notLoaded') : t('system.module.query.lastSuccess', { time: formatDate(lastSuccessAt) }) }}
    </p>
    <el-alert v-if="dataStale" type="warning" :title="t('system.module.query.staleData')"
      :description="t('system.module.query.staleHint')" show-icon :closable="false" class="query-error" />
    <el-alert v-if="error" type="error" :title="error" show-icon :closable="false" class="query-error" />

    <el-table v-loading="loading" :data="rows" border stripe>
      <el-table-column type="expand" :label="t('system.module.instances.diagnostics')" width="80">
        <template #default="{ row }">
          <div class="instance-diagnostics">
            <el-descriptions :column="2" border>
              <el-descriptions-item :label="t('system.module.instances.lastHeartbeat')">{{ formatDate(row.last_heartbeat) }}</el-descriptions-item>
              <el-descriptions-item :label="t('system.module.instances.leaseExpiresAt')">{{ formatDate(row.lease_expires_at) }}</el-descriptions-item>
              <el-descriptions-item :label="t('system.module.instances.offlineDeterminedAt')">{{ formatDate(row.stopped_at) }}</el-descriptions-item>
              <el-descriptions-item :label="t('system.module.instances.stopReason')">{{ stopReasonLabel(row) }}</el-descriptions-item>
              <el-descriptions-item :label="t('system.module.instances.hostNodeName')">{{ row.host_node_name || t('system.module.instances.nodeUnknown') }}</el-descriptions-item>
              <el-descriptions-item :label="t('system.module.instances.hostNodeIPs')">{{ row.host_node_ips?.join(', ') || t('system.module.instances.nodeUnknown') }}</el-descriptions-item>
              <el-descriptions-item :label="t('system.module.instances.runtimeHostname')">{{ row.runtime_hostname || t('system.module.instances.nodeUnknown') }}</el-descriptions-item>
              <el-descriptions-item :label="t('system.module.instances.url')">{{ row.module_url || '—' }}</el-descriptions-item>
              <el-descriptions-item :label="t('system.module.instances.healthCheckUrl')">{{ row.health_check_url || '—' }}</el-descriptions-item>
            </el-descriptions>
            <p v-if="!isRuntimeInstanceOnline(row) && row.stop_reason === 'lease_expired'" class="diagnostic-hint">
              {{ t('system.module.instances.leaseExpiredHint') }}
            </p>
          </div>
        </template>
      </el-table-column>
      <el-table-column :label="t('system.module.columns.name')" min-width="140">
        <template #default="{ row }">
          <strong>{{ resolveIAMModuleName(row.module_name, t, te) }}</strong>
          <div class="technical-name">{{ row.module_name }}</div>
        </template>
      </el-table-column>
      <el-table-column prop="instance_id" :label="t('system.module.instances.id')" min-width="185" show-overflow-tooltip />
      <el-table-column :label="t('system.module.instances.role')" width="112">
        <template #default="{ row }">{{ roleLabel(row.role) }}</template>
      </el-table-column>
      <el-table-column :label="t('system.module.instances.node')" min-width="220">
        <template #default="{ row }"><ModuleInstanceNode :instance="row" /></template>
      </el-table-column>
      <el-table-column :label="t('system.module.instances.endpoint')" min-width="180" show-overflow-tooltip>
        <template #default="{ row }">{{ getRegisteredEndpoint(row) || t('system.module.instances.noEndpoint') }}</template>
      </el-table-column>
      <el-table-column :label="t('system.module.instances.status')" width="118">
        <template #default="{ row }">
          <el-tag size="small" :type="isRuntimeInstanceOnline(row) ? 'success' : 'danger'">
            {{ t(`system.module.status.${isRuntimeInstanceOnline(row) ? 'up' : 'down'}`) }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column v-if="applied.timeBasis === 'offline'" :label="t('system.module.instances.offlineDeterminedAt')" width="175">
        <template #default="{ row }">{{ formatDate(row.stopped_at) }}</template>
      </el-table-column>
      <el-table-column :label="t('system.module.instances.uptime')" width="145">
        <template #default="{ row }">{{ formatRuntimeUptime(row, t) }}</template>
      </el-table-column>
      <el-table-column :label="t('system.module.instances.processStartedAt')" width="175">
        <template #default="{ row }">{{ formatDate(row.process_started_at) }}</template>
      </el-table-column>
      <el-table-column :label="t('system.module.instances.registeredAt')" width="175">
        <template #default="{ row }">{{ formatDate(row.registered_at) }}</template>
      </el-table-column>
      <el-table-column :label="t('system.module.instances.stopReason')" width="170">
        <template #default="{ row }">{{ stopReasonLabel(row) }}</template>
      </el-table-column>
      <el-table-column v-if="canReadLogs" :label="t('system.module.logs.title')" width="120" fixed="right">
        <template #default="{ row }"><el-button text type="primary" @click="logInstance = row; logsOpen = true">{{ t('system.module.logs.view') }}</el-button></template>
      </el-table-column>
      <template #empty><el-empty :description="t(period === 'custom' && !validCustomRange ? 'system.module.query.completeRange' : lastSuccessAt === null ? 'system.module.query.notLoaded' : 'system.module.query.empty')" :image-size="72" /></template>
    </el-table>
    <el-pagination v-if="lastSuccessAt !== null" v-model:current-page="page" v-model:page-size="pageSize" :page-sizes="[10, 20, 50, 100]"
      :total="total" layout="total, sizes, prev, pager, next" class="query-pagination"
      @current-change="changePage" @size-change="changePageSize" />
    <ModuleRuntimeLogs v-model="logsOpen" :instance="logInstance" />
  </div>
</template>

<script setup>
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Refresh } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import ModuleInstanceNode from './ModuleInstanceNode.vue'
import ModuleRuntimeLogs from './ModuleRuntimeLogs.vue'
import { useAuthStore } from '../store/auth'
import { modulesAPI } from '../api/modules'
import { resolveIAMModuleName } from '../utils/iamPresentation'
import { isRuntimeInstanceOnline } from '../utils/moduleRegistry'
import { formatRuntimeUptime, getRegisteredEndpoint } from '../utils/moduleRuntimePresentation'
import { MODULE_INSTANCE_ROLES, MODULE_INSTANCE_STOP_REASONS, MODULE_INSTANCE_PERIODS, resolveModulesRouteState, buildModuleInstancesQuery } from '../utils/routeState'
import { navigateSystemRoute } from '../utils/moduleNavigation'

defineProps({ modules: { type: Array, required: true } })
const { t, te } = useI18n()
const authStore = useAuthStore()
const canReadLogs = computed(() => authStore.hasPermission('platform.module_log.read'))
const logInstance = ref(null), logsOpen = ref(false)
const route = useRoute()
const router = useRouter()
const roles = MODULE_INSTANCE_ROLES
const stopReasons = MODULE_INSTANCE_STOP_REASONS
const periods = MODULE_INSTANCE_PERIODS
const moduleName = ref('')
const registeredHost = ref('')
const nodeName = ref('')
const nodeIP = ref('')
const role = ref('')
const defaultStatus = 'up'
const status = ref(defaultStatus)
const stopReason = ref('')
const timeBasis = ref('registered')
const period = ref('all')
const customRange = ref(null)
const validCustomRange = computed(() => Array.isArray(customRange.value) && customRange.value.length === 2 &&
  new Date(customRange.value[0]).getTime() < new Date(customRange.value[1]).getTime())
const applied = ref({})
const rows = ref([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(10)
const loading = ref(false)
const error = ref('')
const refreshIntervalMs = 10000
const lastSuccessAt = ref(null)
const observedAt = ref(Date.now())
const dataStale = computed(() => lastSuccessAt.value !== null &&
  (Boolean(error.value) || observedAt.value - lastSuccessAt.value >= 2 * refreshIntervalMs))
let generation = 0
let searchTimer = null
let refreshTimer = null
let requestsInFlight = 0

function roleLabel(value) {
  return t(`system.module.roles.${value}`)
}

function stopReasonLabel(instance) {
  if (isRuntimeInstanceOnline(instance)) return '—'
  const key = `system.module.stopReasons.${instance.stop_reason}`
  return instance.stop_reason && te(key) ? t(key) : '—'
}

function formatDate(value) {
  if (!value) return '—'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? '—' : date.toLocaleString()
}

function currentParams() {
  const params = {
    page: page.value, page_size: pageSize.value,
    ...(applied.value.moduleName ? { module_name: applied.value.moduleName } : {}),
    ...(applied.value.registeredHost ? { registered_host: applied.value.registeredHost } : {}),
    ...(applied.value.nodeName ? { node_name: applied.value.nodeName } : {}),
    ...(applied.value.nodeIP ? { node_ip: applied.value.nodeIP } : {}),
    ...(applied.value.role ? { role: applied.value.role } : {}),
    ...(applied.value.status ? { status: applied.value.status } : {}),
    ...(applied.value.stopReason ? { stop_reason: applied.value.stopReason } : {}),
    ...(applied.value.timeBasis === 'offline' ? { time_basis: 'offline' } : {})
  }
  if (applied.value.period === 'custom') {
    params.time_from = applied.value.from
    params.time_to = applied.value.to
  } else {
    const preset = periods.find(item => item.value === applied.value.period)
    if (preset) {
      const now = Date.now()
      params.time_from = new Date(now - preset.minutes * 60000).toISOString()
      params.time_to = new Date(now).toISOString()
    }
  }
  return params
}

async function load({ silent = false } = {}) {
  if (period.value === 'custom' && !validCustomRange.value) return
  if (silent && (requestsInFlight > 0 || searchTimer !== null || document.hidden)) return
  const requestGeneration = ++generation
  requestsInFlight += 1
  if (!silent) loading.value = true
  try {
    const response = await modulesAPI.listInstances(currentParams())
    if (requestGeneration !== generation) return
    rows.value = response.data || []
    total.value = response.total || 0
    lastSuccessAt.value = Date.now()
    observedAt.value = lastSuccessAt.value
    error.value = ''
  } catch (failure) {
    if (requestGeneration !== generation) return
    error.value = failure.response?.data?.error || failure.message || t('system.module.query.loadFailed')
  } finally {
    requestsInFlight -= 1
    if (requestGeneration === generation) loading.value = false
  }
}

function clearResults() {
  rows.value = []
  total.value = 0
  lastSuccessAt.value = null
  error.value = ''
}

function search() {
  clearTimeout(searchTimer)
  searchTimer = null
  if (period.value === 'custom' && !validCustomRange.value) {
    generation += 1
    loading.value = false
    clearResults()
    return
  }
  const filters = {
    moduleName: moduleName.value, registeredHost: registeredHost.value.trim(), nodeName: nodeName.value.trim(), nodeIP: nodeIP.value.trim(), role: role.value, status: status.value,
    stopReason: stopReason.value, timeBasis: timeBasis.value, period: period.value,
    ...(period.value === 'custom' ? {
      from: new Date(customRange.value[0]).toISOString(), to: new Date(customRange.value[1]).toISOString()
    } : {})
  }
  writeRoute({ ...filters, page: 1, pageSize: pageSize.value })
}

function writeRoute(filters) {
  generation += 1
  const query = buildModuleInstancesQuery(filters)
  const target = { name: 'Modules', query }
  if (router.resolve(target).fullPath === route.fullPath) restoreRoute()
  else navigateSystemRoute(router, target, { history: 'replace' })
}

function scheduleSearch() {
  clearTimeout(searchTimer)
  generation += 1
  loading.value = false
  searchTimer = setTimeout(search, 300)
}

function refresh() {
  if (searchTimer !== null) search()
  else load()
}

function reset() {
  clearTimeout(searchTimer)
  searchTimer = null
  writeRoute({ status: defaultStatus, page: 1, pageSize: 10 })
}

function changePage() {
  if (searchTimer !== null) return search()
  writeRoute({ ...applied.value, page: page.value, pageSize: pageSize.value })
}

function changePageSize() {
  if (searchTimer !== null) return search()
  writeRoute({ ...applied.value, page: 1, pageSize: pageSize.value })
}

function restoreRoute() {
  clearTimeout(searchTimer)
  searchTimer = null
  generation += 1
  const state = resolveModulesRouteState(route.query)
  if (state.changed || state.tab !== 'instances') return
  const filters = state.filters
  moduleName.value = filters.moduleName
  registeredHost.value = filters.registeredHost
  nodeName.value = filters.nodeName
  nodeIP.value = filters.nodeIP
  role.value = filters.role
  status.value = filters.status
  stopReason.value = filters.stopReason
  timeBasis.value = filters.timeBasis
  period.value = filters.period
  customRange.value = filters.period === 'custom' ? [new Date(filters.from), new Date(filters.to)] : null
  page.value = filters.page
  pageSize.value = filters.pageSize
  applied.value = filters
  clearResults()
  load()
}
function autoRefresh() {
  observedAt.value = Date.now()
  load({ silent: true })
}
watch(() => route.fullPath, restoreRoute, { immediate: true })
onMounted(() => {
  refreshTimer = window.setInterval(autoRefresh, refreshIntervalMs)
  document.addEventListener('visibilitychange', autoRefresh)
})
onUnmounted(() => {
  clearTimeout(searchTimer)
  window.clearInterval(refreshTimer)
  document.removeEventListener('visibilitychange', autoRefresh)
  generation += 1
})
</script>

<style scoped>
.query-toolbar { display: flex; flex-wrap: wrap; gap: 10px; align-items: center; margin: 10px 0; }
.query-control { width: 175px; }
.query-range { max-width: 380px; }
.query-hint, .query-freshness, .technical-name { color: var(--addp-text-secondary); font-size: 12px; }
.query-hint { margin: 0 0 12px; }
.query-freshness { margin: 0 0 12px; }
.query-error { margin-bottom: 12px; }
.query-pagination { justify-content: flex-end; margin-top: 16px; }
.instance-diagnostics { padding: 12px 18px; }
.instance-diagnostics :deep(.el-descriptions__content) { overflow-wrap: anywhere; }
.diagnostic-hint { color: var(--addp-text-secondary); font-size: 12px; margin: 10px 0 0; }
@media (max-width: 900px) { .instance-diagnostics :deep(.el-descriptions__body) { overflow-x: auto; } }
@media (max-width: 600px) { .query-control, .query-range { width: 100%; max-width: none; } }
</style>
