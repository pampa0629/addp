<template>
  <div>
    <div class="page-header">
      <h2>{{ t('quality.issue.title') }}</h2>
    </div>

    <el-form :inline="true" style="margin-bottom:16px">
      <el-form-item :label="t('quality.issue.statusFilter')">
        <el-select v-model="filter.status" clearable :placeholder="t('quality.issue.allStatus')" @change="applyFilters" style="width:120px">
          <el-option :label="t('quality.issue.open')" value="open" />
          <el-option :label="t('quality.issue.resolved')" value="resolved" />
          <el-option :label="t('quality.issue.ignored')" value="ignored" />
        </el-select>
      </el-form-item>
      <el-form-item :label="t('quality.issue.engineFilter')">
        <el-select
          v-model="filter.engine_id"
          clearable
          :placeholder="t('quality.issue.allEngines')"
          style="width:180px"
          @change="applyFilters"
        >
          <el-option v-for="engine in engines" :key="engine.id" :label="engine.name" :value="engine.id" />
        </el-select>
      </el-form-item>
    </el-form>

    <el-table :data="list" v-loading="loading" border>
      <el-table-column prop="id" :label="t('quality.issue.id')" width="80" />
      <el-table-column prop="type" :label="t('quality.issue.ruleType')" width="120" />
      <el-table-column prop="table_name" :label="t('quality.issue.tableName')" width="160" />
      <el-table-column prop="column_name" :label="t('quality.issue.column')" width="130" />
      <el-table-column :label="t('quality.issue.passRate')" width="100">
        <template #default="{ row }">{{ row.pass_rate?.toFixed(1) }}%</template>
      </el-table-column>
      <el-table-column prop="failed_count" :label="t('quality.issue.failedCount')" width="100" />
      <el-table-column :label="t('quality.issue.relatedExecutions')" min-width="320">
        <template #default="{ row }">
          <div class="execution-links">
            <div class="execution-link-row">
              <span class="execution-link-label">{{ t('quality.issue.firstExecution') }}</span>
              <el-button
                v-if="issueExecutionRoute(row.execution_id)"
                class="execution-link"
                type="primary"
                link
                @click="openExecution(row.execution_id)"
              >
                {{ row.execution_id }}
              </el-button>
              <span v-else>-</span>
            </div>
            <div class="execution-link-row">
              <span class="execution-link-label">{{ t('quality.issue.lastExecution') }}</span>
              <el-button
                v-if="issueExecutionRoute(row.last_execution_id)"
                class="execution-link"
                type="primary"
                link
                @click="openExecution(row.last_execution_id)"
              >
                {{ row.last_execution_id }}
              </el-button>
              <span v-else>-</span>
            </div>
          </div>
        </template>
      </el-table-column>
      <el-table-column prop="status" :label="t('quality.issue.status')" width="100">
        <template #default="{ row }">
          <el-tag :type="statusTagType(row.status)">{{ statusLabel(row.status) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="created_at" :label="t('quality.issue.createdAt')" width="180">
        <template #default="{ row }">{{ new Date(row.created_at).toLocaleString() }}</template>
      </el-table-column>
      <el-table-column :label="t('quality.issue.actions')" width="220" fixed="right">
        <template #default="{ row }">
          <el-button size="small" @click="openIssue(row.id)">{{ t('quality.issue.detail') }}</el-button>
          <el-button v-if="row.status === 'open'" size="small" type="success" @click="changeStatus(row.id, 'resolved')">{{ t('quality.issue.markResolved') }}</el-button>
          <el-button v-if="row.status === 'open'" size="small" @click="changeStatus(row.id, 'ignored')">{{ t('quality.issue.ignore') }}</el-button>
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
import { ref, onMounted, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { issueAPI, systemEngineAPI } from '../api/quality'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { navigateQualityRoute } from '../utils/moduleNavigation'
import { issueDetailRoute, issueExecutionRoute } from '../utils/issueNavigation'
import { buildIssueListRouteQuery, resolveIssueListRouteState } from '../utils/issueListRouteState'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()

const list = ref([])
const loading = ref(false)
const filter = ref({ status: '', engine_id: null })
const pagination = ref({ page: 1, page_size: 20, total: 0 })
const engines = ref([])
let routeReady = false

const statusTagType = (s) => ({ open: 'danger', resolved: 'success', ignored: 'info' }[s] || 'info')
const statusLabel = (s) => ({
  open: t('quality.issue.open'),
  resolved: t('quality.issue.resolved'),
  ignored: t('quality.issue.ignored')
}[s] || s)

const fetchList = async () => {
  loading.value = true
  try {
    const params = { page: pagination.value.page, page_size: pagination.value.page_size }
    if (filter.value.status) params.status = filter.value.status
    if (filter.value.engine_id) params.engine_id = filter.value.engine_id
    const res = await issueAPI.list(params)
    list.value = res?.data || []
    pagination.value.total = res?.total || 0
    const lastPage = Math.max(1, Math.ceil(pagination.value.total / pagination.value.page_size))
    if (pagination.value.page > lastPage) {
      pagination.value.page = lastPage
      await syncRoute()
      await fetchList()
      return
    }
  } catch (e) {
    ElMessage.error(e.response?.data?.error || t('quality.issue.loadFailed'))
  } finally {
    loading.value = false
  }
}

const buildRouteQuery = () => buildIssueListRouteQuery({
  status: filter.value.status,
  engineID: filter.value.engine_id,
  page: pagination.value.page,
  pageSize: pagination.value.page_size
})

const syncRoute = () => navigateQualityRoute(router, {
  path: '/issues',
  query: buildRouteQuery()
}, { history: 'replace' })

const applyFilters = async () => {
  pagination.value.page = 1
  await syncRoute()
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

const openExecution = (executionId) => {
  const location = issueExecutionRoute(executionId)
  if (location) navigateQualityRoute(router, location)
}

const openIssue = (issueId) => {
  const location = issueDetailRoute(issueId, route.query)
  if (location) navigateQualityRoute(router, location)
}

const changeStatus = async (id, status) => {
  try {
    const { value: note } = await ElMessageBox.prompt(t('quality.issue.notePrompt'), t('quality.issue.noteTitle'), {
      inputPattern: /\S+/,
      inputErrorMessage: t('quality.issue.noteRequired'),
      confirmButtonText: t('quality.issue.confirm'),
      cancelButtonText: t('quality.issue.cancel')
    })
    await issueAPI.updateStatus(id, status, note)
    ElMessage.success(t('quality.issue.updateSuccess'))
    await fetchList()
  } catch (e) {
    if (e === 'cancel' || e === 'close') return
    ElMessage.error(e.response?.data?.error || t('quality.issue.updateFailed'))
  }
}

const applyRouteState = (query) => {
  const state = resolveIssueListRouteState(query)
  filter.value.status = state.status
  filter.value.engine_id = state.engineID
  pagination.value.page = state.page
  pagination.value.page_size = state.pageSize
  return state
}

const restoreListFromRoute = async (query) => {
  if (!routeReady) return
  const state = applyRouteState(query)
  if (state.changed) {
    await navigateQualityRoute(router, { path: '/issues', query: state.query }, { history: 'replace' })
    return
  }
  await fetchList()
}

const fetchEngines = async () => {
  try {
    const result = await systemEngineAPI.list()
    engines.value = (result || []).filter(engine => engine.engine_type === 'postgresql' && engine.lifecycle_state === 'active')
  } catch {
    engines.value = []
  }
}

watch(() => route.query, restoreListFromRoute, { deep: true })

onMounted(async () => {
  const state = applyRouteState(route.query)
  if (state.changed) {
    await navigateQualityRoute(router, { path: '/issues', query: state.query }, { history: 'replace' })
  }
  await Promise.all([fetchEngines(), fetchList()])
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
.execution-links {
  display: grid;
  gap: 4px;
}
.execution-link-row {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr);
  gap: 8px;
  align-items: center;
}
.execution-link-label {
  color: var(--addp-text-secondary);
  white-space: nowrap;
}
.execution-link {
  min-width: 0;
  justify-content: flex-start;
  padding: 0;
}
.execution-link :deep(span) {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
