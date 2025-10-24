<template>
  <div class="task-detail">
    <el-button @click="$router.back()" style="margin-bottom: 20px;">
      <el-icon><ArrowLeft /></el-icon>
      返回
    </el-button>

    <el-card v-loading="loading">
      <template #header>
        <div class="header">
          <span>任务详情 - {{ task.name }}</span>
          <div>
            <el-button type="primary" @click="handleStart" :disabled="task.status === 'running'">
              启动
            </el-button>
            <el-button type="warning" @click="handleStop" :disabled="task.status !== 'running'">
              停止
            </el-button>
            <el-button @click="handleEdit">编辑</el-button>
          </div>
        </div>
      </template>

      <el-descriptions :column="2" border>
        <el-descriptions-item label="任务ID">{{ task.id }}</el-descriptions-item>
        <el-descriptions-item label="任务名称">{{ task.name }}</el-descriptions-item>
        <el-descriptions-item label="任务类型">
          <el-tag>{{ task.type }}</el-tag>
        </el-descriptions-item>
        <el-descriptions-item label="执行模式">{{ task.mode }}</el-descriptions-item>
        <el-descriptions-item label="状态">
          <el-tag :type="getStatusType(task.status)">{{ task.status }}</el-tag>
        </el-descriptions-item>
        <el-descriptions-item label="进度">
          <el-progress :percentage="task.progress || 0" />
        </el-descriptions-item>
        <el-descriptions-item label="批量大小">{{ task.batch_size }}</el-descriptions-item>
        <el-descriptions-item label="定时调度">{{ task.schedule || '无' }}</el-descriptions-item>
        <el-descriptions-item label="创建时间" :span="2">
          {{ formatDate(task.created_at) }}
        </el-descriptions-item>
        <el-descriptions-item label="描述" :span="2">
          {{ task.description || '-' }}
        </el-descriptions-item>
      </el-descriptions>

      <el-divider>执行记录</el-divider>
      <el-table :data="executions" stripe>
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column prop="status" label="状态" width="100">
          <template #default="{ row }">
            <el-tag :type="getStatusType(row.status)">{{ row.status }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="records_written" label="已写入记录" width="120" />
        <el-table-column prop="start_time" label="开始时间" width="180">
          <template #default="{ row }">
            {{ formatDate(row.start_time) }}
          </template>
        </el-table-column>
        <el-table-column prop="end_time" label="结束时间" width="180">
          <template #default="{ row }">
            {{ formatDate(row.end_time) }}
          </template>
        </el-table-column>
        <el-table-column label="操作" width="150">
          <template #default="{ row }">
            <el-button size="small" @click="viewExecution(row.id)">详情</el-button>
            <el-button size="small" type="primary" @click="retryExecution(row.id)"
              v-if="row.status === 'failed'">
              重试
            </el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>
  </div>
</template>

<script setup>
import { ref, onMounted, onBeforeUnmount } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { taskAPI, executionAPI } from '@/api/tasks'

const route = useRoute()
const router = useRouter()
const loading = ref(false)
const task = ref({})
const executions = ref([])

const loadTask = async () => {
  if (!route.params.id) return
  loading.value = true
  try {
    task.value = await taskAPI.get(route.params.id)
    const res = await taskAPI.executions(route.params.id, { page: 1, page_size: 10 })
    executions.value = res.data || []
  } finally {
    loading.value = false
  }
}

const handleStart = async () => {
  await taskAPI.start(route.params.id)
  ElMessage.success('任务已启动')
  loadTask()
}

const handleStop = async () => {
  await taskAPI.stop(route.params.id)
  ElMessage.success('任务已停止')
  loadTask()
}

const handleEdit = () => {
  router.push(`/tasks/${route.params.id}/edit`)
}

const viewExecution = (id) => {
  router.push(`/executions/${id}`)
}

const retryExecution = async (id) => {
  await executionAPI.retry(id)
  ElMessage.success('重试已提交')
  loadTask()
}

const getStatusType = (status) => {
  const types = {
    pending: 'info',
    running: 'primary',
    success: 'success',
    failed: 'danger'
  }
  return types[status] || ''
}

const formatDate = (date) => {
  return date ? new Date(date).toLocaleString('zh-CN') : '-'
}

let refreshTimer = null

onMounted(() => {
  loadTask()
  refreshTimer = setInterval(loadTask, 5000)
})

onBeforeUnmount(() => {
  if (refreshTimer) {
    clearInterval(refreshTimer)
    refreshTimer = null
  }
})
</script>

<style scoped>
.task-detail {
  padding: 20px;
}

.header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
</style>
