<template>
  <div>
    <div class="page-header">
      <h2>{{ t('quality.execution.title') }}</h2>
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
          <span v-if="row.result?.quality_score != null">{{ row.result.quality_score.toFixed(1) }}%</span>
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
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { executionAPI } from '../api/quality'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { navigateQualityRoute } from '../utils/moduleNavigation'

const { t } = useI18n()
const router = useRouter()

const list = ref([])
const loading = ref(false)

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
    const res = await executionAPI.list()
    list.value = res.data || []
  } finally {
    loading.value = false
  }
}

onMounted(fetchList)
</script>

<style scoped>
.page-header {
  margin-bottom: 16px;
}
</style>
