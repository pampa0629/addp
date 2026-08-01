<template>
  <div class="alert-list">
    <el-tabs v-model="activeView" class="alert-tabs" @tab-change="handleTabChange">
      <el-tab-pane :label="t('monitor.alert.incident_tab')" name="incidents">
        <el-card>
      <template #header><span class="page-title">{{ t('monitor.alert.title') }}</span></template>
      <el-form :inline="true" class="filter-form">
        <el-form-item :label="t('monitor.alert.status')">
          <el-select v-model="filters.status" clearable style="width: 150px">
            <el-option :label="t('monitor.alert.status_values.open')" value="open" />
            <el-option :label="t('monitor.alert.status_values.acknowledged')" value="acknowledged" />
            <el-option :label="t('monitor.alert.status_values.resolved')" value="resolved" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('monitor.alert.severity')">
          <el-select v-model="filters.severity" clearable style="width: 150px">
            <el-option :label="t('monitor.alert.severity_values.critical')" value="critical" />
            <el-option :label="t('monitor.alert.severity_values.warning')" value="warning" />
          </el-select>
        </el-form-item>
        <el-button type="primary" @click="search">{{ t('monitor.execution.filter.search') }}</el-button>
      </el-form>

      <el-table v-loading="loading" :data="alerts" stripe>
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column :label="t('monitor.alert.signal')" min-width="230">
          <template #default="{ row }">
            <el-tag :type="row.severity === 'critical' ? 'danger' : 'warning'" size="small">
              {{ signalText(row.signal_code) }}
            </el-tag>
            <div v-if="row.rule_name" class="rule-name">{{ row.rule_name }}</div>
          </template>
        </el-table-column>
        <el-table-column :label="t('monitor.alert.status')" width="130">
          <template #default="{ row }">{{ alertStatusText(row.status) }}</template>
        </el-table-column>
        <el-table-column prop="module" :label="t('monitor.table.module')" width="110" />
        <el-table-column prop="source_task_id" :label="t('monitor.table.source_task_id')" width="130" />
        <el-table-column :label="t('monitor.alert.opened_at')" width="180">
          <template #default="{ row }">{{ formatDate(row.opened_at) }}</template>
        </el-table-column>
        <el-table-column :label="t('monitor.alert.last_observed_at')" width="180">
          <template #default="{ row }">{{ formatDate(row.last_observed_at) }}</template>
        </el-table-column>
        <el-table-column :label="t('monitor.alert.actions')" width="260" fixed="right">
          <template #default="{ row }">
            <el-button text type="primary" @click="openExecution(row)">{{ t('monitor.alert.view_execution') }}</el-button>
            <el-button v-if="row.status === 'open'" text @click="acknowledge(row)">{{ t('monitor.alert.acknowledge') }}</el-button>
            <el-button v-if="row.status !== 'resolved'" text @click="suppress(row)">{{ t('monitor.alert.suppress_one_hour') }}</el-button>
          </template>
        </el-table-column>
      </el-table>
      <el-pagination
        v-model:current-page="pagination.page" v-model:page-size="pagination.pageSize"
        :total="pagination.total" layout="total, prev, pager, next" class="pagination"
        @current-change="loadAlerts"
      />
        </el-card>
      </el-tab-pane>
      <el-tab-pane :label="t('monitor.alert.rule_tab')" name="rules">
        <AlertRuleList v-if="activeView === 'rules'" />
      </el-tab-pane>
    </el-tabs>
  </div>
</template>

<script setup>
import { onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { acknowledgeAlert, listAlerts, suppressAlert } from '@/api/monitor'
import AlertRuleList from './AlertRuleList.vue'
import { navigateMonitorRoute } from '@/utils/moduleNavigation'
import { resolveMonitorTabRouteState } from '@/utils/tabRouteState'

const { t } = useI18n()
const router = useRouter()
const route = useRoute()
const resolveRouteState = routeQuery => resolveMonitorTabRouteState(routeQuery, ['incidents', 'rules'], 'incidents')
const activeView = ref(resolveRouteState(route.query).tab)
const loading = ref(false)
const alerts = ref([])
const filters = reactive({ status: '', severity: '' })
const pagination = reactive({ page: 1, pageSize: 20, total: 0 })
let timer = null

async function loadAlerts() {
  loading.value = true
  try {
    const response = await listAlerts({ ...filters, page: pagination.page, page_size: pagination.pageSize })
    alerts.value = response.data || []
    pagination.total = response.total || 0
  } finally { loading.value = false }
}

function search() { pagination.page = 1; loadAlerts() }
function alertStatusText(status) { return status ? t(`monitor.alert.status_values.${status}`) : '-' }
function signalText(code) {
  if (['last_terminal_failed', 'last_terminal_timeout', 'consecutive_failures'].includes(code)) {
    return t(`monitor.alert_rule.rule_types.${code}`)
  }
  const key = `monitor.execution.detail.continuous.signals.${code}.title`
  const translated = t(key)
  return translated === key ? code : translated
}
function formatDate(value) { return value ? new Date(value).toLocaleString() : '-' }
function openExecution(row) { navigateMonitorRoute(router, { path: '/executions', query: { execution_id: row.execution_id } }) }
async function handleTabChange(tab) {
  const routeState = resolveRouteState({ tab })
  const location = { path: route.path, query: routeState.query }
  if (router.resolve(location).fullPath !== route.fullPath) {
    await navigateMonitorRoute(router, location, { history: 'replace' })
  }
}
async function restoreTabFromRoute() {
  const routeState = resolveRouteState(route.query)
  activeView.value = routeState.tab
  if (routeState.changed) {
    await navigateMonitorRoute(router, { path: route.path, query: routeState.query }, { history: 'replace' })
  }
}
async function acknowledge(row) {
  await acknowledgeAlert(row.id)
  ElMessage.success(t('monitor.alert.acknowledged'))
  await loadAlerts()
}
async function suppress(row) {
  await ElMessageBox.confirm(t('monitor.alert.suppress_confirm'), t('monitor.alert.suppress_one_hour'), { type: 'warning' })
  await suppressAlert(row.id, new Date(Date.now() + 60 * 60 * 1000).toISOString())
  ElMessage.success(t('monitor.alert.suppressed'))
  await loadAlerts()
}
onMounted(async () => {
  await restoreTabFromRoute()
  await loadAlerts()
  timer = window.setInterval(loadAlerts, 15000)
})
onBeforeUnmount(() => { if (timer) window.clearInterval(timer) })
watch(() => route.query, restoreTabFromRoute)
</script>

<style scoped>
.alert-list { min-height: 100%; background: var(--addp-bg-secondary); }
.alert-tabs { padding: 12px 20px 0; }
.alert-tabs :deep(.el-tabs__content) { margin: 0 -20px; }
.alert-tabs :deep(.el-tab-pane > .el-card) { margin: 0 20px 20px; }
.page-title { color: var(--addp-text-primary); font-weight: 500; font-size: 16px; }
.filter-form { margin-bottom: 16px; }
.pagination { margin-top: 20px; justify-content: flex-end; }
.rule-name { margin-top: 5px; color: var(--addp-text-secondary); font-size: 12px; }
</style>
