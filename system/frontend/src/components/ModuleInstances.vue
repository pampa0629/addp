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
      <el-select v-model="role" :placeholder="t('system.module.query.role')" clearable class="query-control" @change="search">
        <el-option v-for="item in roles" :key="item" :label="roleLabel(item)" :value="item" />
      </el-select>
      <el-select v-model="status" :aria-label="t('system.module.query.status')" class="query-control" @change="search">
        <el-option :label="t('system.module.query.allStatuses')" value="" />
        <el-option :label="t('system.module.status.up')" value="up" />
        <el-option :label="t('system.module.status.down')" value="down" />
      </el-select>
      <el-select v-model="period" :aria-label="t('system.module.query.registeredPeriod')" class="query-control" @change="search">
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
      <template #empty><el-empty :description="t(period === 'custom' && !validCustomRange ? 'system.module.query.completeRange' : 'system.module.query.empty')" :image-size="72" /></template>
    </el-table>
    <el-pagination v-model:current-page="page" v-model:page-size="pageSize" :page-sizes="[10, 20, 50, 100]"
      :total="total" layout="total, sizes, prev, pager, next" class="query-pagination"
      @current-change="load" @size-change="changePageSize" />
  </div>
</template>

<script setup>
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { Refresh } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import ModuleInstanceNode from './ModuleInstanceNode.vue'
import { modulesAPI } from '../api/modules'
import { resolveIAMModuleName } from '../utils/iamPresentation'
import { isRuntimeInstanceOnline } from '../utils/moduleRegistry'
import { formatRuntimeUptime, getRegisteredEndpoint } from '../utils/moduleRuntimePresentation'

const props = defineProps({
  modules: { type: Array, required: true },
  initialModuleName: { type: String, default: '' }
})
const emit = defineEmits(['clear-selection'])
const { t, te } = useI18n()
const roles = ['backend', 'worker', 'scheduler', 'ingress']
const periods = [
  { value: '15m', minutes: 15, label: 'system.module.query.periods.m15' },
  { value: '30m', minutes: 30, label: 'system.module.query.periods.m30' },
  { value: '1h', minutes: 60, label: 'system.module.query.periods.h1' },
  { value: '6h', minutes: 360, label: 'system.module.query.periods.h6' },
  { value: '12h', minutes: 720, label: 'system.module.query.periods.h12' },
  { value: '24h', minutes: 1440, label: 'system.module.query.periods.h24' },
  { value: '7d', minutes: 10080, label: 'system.module.query.periods.d7' }
]
const moduleName = ref(props.initialModuleName)
const registeredHost = ref('')
const nodeName = ref('')
const role = ref('')
const defaultStatus = 'up'
const status = ref(defaultStatus)
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
    ...(applied.value.role ? { role: applied.value.role } : {}),
    ...(applied.value.status ? { status: applied.value.status } : {})
  }
  if (applied.value.period === 'custom') {
    params.registered_from = applied.value.from
    params.registered_to = applied.value.to
  } else {
    const preset = periods.find(item => item.value === applied.value.period)
    if (preset) {
      const now = Date.now()
      params.registered_from = new Date(now - preset.minutes * 60000).toISOString()
      params.registered_to = new Date(now).toISOString()
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
  error.value = ''
  try {
    const response = await modulesAPI.listInstances(currentParams())
    if (requestGeneration !== generation) return
    rows.value = response.data || []
    total.value = response.total || 0
  } catch (failure) {
    if (requestGeneration !== generation) return
    error.value = failure.response?.data?.error || failure.message || t('system.module.query.loadFailed')
  } finally {
    requestsInFlight -= 1
    if (requestGeneration === generation) loading.value = false
  }
}

function search() {
  clearTimeout(searchTimer)
  searchTimer = null
  if (period.value === 'custom' && !validCustomRange.value) {
    generation += 1
    loading.value = false
    error.value = ''
    rows.value = []
    total.value = 0
    return
  }
  applied.value = {
    moduleName: moduleName.value, registeredHost: registeredHost.value.trim(), nodeName: nodeName.value.trim(), role: role.value, status: status.value,
    period: period.value,
    ...(period.value === 'custom' ? {
      from: new Date(customRange.value[0]).toISOString(), to: new Date(customRange.value[1]).toISOString()
    } : {})
  }
  page.value = 1
  load()
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
  moduleName.value = ''
  registeredHost.value = ''
  nodeName.value = ''
  role.value = ''
  status.value = defaultStatus
  period.value = 'all'
  customRange.value = null
  emit('clear-selection')
  search()
}

function changePageSize() {
  page.value = 1
  load()
}

watch(() => props.initialModuleName, value => {
  if (moduleName.value === value) return
  moduleName.value = value
  search()
})
onMounted(() => {
  search()
  refreshTimer = window.setInterval(() => load({ silent: true }), 10000)
})
onUnmounted(() => {
  clearTimeout(searchTimer)
  window.clearInterval(refreshTimer)
  generation += 1
})
</script>

<style scoped>
.query-toolbar { display: flex; flex-wrap: wrap; gap: 10px; align-items: center; margin: 10px 0; }
.query-control { width: 175px; }
.query-range { max-width: 380px; }
.query-hint, .technical-name { color: var(--addp-text-secondary); font-size: 12px; }
.query-hint { margin: 0 0 12px; }
.query-error { margin-bottom: 12px; }
.query-pagination { justify-content: flex-end; margin-top: 16px; }
.instance-diagnostics { padding: 12px 18px; }
.instance-diagnostics :deep(.el-descriptions__content) { overflow-wrap: anywhere; }
.diagnostic-hint { color: var(--addp-text-secondary); font-size: 12px; margin: 10px 0 0; }
@media (max-width: 900px) { .instance-diagnostics :deep(.el-descriptions__body) { overflow-x: auto; } }
@media (max-width: 600px) { .query-control, .query-range { width: 100%; max-width: none; } }
</style>
