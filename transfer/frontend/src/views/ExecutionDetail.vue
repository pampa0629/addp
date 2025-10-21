<template>
  <div class="execution-detail">
    <el-button @click="$router.back()" style="margin-bottom: 20px;">返回</el-button>
    <el-card v-loading="loading">
      <template #header>执行详情 #{{ execution.id }}</template>
      <el-descriptions :column="2" border>
        <el-descriptions-item label="执行ID">{{ execution.id }}</el-descriptions-item>
        <el-descriptions-item label="任务ID">{{ execution.task_id }}</el-descriptions-item>
        <el-descriptions-item label="状态">
          <el-tag :type="getStatusType(execution.status)">{{ execution.status }}</el-tag>
        </el-descriptions-item>
        <el-descriptions-item label="触发方式">{{ execution.trigger_type }}</el-descriptions-item>
        <el-descriptions-item label="已读取记录">{{ execution.records_read }}</el-descriptions-item>
        <el-descriptions-item label="已写入记录">{{ execution.records_written }}</el-descriptions-item>
        <el-descriptions-item label="开始时间">{{ execution.start_time }}</el-descriptions-item>
        <el-descriptions-item label="结束时间">{{ execution.end_time || '-' }}</el-descriptions-item>
      </el-descriptions>

      <el-divider>执行日志</el-divider>
      <el-input v-model="logs" type="textarea" :rows="20" readonly />
    </el-card>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { executionAPI } from '@/api/tasks'

const route = useRoute()
const loading = ref(false)
const execution = ref({})
const logs = ref('')

const loadExecution = async () => {
  loading.value = true
  try {
    execution.value = await executionAPI.get(route.params.id)
    const logData = await executionAPI.logs(route.params.id, { limit: 1000 })
    logs.value = logData.join('\n')
  } finally {
    loading.value = false
  }
}

const getStatusType = (status) => {
  const types = { pending: 'info', running: 'primary', success: 'success', failed: 'danger' }
  return types[status] || ''
}

onMounted(() => {
  loadExecution()
})
</script>

<style scoped>
.execution-detail { padding: 20px; }
</style>
