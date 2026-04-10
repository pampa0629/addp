<template>
  <div class="execution-list">
    <el-card>
      <template #header>{{ t('transfer.executionList.title') }}</template>
      <el-table :data="executions" v-loading="loading">
        <el-table-column prop="id" :label="t('transfer.executionList.id')" width="80" />
        <el-table-column prop="task_id" :label="t('transfer.executionList.taskId')" width="100" />
        <el-table-column prop="status" :label="t('transfer.executionList.status')" width="100">
          <template #default="{ row }">
            <el-tag :type="getStatusType(row.status)">{{ row.status }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="records_written" :label="t('transfer.executionList.processed')" width="120" />
        <el-table-column prop="start_time" :label="t('transfer.executionList.startTime')" width="180" />
        <el-table-column :label="t('transfer.executionList.actions')" width="150">
          <template #default="{ row }">
            <el-button size="small" @click="viewDetail(row.id)">{{ t('transfer.executionList.detail') }}</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { executionAPI } from '@/api/tasks'

const router = useRouter()
const { t } = useI18n()
const loading = ref(false)
const executions = ref([])

const loadExecutions = async () => {
  loading.value = true
  try {
    const res = await executionAPI.list({ page: 1, page_size: 50 })
    executions.value = res.data || []
  } finally {
    loading.value = false
  }
}

const viewDetail = (id) => {
  router.push(`/executions/${id}`)
}

const getStatusType = (status) => {
  const types = { pending: 'info', running: 'primary', success: 'success', failed: 'danger' }
  return types[status] || 'info'
}

onMounted(() => {
  loadExecutions()
})
</script>

<style scoped>
.execution-list { padding: 20px; }
</style>
