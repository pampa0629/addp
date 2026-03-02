<template>
  <div>
    <div class="page-header">
      <h2>执行记录</h2>
    </div>

    <el-table :data="list" v-loading="loading" border>
      <el-table-column prop="execution_id" label="执行 ID" min-width="250" show-overflow-tooltip />
      <el-table-column prop="source_task_name" label="任务名称" width="200" />
      <el-table-column prop="status" label="状态" width="100">
        <template #default="{ row }">
          <el-tag :type="statusType(row.status)">{{ row.status }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="质量评分" width="120">
        <template #default="{ row }">
          <span v-if="row.result?.quality_score != null">{{ row.result.quality_score.toFixed(1) }}%</span>
          <span v-else>-</span>
        </template>
      </el-table-column>
      <el-table-column prop="execution_time_ms" label="耗时(ms)" width="100" />
      <el-table-column prop="created_at" label="创建时间" width="180">
        <template #default="{ row }">{{ new Date(row.created_at).toLocaleString() }}</template>
      </el-table-column>
      <el-table-column label="操作" width="100">
        <template #default="{ row }">
          <el-button size="small" @click="$router.push(`/quality/executions/${row.execution_id}`)">详情</el-button>
        </template>
      </el-table-column>
    </el-table>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { executionAPI } from '../api/quality'

const list = ref([])
const loading = ref(false)

const statusType = (status) => {
  const map = { success: 'success', failed: 'danger', running: 'warning', pending: 'info' }
  return map[status] || 'info'
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
