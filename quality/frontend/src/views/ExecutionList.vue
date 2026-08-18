<template>
  <div>
    <div class="page-header">
      <h2>{{ t('quality.execution.title') }}</h2>
      <el-select v-model="statusFilter" :placeholder="t('quality.execution.statusFilter')" clearable style="width:160px" @change="changeStatusFilter">
        <el-option :label="t('quality.execution.pending')" value="pending" />
        <el-option :label="t('quality.execution.running')" value="running" />
        <el-option :label="t('quality.execution.success')" value="success" />
        <el-option :label="t('quality.execution.failed')" value="failed" />
        <el-option :label="t('quality.execution.timeout')" value="timeout" />
        <el-option :label="t('quality.execution.cancelled')" value="cancelled" />
      </el-select>
    </div>

    <el-alert v-if="loadError" :title="loadError" type="error" show-icon :closable="false" class="load-error" />
    <el-table :data="list" v-loading="loading" :empty-text="emptyText" border>
      <el-table-column prop="execution_id" :label="t('quality.execution.executionId')" min-width="250" show-overflow-tooltip />
      <el-table-column prop="source_task_name" :label="t('quality.execution.taskName')" width="200" />
      <el-table-column prop="status" :label="t('quality.execution.status')" width="100">
        <template #default="{ row }">
          <el-tag :type="statusType(row.status)">{{ statusLabel(row.status) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column :label="t('quality.execution.failureReason')" min-width="210" show-overflow-tooltip>
        <template #default="{ row }">{{ executionFailureLabel(row, t) || '-' }}</template>
      </el-table-column>
      <el-table-column :label="t('quality.execution.qualityScore')" width="120">
        <template #default="{ row }">
          <span v-if="row.metadata?.schema_version === 'addp.quality.execution-result/v1'">{{ Number(row.metadata.quality_score).toFixed(1) }}%</span>
          <span v-else>-</span>
        </template>
      </el-table-column>
      <el-table-column prop="execution_time_ms" :label="t('quality.execution.duration')" width="100" />
      <el-table-column prop="created_at" :label="t('quality.execution.createdAt')" width="180">
        <template #default="{ row }">{{ new Date(row.created_at).toLocaleString() }}</template>
      </el-table-column>
      <el-table-column :label="t('quality.execution.actions')" width="100">
        <template #default="{ row }">
          <el-button size="small" @click="openExecution(row.execution_id)">{{ t('quality.execution.detail') }}</el-button>
        </template>
      </el-table-column>
    </el-table>
    <el-pagination
      v-model:current-page="pagination.page"
      v-model:page-size="pagination.page_size"
      :page-sizes="[20, 50, 100]"
      layout="total, sizes, prev, pager, next"
      :total="pagination.total"
      class="pagination"
      @size-change="changePageSize"
      @current-change="changePage"
    />
  </div>
</template>

<script setup>
import { computed, ref, onMounted, watch } from 'vue'
import { executionAPI } from '../api/quality'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { navigateQualityRoute } from '../utils/moduleNavigation'
import { executionDetailRoute } from '../utils/executionNavigation'
import { buildExecutionListRouteQuery, resolveExecutionListRouteState } from '../utils/executionListRouteState'
import { executionFailureLabel } from '../utils/executionFailure'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()

const list = ref([])
const loading = ref(false)
const loadError = ref('')
const pagination = ref({ page: 1, page_size: 20, total: 0 })
const statusFilter = ref('')
let listRequestSequence = 0
let routeReady = false
const emptyText = computed(() => t(statusFilter.value
  ? 'quality.execution.filteredEmpty'
  : 'quality.execution.empty'))

const statusType = (status) => {
  const map = { success: 'success', failed: 'danger', timeout: 'danger', running: 'warning', pending: 'info' }
  return map[status] || 'info'
}
const statusLabel = (status) => ({
  pending: t('quality.execution.pending'),
  running: t('quality.execution.running'),
  success: t('quality.execution.success'),
  failed: t('quality.execution.failed'),
  timeout: t('quality.execution.timeout'),
  cancelled: t('quality.execution.cancelled')
}[status] || status)

const openExecution = (executionId) => {
  const location = executionDetailRoute(executionId, route.query)
  if (location) navigateQualityRoute(router, location)
}

const fetchList = async () => {
  const requestSequence = ++listRequestSequence
  loading.value = true
  loadError.value = ''
  try {
    const res = await executionAPI.list({ page: pagination.value.page, page_size: pagination.value.page_size, ...(statusFilter.value ? { status: statusFilter.value } : {}) })
    if (requestSequence !== listRequestSequence) return
    list.value = res.data || []
    pagination.value.total = res.total || 0
    const lastPage = Math.max(1, Math.ceil(pagination.value.total / pagination.value.page_size))
    if (pagination.value.page > lastPage) {
      pagination.value.page = lastPage
      await syncRoute()
      return
    }
  } catch (e) {
    if (requestSequence !== listRequestSequence) return
    list.value = []
    pagination.value.total = 0
    loadError.value = e.response?.data?.error || t('quality.execution.loadFailed')
    ElMessage.error(loadError.value)
  } finally {
    if (requestSequence === listRequestSequence) loading.value = false
  }
}

const changeStatusFilter = () => {
  pagination.value.page = 1
  syncRoute()
}

const changePage = async (page) => {
  pagination.value.page = page
  await syncRoute()
}

const changePageSize = async (pageSize) => {
  pagination.value.page_size = pageSize
  pagination.value.page = 1
  await syncRoute()
}

const buildRouteQuery = () => buildExecutionListRouteQuery({
  status: statusFilter.value,
  page: pagination.value.page,
  pageSize: pagination.value.page_size
})

const syncRoute = () => navigateQualityRoute(router, {
  path: '/executions',
  query: buildRouteQuery()
}, { history: 'replace' })

const applyRouteState = (query) => {
  const state = resolveExecutionListRouteState(query)
  statusFilter.value = state.status
  pagination.value.page = state.page
  pagination.value.page_size = state.pageSize
  return state
}

const restoreListFromRoute = async (query) => {
  if (!routeReady) return
  const state = applyRouteState(query)
  if (state.changed) {
    await navigateQualityRoute(router, { path: '/executions', query: state.query }, { history: 'replace' })
    return
  }
  await fetchList()
}

watch(() => route.query, restoreListFromRoute, { deep: true })

onMounted(async () => {
  const state = applyRouteState(route.query)
  if (state.changed) {
    await navigateQualityRoute(router, { path: '/executions', query: state.query }, { history: 'replace' })
  }
  await fetchList()
  routeReady = true
})
</script>

<style scoped>
.page-header {
  margin-bottom: 16px;
}
.pagination {
  margin-top: 16px;
  justify-content: flex-end;
}
.load-error {
  margin-bottom: 16px;
}
</style>
