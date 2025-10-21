<template>
  <div class="execution-list">
    <el-card>
      <template #header>执行记录</template>
      <el-table :data="executions" v-loading="loading">
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column prop="task_id" label="任务ID" width="100" />
        <el-table-column prop="status" label="状态" width="100">
          <template #default="{ row }">
            <el-tag :type="getStatusType(row.status)">{{ row.status }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="records_written" label="已处理" width="120" />
        <el-table-column prop="start_time" label="开始时间" width="180" />
        <el-table-column label="操作" width="150">
          <template #default="{ row }">
            <el-button size="small" @click="viewDetail(row.id)">详情</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { executionAPI } from '@/api/tasks'

const router = useRouter()
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
  return types[status] || ''
}

onMounted(() => {
  loadExecutions()
})
</script>

<style scoped>
.execution-list { padding: 20px; }
</style>
