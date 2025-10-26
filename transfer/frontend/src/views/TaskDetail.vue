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
            <template v-if="isManualTask">
              <el-button type="primary" @click="handleExecute" :disabled="task.status === 'running'">
                执行
              </el-button>
              <el-button type="warning" @click="handleStop" :disabled="task.status !== 'running'">
                停止
              </el-button>
            </template>
            <template v-else>
              <el-button type="primary" @click="handleResume" :disabled="!canStartSchedule">
                启动
              </el-button>
              <el-button type="warning" @click="handlePause" :disabled="!canPauseSchedule">
                暂停
              </el-button>
              <el-button @click="handleExecute" :disabled="task.status === 'running'">
                单次执行
              </el-button>
            </template>
            <el-button @click="handleEdit" :disabled="!canEditTask">编辑</el-button>
          </div>
        </div>
      </template>

      <el-descriptions :column="2" border>
        <el-descriptions-item label="任务ID">{{ task.id }}</el-descriptions-item>
        <el-descriptions-item label="任务名称">{{ task.name }}</el-descriptions-item>
        <el-descriptions-item label="执行模式">{{ task.mode }}</el-descriptions-item>
        <el-descriptions-item label="状态">
          <el-tag :type="getTaskStatusTagType(task)">{{ getTaskStatusLabel(task) }}</el-tag>
        </el-descriptions-item>
        <el-descriptions-item label="最后执行状态">
          <el-tag :type="getExecutionTagType(task.last_execution_status)">
            {{ getLastExecutionLabel(task.last_execution_status) }}
          </el-tag>
        </el-descriptions-item>
        <el-descriptions-item label="最后执行时间">
          {{ formatDate(task.last_execution_finished_at || task.last_execution_started_at) }}
        </el-descriptions-item>
        <el-descriptions-item label="批量大小">{{ task.batch_size }}</el-descriptions-item>
        <el-descriptions-item label="定时调度">{{ formatSchedule(task.schedule) }}</el-descriptions-item>
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
            <el-tag :type="getExecutionTagType(row.status)">
              {{ getExecutionLabel(row.status) }}
            </el-tag>
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
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { taskAPI, executionAPI } from '@/api/tasks'
import { describeCron } from '@/utils/schedule'

const route = useRoute()
const router = useRouter()
const loading = ref(false)
const task = ref({})
const executions = ref([])
const isManualTask = computed(() => !task.value?.schedule)
const canStartSchedule = computed(() => {
  const status = task.value?.status
  return !!status && ['pending', 'paused'].includes(status)
})
const canPauseSchedule = computed(() => {
  const status = task.value?.status
  return !!status && ['scheduled', 'running'].includes(status)
})
const canEditTask = computed(() => {
  const status = task.value?.status
  return !status || status !== 'running'
})

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

const handleExecute = async () => {
  await taskAPI.start(route.params.id)
  const message = isManualTask.value ? '任务执行已提交' : '单次执行已提交'
  ElMessage.success(message)
  await loadTask()
}

const handleStop = async () => {
  try {
    await ElMessageBox.confirm('确定要停止该任务吗？', '提示', {
      confirmButtonText: '确定',
      cancelButtonText: '取消',
      type: 'warning'
    })
    await taskAPI.stop(route.params.id)
    ElMessage.success('任务已停止')
    await loadTask()
  } catch (error) {
    if (error !== 'cancel') {
      console.error('停止任务失败:', error)
    }
  }
}

const handlePause = async () => {
  try {
    await ElMessageBox.confirm('确定要暂停该定时任务吗？', '提示', {
      confirmButtonText: '确定',
      cancelButtonText: '取消',
      type: 'warning'
    })
    await taskAPI.pause(route.params.id)
    ElMessage.success('任务已暂停')
    await loadTask()
  } catch (error) {
    if (error !== 'cancel') {
      console.error('暂停任务失败:', error)
    }
  }
}

const handleResume = async () => {
  await taskAPI.resume(route.params.id)
  ElMessage.success('任务已启用')
  await loadTask()
}

const handleEdit = () => {
  if (!canEditTask.value) {
    ElMessage.warning('任务执行中，无法编辑')
    return
  }
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

const getTaskStatusLabel = (taskData) => {
  if (!taskData) return '未执行'
  if (!taskData.schedule) {
    const labels = {
      pending: '未执行',
      running: '执行中',
      stopped: '已停止',
      completed: '已完成'
    }
    return labels[taskData.status] || '未执行'
  }
  if (['scheduled', 'running'].includes(taskData.status)) return '已启动'
  if (['pending', 'paused'].includes(taskData.status)) return '未启动'
  if (taskData.status === 'stopped') return '已停止'
  return '未启动'
}

const getTaskStatusTagType = (taskData) => {
  const label = getTaskStatusLabel(taskData)
  const types = {
    未执行: 'info',
    执行中: 'primary',
    已停止: 'info',
    已完成: 'success',
    已启动: 'primary',
    未启动: 'info'
  }
  return types[label] || ''
}

const getLastExecutionLabel = (status) => {
  if (!status || status === 'pending') return '未执行'
  if (status === 'running') return '执行中'
  if (status === 'success') return '成功'
  if (status === 'failed' || status === 'cancelled') return '失败'
  return status
}

const getExecutionTagType = (status) => {
  const label = getLastExecutionLabel(status)
  const types = {
    未执行: 'info',
    执行中: 'primary',
    成功: 'success',
    失败: 'danger'
  }
  return types[label] || 'info'
}

const getExecutionLabel = (status) => {
  const label = getLastExecutionLabel(status)
  return label === '未执行' ? '待开始' : label
}

const formatDate = (date) => {
  return date ? new Date(date).toLocaleString('zh-CN') : '-'
}

const formatSchedule = (cron) => {
  if (!cron) return '手动执行'
  return describeCron(cron)
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
