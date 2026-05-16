<template>
  <div class="task-detail">
    <el-button @click="$router.back()" style="margin-bottom: 20px;">
      <el-icon><ArrowLeft /></el-icon>
      {{ t('transfer.taskDetail.back') }}
    </el-button>

    <el-card v-loading="loading">
      <template #header>
        <div class="header">
          <span>{{ t('transfer.taskDetail.taskDetailTitle', { name: task.name }) }}</span>
          <div>
            <template v-if="isManualTask">
              <el-button type="primary" @click="handleExecute" :disabled="task.status === 'running'">
                {{ t('transfer.taskDetail.execute') }}
              </el-button>
              <el-button type="warning" @click="handleStop" :disabled="task.status !== 'running'">
                {{ t('transfer.taskDetail.stop') }}
              </el-button>
            </template>
            <template v-else>
              <el-button type="primary" @click="handleResume" :disabled="!canStartSchedule">
                {{ t('transfer.taskDetail.start') }}
              </el-button>
              <el-button type="warning" @click="handlePause" :disabled="!canPauseSchedule">
                {{ t('transfer.taskDetail.pause') }}
              </el-button>
              <el-button @click="handleExecute" :disabled="task.status === 'running'">
                {{ t('transfer.taskDetail.runOnce') }}
              </el-button>
            </template>
            <el-button @click="handleEdit" :disabled="!canEditTask">{{ t('transfer.taskDetail.edit') }}</el-button>
            <el-button @click="openJsonDialog">{{ t('transfer.taskDetail.viewJsonConfig') }}</el-button>
          </div>
        </div>
      </template>

      <el-descriptions :column="2" border>
        <el-descriptions-item :label="t('transfer.taskDetail.taskId')">{{ task.id }}</el-descriptions-item>
        <el-descriptions-item :label="t('transfer.taskDetail.taskName')">{{ task.name }}</el-descriptions-item>
        <el-descriptions-item :label="t('transfer.taskDetail.status')">
          <el-tag :type="getTaskStatusTagType(task)">{{ getTaskStatusLabel(task) }}</el-tag>
        </el-descriptions-item>
        <el-descriptions-item :label="t('transfer.taskDetail.batchSize')">{{ task.batch_size }}</el-descriptions-item>
        <el-descriptions-item :label="t('transfer.taskDetail.schedule')">{{ formatSchedule(task.schedule) }}</el-descriptions-item>
        <el-descriptions-item :label="t('transfer.taskDetail.createdAt')">
          {{ formatDate(task.created_at) }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('transfer.taskDetail.description')" :span="2">
          {{ task.description || '-' }}
        </el-descriptions-item>
      </el-descriptions>

      <el-divider content-position="left">{{ t('transfer.taskDetail.sourceDataSource') }}</el-divider>
      <el-descriptions :column="2" border>
        <el-descriptions-item
          v-for="item in sourceDetails"
          :key="`source-${item.label}`"
          :span="item.span || 1"
        >
          <template #label>{{ item.label }}</template>
          <span class="detail-text">{{ item.value }}</span>
        </el-descriptions-item>
      </el-descriptions>

      <el-divider content-position="left">{{ t('transfer.taskDetail.targetDataSource') }}</el-divider>
      <el-descriptions :column="2" border>
        <el-descriptions-item
          v-for="item in targetDetails"
          :key="`target-${item.label}`"
          :span="item.span || 1"
        >
          <template #label>{{ item.label }}</template>
          <span class="detail-text">{{ item.value }}</span>
        </el-descriptions-item>
      </el-descriptions>

      <el-divider>{{ t('transfer.taskDetail.executionRecords') }}</el-divider>
      <el-table :data="executions" stripe>
        <el-table-column prop="id" :label="t('transfer.taskDetail.id')" width="80" />
        <el-table-column prop="status" :label="t('transfer.taskDetail.status')" width="100">
          <template #default="{ row }">
            <el-tag :type="getExecutionTagType(row.status)">
              {{ getExecutionLabel(row.status) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="records_written" :label="t('transfer.taskDetail.recordsWritten')" width="120" />
        <el-table-column prop="start_time" :label="t('transfer.taskDetail.startTime')" width="180">
          <template #default="{ row }">
            {{ formatDate(row.start_time) }}
          </template>
        </el-table-column>
        <el-table-column prop="end_time" :label="t('transfer.taskDetail.endTime')" width="180">
          <template #default="{ row }">
            {{ formatDate(row.end_time) }}
          </template>
        </el-table-column>
        <el-table-column :label="t('transfer.taskDetail.actions')" width="150">
          <template #default="{ row }">
            <el-button size="small" @click="viewExecution(row.id)">{{ t('transfer.taskDetail.detail') }}</el-button>
            <el-button size="small" type="primary" @click="retryExecution(row.id)" v-if="row.status === 'failed'">
              {{ t('transfer.taskDetail.retry') }}
            </el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <el-dialog v-model="jsonDialogVisible" width="700px">
      <template #header>
        <div class="json-dialog-header">
          <span>{{ t('transfer.taskDetail.jsonConfig') }}</span>
          <el-button size="small" type="primary" @click="handleCopyJson">{{ t('transfer.taskDetail.copy') }}</el-button>
        </div>
      </template>
      <pre class="json-pre">{{ formattedConfig }}</pre>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { ArrowLeft } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import { taskAPI, executionAPI } from '@/api/tasks'
import { formatDate } from '@common-ui'
import { formatSchedule, getTaskStatusLabel, getTaskStatusTagType, getExecutionTagType, getExecutionLabel } from '@/utils/formatters'

const { t } = useI18n()

const route = useRoute()
const router = useRouter()
const loading = ref(false)
const task = ref({})
const executions = ref([])
const jsonDialogVisible = ref(false)

const isManualTask = computed(() => !task.value?.schedule)
const canStartSchedule = computed(() => !task.value?.enabled)
const canPauseSchedule = computed(() => task.value?.enabled)
const canEditTask = computed(() => task.value?.status !== 'running')

const loadTask = async () => {
  if (!route.params.id) return
  loading.value = true
  try {
    const taskData = await taskAPI.get(route.params.id)
    const executionsRes = await taskAPI.executions(route.params.id, { page: 1, page_size: 10 }).catch(() => ({ data: [] }))

    task.value = taskData || {}
    executions.value = executionsRes?.data || []
  } finally {
    loading.value = false
  }
}

const handleExecute = async () => {
  await taskAPI.start(route.params.id)
  const message = isManualTask.value ? t('transfer.taskDetail.executeSubmitted') : t('transfer.taskDetail.runOnceSubmitted')
  ElMessage.success(message)
  await loadTask()
}

const handleStop = async () => {
  try {
    await ElMessageBox.confirm(t('transfer.taskDetail.stopConfirm'), t('transfer.taskDetail.hint'), {
      confirmButtonText: t('transfer.taskDetail.confirm'),
      cancelButtonText: t('transfer.taskDetail.cancel'),
      type: 'warning'
    })
    await taskAPI.stop(route.params.id)
    ElMessage.success(t('transfer.taskDetail.taskStopped'))
    await loadTask()
  } catch (error) {
    if (error !== 'cancel') {
      console.error('停止任务失败:', error)
    }
  }
}

const handlePause = async () => {
  try {
    await ElMessageBox.confirm(t('transfer.taskDetail.pauseConfirm'), t('transfer.taskDetail.hint'), {
      confirmButtonText: t('transfer.taskDetail.confirm'),
      cancelButtonText: t('transfer.taskDetail.cancel'),
      type: 'warning'
    })
    await taskAPI.pause(route.params.id)
    ElMessage.success(t('transfer.taskDetail.taskPaused'))
    await loadTask()
  } catch (error) {
    if (error !== 'cancel') {
      console.error('暂停任务失败:', error)
    }
  }
}

const handleResume = async () => {
  await taskAPI.resume(route.params.id)
  ElMessage.success(t('transfer.taskDetail.taskResumed'))
  await loadTask()
}

const handleEdit = () => {
  if (!canEditTask.value) {
    ElMessage.warning(t('transfer.taskDetail.taskRunning'))
    return
  }
  router.push(`/tasks/${route.params.id}/edit`)
}

const viewExecution = (id) => {
  router.push(`/executions/${id}`)
}

const retryExecution = async (id) => {
  await executionAPI.retry(id)
  ElMessage.success(t('transfer.taskDetail.retrySubmitted'))
  loadTask()
}

const sourceDetails = computed(() => buildEndpointDetails(task.value?.config?.source, 'source'))
const targetDetails = computed(() => buildEndpointDetails(task.value?.config?.target, 'target'))

function buildEndpointDetails(endpoint, role) {
  if (!endpoint || typeof endpoint !== 'object') {
    return [{ label: t('transfer.taskDetail.dataSource'), value: t('transfer.taskDetail.notConfigured'), span: 2 }]
  }

  const items = []
  addItem(items, t('transfer.taskDetail.engineScope'), endpoint.engine?.scope)
  addItem(items, t('transfer.taskDetail.reviewEngineId'), endpoint.engine?.id)
  addItem(items, t('transfer.taskDetail.connectionType'), endpoint.engine?.type)
  addItem(items, t('transfer.taskDetail.dataType'), endpoint.data_type)
  addItem(items, t('transfer.taskDetail.representation'), endpoint.representation)
  addItem(items, t('transfer.taskDetail.resourceKind'), endpoint.resource?.kind)
  addItem(items, t('transfer.taskDetail.path'), formatResourcePath(endpoint.resource), 2)

  if (role === 'target') {
    addItem(items, t('transfer.taskDetail.format'), endpoint.format)
    addItem(items, t('transfer.taskDetail.writeMode'), endpoint.policy?.write_mode)
    addItem(items, t('transfer.taskDetail.options'), endpoint.options, 2)
  }

  return items
}

function addItem(items, label, value, span) {
  items.push({ label, value: formatValue(value), span })
}

function formatResourcePath(resource) {
  const path = resource?.path
  if (path === undefined || path === null) {
    return ''
  }
  if (typeof path === 'string') {
    return path
  }
  if (path.schema || path.table) {
    return [path.schema, path.table].filter(Boolean).join('.')
  }
  if (path.bucket || path.path) {
    return [path.bucket, path.path].filter(Boolean).join('/')
  }
  if (path.name) {
    return path.name
  }
  return path
}

function formatValue(value) {
  if (value === undefined || value === null || value === '') return '-'
  if (typeof value === 'boolean') return value ? t('transfer.taskDetail.boolYes') : t('transfer.taskDetail.boolNo')
  if (typeof value === 'object') {
    try {
      const json = JSON.stringify(value)
      return json && json !== '{}' ? json : '-'
    } catch {
      return '-'
    }
  }
  return String(value)
}

const formattedConfig = computed(() => {
  try {
    const value = {
      task_id: task.value.id,
      name: task.value.name,
      description: task.value.description,
      task_type: task.value.task_type,
      schedule: task.value.schedule || '',
      batch_size: task.value.batch_size,
      config: task.value.config || {}
    }
    return JSON.stringify(value, null, 2)
  } catch (error) {
    console.error('格式化配置失败:', error)
    return '{}'
  }
})

const openJsonDialog = () => {
  jsonDialogVisible.value = true
}

const handleCopyJson = () => {
  try {
    copyToClipboard(formattedConfig.value)
    ElMessage.success(t('transfer.taskDetail.copiedToClipboard'))
  } catch (error) {
    console.error('复制失败:', error)
    ElMessage.error(t('transfer.taskDetail.copyFailed'))
  }
}

const copyToClipboard = (text) => {
  const textarea = document.createElement('textarea')
  textarea.value = text
  textarea.style.position = 'fixed'
  textarea.style.top = '0'
  textarea.style.left = '0'
  textarea.style.width = '2em'
  textarea.style.height = '2em'
  textarea.style.padding = '0'
  textarea.style.border = 'none'
  textarea.style.outline = 'none'
  textarea.style.boxShadow = 'none'
  textarea.style.background = 'transparent'
  document.body.appendChild(textarea)
  textarea.focus()
  textarea.select()

  try {
    const successful = document.execCommand('copy')
    document.body.removeChild(textarea)
    if (!successful) {
      throw new Error(t('transfer.taskDetail.copyFailed'))
    }
  } catch (err) {
    document.body.removeChild(textarea)
    throw err
  }
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

.detail-text {
  white-space: pre-wrap;
  word-break: break-word;
}

.json-dialog-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.json-pre {
  background-color: var(--addp-bg-secondary);
  border-radius: 6px;
  padding: 16px;
  font-size: 12px;
  line-height: 1.6;
  max-height: 420px;
  overflow: auto;
  color: var(--addp-text-primary);
}
</style>
