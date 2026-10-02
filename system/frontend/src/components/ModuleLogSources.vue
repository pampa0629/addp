<template>
  <div>
    <el-alert :title="t('system.module.sources.hint')" type="info" :closable="false" show-icon />
    <div class="source-controls">
      <el-select v-model="moduleName" clearable filterable allow-create :placeholder="t('system.module.query.module')" @change="search">
        <el-option v-for="module in modules" :key="module.module_name" :value="module.module_name" :label="resolveIAMModuleName(module.module_name, t, te)" />
      </el-select>
      <el-input v-model.trim="node" clearable :placeholder="t('system.module.sources.node')" @input="schedule" @keyup.enter="search" />
      <el-select v-model="role" clearable :placeholder="t('system.module.query.role')" @change="search">
        <el-option v-for="value in ['backend', 'worker', 'scheduler', 'ingress']" :key="value" :value="value" :label="t(`system.module.roles.${value}`)" />
      </el-select>
      <el-select v-model="period" :aria-label="t('system.module.sources.timeRange')" @change="search">
        <el-option value="all" :label="t('system.module.sources.allTimes')" />
        <el-option v-for="item in MODULE_INSTANCE_PERIODS" :key="item.value" :value="item.value" :label="t(item.label)" />
        <el-option value="custom" :label="t('system.module.query.custom')" />
      </el-select>
      <el-button @click="reset">{{ t('system.module.query.reset') }}</el-button>
      <el-button :loading="loading" @click="load">{{ t('system.module.refresh') }}</el-button>
    </div>
    <el-date-picker v-if="period === 'custom'" v-model="customRange" type="datetimerange" :start-placeholder="t('system.module.query.from')" :end-placeholder="t('system.module.query.to')" @change="search" />
    <el-alert v-if="error" :title="error" type="error" :closable="false" />
    <el-alert v-if="result && result.discovery_state === 'unknown'" :title="t('system.module.sources.coverageUnknown')" type="warning" :closable="false" />
    <p v-if="result?.discovery_issues?.length" class="source-hint">{{ t('system.module.sources.diagnosticScope') }}<span v-if="result.observation?.node"> · {{ result.observation.node }}</span></p>
    <ul v-if="result?.discovery_issues?.length" class="source-issues" data-testid="source-discovery-issues">
      <li v-for="issue in result.discovery_issues" :key="issue.code">
        <strong>{{ t(`system.module.sources.issues.${issue.code}.title`) }}</strong>
        <span v-if="issue.count"> · {{ t('system.module.sources.issueCount', { count: issue.count }) }}</span>
        <p>{{ t(`system.module.sources.issues.${issue.code}.action`) }}</p>
      </li>
    </ul>
    <p v-if="result?.observation" class="source-hint">{{ t('system.module.sources.sampledAt') }}：{{ date(result.observation.sampled_at) }}</p>
    <el-table :data="result?.data || []" v-loading="loading" border data-testid="module-log-sources">
      <el-table-column :label="t('system.module.columns.name')" min-width="170">
        <template #default="{ row }"><strong>{{ resolveIAMModuleName(row.module_name, t, te) }}</strong><div>{{ row.module_name }}</div></template>
      </el-table-column>
      <el-table-column prop="instance_id" :label="t('system.module.instances.id')" min-width="300" show-overflow-tooltip />
      <el-table-column :label="t('system.module.instances.role')" min-width="120"><template #default="{ row }">{{ t(`system.module.roles.${row.role}`) }}</template></el-table-column>
      <el-table-column prop="host_node_name" :label="t('system.module.instances.hostNodeName')" min-width="160" show-overflow-tooltip />
      <el-table-column :label="t('system.module.sources.captureStartedAt')" min-width="175"><template #default="{ row }">{{ date(row.capture_started_at) }}</template></el-table-column>
      <el-table-column :label="t('system.module.sources.observedAt')" min-width="175"><template #default="{ row }">{{ date(row.observed_at) }}</template></el-table-column>
      <el-table-column :label="t('system.module.sources.startupResult')" min-width="125"><template #default><el-tag type="info">{{ t('system.module.sources.unconfirmed') }}</el-tag></template></el-table-column>
      <el-table-column :label="t('system.module.logs.title')" min-width="110" fixed="right"><template #default="{ row }"><el-button text type="primary" @click="selected = row; open = true">{{ t('system.module.logs.view') }}</el-button></template></el-table-column>
    </el-table>
    <el-pagination v-if="result" v-model:current-page="page" v-model:page-size="pageSize" :page-sizes="[20, 50, 100]" :total="result.total" layout="total, sizes, prev, pager, next" @current-change="load" @size-change="search" />
    <ModuleRuntimeLogs v-model="open" :instance="selected" />
  </div>
</template>

<script setup>
import { onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { modulesAPI } from '../api/modules'
import { resolveIAMModuleName } from '../utils/iamPresentation'
import { MODULE_INSTANCE_PERIODS } from '../utils/routeState'
import ModuleRuntimeLogs from './ModuleRuntimeLogs.vue'
defineProps({ modules: { type: Array, default: () => [] } })
const { t, te } = useI18n()
const moduleName = ref(''), node = ref(''), role = ref(''), period = ref('all'), customRange = ref(null)
const page = ref(1), pageSize = ref(20), result = ref(null), error = ref(''), loading = ref(false)
const selected = ref(null), open = ref(false)
let generation = 0, controller, timer, poll
const durations = { '15m': 900000, '30m': 1800000, '1h': 3600000, '6h': 21600000, '12h': 43200000, '24h': 86400000, '7d': 604800000 }
function date(value) { return value ? new Date(value).toLocaleString() : '—' }
function invalidate() { generation++; controller?.abort(); clearTimeout(timer); timer = null; loading.value = false }
async function load() {
  if (document.hidden) return
  invalidate()
  const id = generation
  const params = { page: page.value, page_size: pageSize.value }
  if (moduleName.value) params.module_name = moduleName.value
  if (node.value) params.node_name = node.value
  if (role.value) params.role = role.value
  if (period.value !== 'all') {
    const now = Date.now()
    const range = period.value === 'custom' ? customRange.value?.map(value => new Date(value).getTime()) : [now - durations[period.value], now]
    if (!range || range.length !== 2 || !Number.isFinite(range[0]) || !Number.isFinite(range[1]) || range[0] >= range[1] || range[1] - range[0] > durations['7d']) { error.value = t('system.module.logs.invalidRange'); return }
    params.time_from = new Date(range[0]).toISOString(); params.time_to = new Date(range[1]).toISOString()
  }
  controller = new AbortController(); loading.value = true; error.value = ''
  try {
    const response = await modulesAPI.sources(params, controller.signal)
    if (id === generation) result.value = response
  } catch (e) {
    if (id === generation && e.code !== 'ERR_CANCELED') error.value = e.response?.data?.error || t('system.module.loadFailed')
  } finally { if (id === generation) loading.value = false }
}
function search() { invalidate(); page.value = 1; result.value = null; load() }
function schedule() { invalidate(); result.value = null; timer = setTimeout(search, 300) }
function reset() { moduleName.value = ''; node.value = ''; role.value = ''; period.value = 'all'; customRange.value = null; pageSize.value = 20; search() }
function visibility() { if (document.hidden) invalidate(); else load() }
onMounted(() => { load(); poll = setInterval(() => { if (!loading.value && !timer && !error.value) load() }, 10000); document.addEventListener('visibilitychange', visibility) })
onUnmounted(() => { invalidate(); clearInterval(poll); document.removeEventListener('visibilitychange', visibility) })
</script>

<style scoped>
.source-controls { display: grid; grid-template-columns: repeat(4, minmax(140px, 1fr)) auto auto; gap: 12px; margin: 16px 0; }
.source-hint { color: var(--addp-text-secondary); font-size: 12px; }
.source-issues { padding-left: 24px; font-size: 13px; }
.source-issues li { margin: 10px 0; }
.source-issues p { margin: 4px 0; color: var(--addp-text-secondary); }
.el-pagination { margin-top: 16px; }
@media (max-width: 1100px) { .source-controls { grid-template-columns: repeat(2, minmax(0, 1fr)); } }
@media (max-width: 600px) { .source-controls { grid-template-columns: 1fr; } }
</style>
