<template>
  <section class="iam-panel">
    <div class="audit-summary">
      <div v-for="item in summaryItems" :key="item.key" class="audit-metric">
        <span>{{ item.label }}</span><strong>{{ item.value }}</strong>
      </div>
    </div>

    <div class="iam-toolbar audit-toolbar">
      <div class="iam-filters audit-filters">
        <el-date-picker v-model="dateRange" type="datetimerange" :range-separator="t('system.iam.audit.to')" :start-placeholder="t('system.iam.audit.startTime')" :end-placeholder="t('system.iam.audit.endTime')" @change="applyFilters" />
        <el-input v-model="filters.event_name" :placeholder="t('system.iam.audit.eventName')" clearable @keyup.enter="applyFilters" @clear="applyFilters" />
        <el-select v-model="filters.result" :placeholder="t('system.iam.audit.result')" clearable @change="applyFilters"><el-option v-for="value in results" :key="value" :label="statusLabel(value)" :value="value" /></el-select>
        <el-select v-model="filters.risk_level" :placeholder="t('system.iam.audit.risk')" clearable @change="applyFilters"><el-option v-for="value in risks" :key="value" :label="statusLabel(value)" :value="value" /></el-select>
        <el-input v-model="filters.module_name" :placeholder="t('system.iam.audit.module')" clearable @keyup.enter="applyFilters" @clear="applyFilters" />
        <el-button :icon="Refresh" @click="load">{{ t('system.iam.common.refresh') }}</el-button>
      </div>
      <el-dropdown v-if="canExport" @command="exportEvents">
        <el-button :icon="Download">{{ t('system.iam.common.export') }}<el-icon class="el-icon--right"><ArrowDown /></el-icon></el-button>
        <template #dropdown><el-dropdown-menu><el-dropdown-item command="csv">CSV</el-dropdown-item><el-dropdown-item command="json">JSON</el-dropdown-item></el-dropdown-menu></template>
      </el-dropdown>
    </div>

    <el-table v-loading="loading" :data="rows" stripe @row-click="openDetail">
      <el-table-column prop="id" label="ID" width="90" />
      <el-table-column prop="event_name" :label="t('system.iam.audit.eventName')" min-width="250" show-overflow-tooltip />
      <el-table-column prop="module_name" :label="t('system.iam.audit.module')" width="120" />
      <el-table-column :label="t('system.iam.audit.result')" width="120"><template #default="{ row }"><el-tag :type="resultType(row.result)">{{ statusLabel(row.result) }}</el-tag></template></el-table-column>
      <el-table-column :label="t('system.iam.audit.risk')" width="120"><template #default="{ row }"><el-tag :type="riskType(row.risk_level)" effect="plain">{{ statusLabel(row.risk_level) }}</el-tag></template></el-table-column>
      <el-table-column prop="principal_id" :label="t('system.iam.audit.principal')" width="140" />
      <el-table-column prop="request_id" :label="t('system.iam.audit.requestId')" min-width="220" show-overflow-tooltip />
      <el-table-column :label="t('system.iam.audit.time')" width="180"><template #default="{ row }">{{ formatDate(row.created_at) }}</template></el-table-column>
    </el-table>

    <el-pagination v-model:current-page="page" v-model:page-size="pageSize" class="iam-pagination" :total="total" :page-sizes="[10, 20, 50]" layout="total, sizes, prev, pager, next" @current-change="changePage" @size-change="applyFilters" />

    <el-drawer v-model="detailVisible" :title="t('system.iam.audit.detail')" size="min(620px, 92vw)">
      <el-descriptions v-if="selected" :column="1" border>
        <el-descriptions-item label="ID">{{ selected.id }}</el-descriptions-item>
        <el-descriptions-item :label="t('system.iam.audit.eventName')">{{ selected.event_name }}</el-descriptions-item>
        <el-descriptions-item :label="t('system.iam.audit.principal')">{{ selected.principal_type || '-' }} / {{ selected.principal_id || '-' }}</el-descriptions-item>
        <el-descriptions-item :label="t('system.iam.audit.context')">{{ selected.context_type || '-' }} / {{ selected.tenant_id || '-' }}</el-descriptions-item>
        <el-descriptions-item :label="t('system.iam.audit.request')">{{ selected.http_method || '-' }} {{ selected.resource_path || '-' }} / {{ selected.http_status || '-' }}</el-descriptions-item>
        <el-descriptions-item :label="t('system.iam.audit.source')">{{ selected.ip_address || '-' }}</el-descriptions-item>
        <el-descriptions-item :label="t('system.iam.audit.time')">{{ formatDate(selected.created_at) }}</el-descriptions-item>
      </el-descriptions>
      <pre v-if="selected" class="audit-details-json">{{ JSON.stringify(selected.details || {}, null, 2) }}</pre>
    </el-drawer>
  </section>
</template>

<script setup>
import { computed, reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { ArrowDown, Download, Refresh } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { iamAPI } from '../../api/iam'
import { useAuthStore } from '../../store/auth'
import { navigateSystemRoute } from '../../utils/moduleNavigation'

const props = defineProps({ scope: { type: String, required: true } })
const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const authStore = useAuthStore()
const exportPermission = computed(() => props.scope === 'platform' ? 'audit.event.export' : 'audit.tenant_event.export')
const canExport = computed(() => authStore.hasPermission(exportPermission.value))
const results = ['succeeded', 'failed', 'denied', 'ignored']
const risks = ['low', 'medium', 'high', 'critical']
const rows = ref([])
const summary = ref({ total: 0, succeeded: 0, failed: 0, denied: 0, ignored: 0, high_risk: 0 })
const loading = ref(false)
const page = ref(normalizePage(route.query.page))
const pageSize = ref(20)
const total = ref(0)
const filters = reactive({
  event_name: String(route.query.event_name || ''),
  result: String(route.query.result || ''),
  risk_level: String(route.query.risk_level || ''),
  module_name: String(route.query.module_name || ''),
  entity_type: String(route.query.entity_type || ''),
  entity_id: String(route.query.entity_id || '')
})
const dateRange = ref(null)
const detailVisible = ref(false)
const selected = ref(null)
const summaryItems = computed(() => ['total', 'succeeded', 'failed', 'denied', 'high_risk'].map((key) => ({ key, label: t(`system.iam.audit.summary.${key}`), value: summary.value[key] || 0 })))

function statusLabel(status) { return t(`system.iam.status.${status}`) }
function resultType(result) { return ({ succeeded: 'success', failed: 'danger', denied: 'warning', ignored: 'info' })[result] || 'info' }
function riskType(risk) { return ({ low: 'success', medium: 'warning', high: 'danger', critical: 'danger' })[risk] || 'info' }
function formatDate(value) { return value ? new Date(value).toLocaleString() : '-' }
function normalizePage(value) {
  const parsed = Number(value)
  return Number.isInteger(parsed) && parsed > 0 ? parsed : 1
}
function requestParams() {
  return {
    ...filters,
    start_time: dateRange.value?.[0]?.toISOString(),
    end_time: dateRange.value?.[1]?.toISOString()
  }
}
async function load() {
  loading.value = true
  try {
    const params = requestParams()
    const [listResult, summaryResult] = await Promise.all([
      iamAPI.audit.list(props.scope, { page: page.value, page_size: pageSize.value, ...params }),
      iamAPI.audit.summary(props.scope, params)
    ])
    rows.value = listResult.data || []; total.value = listResult.total || 0; summary.value = summaryResult
  } catch (error) { ElMessage.error(error.response?.data?.error || t('system.iam.common.loadFailed')) }
  finally { loading.value = false }
}
function buildRouteQuery() {
  const query = { tab: String(route.query.tab || '') }
  for (const key of ['event_name', 'result', 'risk_level', 'module_name', 'entity_type', 'entity_id']) {
    const value = String(filters[key] || '').trim()
    if (value) query[key] = value
  }
  if (page.value > 1) query.page = String(page.value)
  return query
}
async function applyFilters() {
  page.value = 1
  const query = buildRouteQuery()
  const unchanged = Object.keys(route.query).length === Object.keys(query).length &&
    Object.keys(query).every(key => String(route.query[key] || '') === String(query[key]))
  if (unchanged) {
    await load()
    return
  }
  await navigateSystemRoute(router, { name: 'IAMWorkbench', query }, { history: 'replace' })
}
async function changePage(nextPage) {
  page.value = nextPage
  await navigateSystemRoute(router, {
    name: 'IAMWorkbench',
    query: buildRouteQuery()
  }, { history: 'replace' })
}
function openDetail(row) { selected.value = row; detailVisible.value = true }
async function exportEvents(format) {
  try {
    const blob = await iamAPI.audit.export(props.scope, { ...requestParams(), format })
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url; link.download = `addp-audit-${props.scope}.${format}`; link.click()
    URL.revokeObjectURL(url)
  } catch (error) { ElMessage.error(error.response?.data?.error || t('system.iam.common.exportFailed')) }
}
watch(() => route.query, async query => {
  for (const key of ['event_name', 'result', 'risk_level', 'module_name', 'entity_type', 'entity_id']) {
    filters[key] = String(query[key] || '')
  }
  page.value = normalizePage(query.page)
  await load()
}, { immediate: true })
</script>

<style scoped>
.audit-summary { display: grid; grid-template-columns: repeat(5, minmax(120px, 1fr)); gap: 12px; margin-bottom: 18px; }
.audit-metric { min-height: 76px; padding: 14px 16px; border: 1px solid var(--addp-border-color); border-radius: 6px; background: var(--addp-bg-primary); }
.audit-metric span { display: block; color: var(--addp-text-secondary); font-size: 13px; }
.audit-metric strong { display: block; margin-top: 7px; color: var(--addp-text-primary); font-size: 24px; font-weight: 600; }
.audit-filters { flex-wrap: wrap; }
.audit-details-json { margin-top: 16px; padding: 14px; overflow: auto; border: 1px solid var(--addp-border-color); border-radius: 4px; background: var(--addp-bg-secondary); color: var(--addp-text-primary); }
@media (max-width: 900px) { .audit-summary { grid-template-columns: repeat(2, minmax(120px, 1fr)); } }
</style>
