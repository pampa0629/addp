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

    <el-table :data="list" v-loading="loading" border>
      <el-table-column prop="execution_id" :label="t('quality.execution.executionId')" min-width="250" show-overflow-tooltip />
      <el-table-column prop="source_task_name" :label="t('quality.execution.taskName')" width="200" />
      <el-table-column prop="status" :label="t('quality.execution.status')" width="100">
        <template #default="{ row }">
          <el-tag :type="statusType(row.status)">{{ row.status }}</el-tag>
        </template>
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
      @size-change="fetchList"
      @current-change="fetchList"
    />
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { executionAPI } from '../api/quality'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { navigateQualityRoute } from '../utils/moduleNavigation'

const { t } = useI18n()
const router = useRouter()

const list = ref([])
const loading = ref(false)
const pagination = ref({ page: 1, page_size: 20, total: 0 })
const statusFilter = ref('')

const statusType = (status) => {
  const map = { success: 'success', failed: 'danger', running: 'warning', pending: 'info' }
  return map[status] || 'info'
}

const openExecution = (executionId) => {
  navigateQualityRoute(router, `/executions/${executionId}`)
}

const fetchList = async () => {
  loading.value = true
  try {
    const res = await executionAPI.list({ page: pagination.value.page, page_size: pagination.value.page_size, ...(statusFilter.value ? { status: statusFilter.value } : {}) })
    list.value = res.data || []
    pagination.value.total = res.total || 0
  } catch (e) {
    ElMessage.error(e.response?.data?.error || t('quality.execution.loadFailed'))
  } finally {
    loading.value = false
  }
}

const changeStatusFilter = () => {
  pagination.value.page = 1
  fetchList()
}

onMounted(fetchList)
</script>

<style scoped>
.page-header {
  margin-bottom: 16px;
}
.pagination {
  margin-top: 16px;
  justify-content: flex-end;
}
</style>
