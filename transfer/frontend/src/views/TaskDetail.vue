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
            <template v-if="isContinuousTask">
              <el-button type="primary" @click="handleStartContinuous" :disabled="!canStartContinuous">
                {{ task.desired_state === 'paused' ? t('transfer.taskDetail.resume') : t('transfer.taskDetail.start') }}
              </el-button>
              <el-button type="warning" @click="handlePause" :disabled="task.desired_state !== 'running'">
                {{ t('transfer.taskDetail.pause') }}
              </el-button>
              <el-button @click="handleStop" :disabled="task.desired_state === 'stopped'">
                {{ t('transfer.taskDetail.stop') }}
              </el-button>
            </template>
            <template v-else-if="isManualTask">
              <el-button type="primary" @click="handleExecute" :disabled="task.status === 'running'">
                {{ t('transfer.taskDetail.execute') }}
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
          <el-tag :type="getTaskStatusTagType(task)">{{ taskDisplayStatus }}</el-tag>
        </el-descriptions-item>
        <el-descriptions-item :label="t('transfer.taskDetail.batchSize')">{{ task.batch_size }}</el-descriptions-item>
        <el-descriptions-item :label="t('transfer.taskDetail.schedule')">
          {{ isContinuousTask ? t('transfer.taskDetail.continuousRuntime') : formatSchedule(task.schedule) }}
        </el-descriptions-item>
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
        <el-table-column prop="execution_id" :label="t('transfer.taskDetail.executionId')" width="220" show-overflow-tooltip />
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
            <el-button size="small" @click="viewExecution(row.execution_id)">{{ t('transfer.taskDetail.detail') }}</el-button>
            <el-button size="small" type="primary" @click="retryExecution(row.execution_id)" v-if="row.status === 'failed'">
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
import { formatLocatorDisplayPath } from '@addp/common-frontend'
import { taskAPI, executionAPI } from '@/api/tasks'
import { formatDate } from '@common-ui'
import { formatSchedule, getTaskStatusLabel, getTaskStatusTagType, getExecutionTagType, getExecutionLabel } from '@/utils/formatters'
import { parseTransferLocator } from '@/utils/resourceLocator'

const { t } = useI18n()

const route = useRoute()
const router = useRouter()
const loading = ref(false)
const task = ref({})
const executions = ref([])
const jsonDialogVisible = ref(false)

const isContinuousTask = computed(() => task.value?.config?.runtime?.boundary === 'continuous')
const isManualTask = computed(() => !isContinuousTask.value && !task.value?.schedule)
const canStartSchedule = computed(() => !task.value?.enabled)
const canPauseSchedule = computed(() => task.value?.enabled)
const canStartContinuous = computed(() => ['paused', 'stopped'].includes(task.value?.desired_state) && task.value?.status !== 'running')
const canEditTask = computed(() => task.value?.status !== 'running' && (!isContinuousTask.value || task.value?.desired_state === 'stopped'))
const taskDisplayStatus = computed(() => {
  if (!isContinuousTask.value) return getTaskStatusLabel(task.value)
  if (task.value?.desired_state === 'paused') return t('transfer.taskDetail.continuousPaused')
  if (task.value?.desired_state === 'stopped') return t('transfer.taskDetail.continuousStopped')
  return t('transfer.taskDetail.continuousRunning')
})

let refreshTimer = null

const isTaskRunning = (taskData) => taskData?.status === 'running' || (taskData?.config?.runtime?.boundary === 'continuous' && taskData?.desired_state === 'running')

const stopAutoRefresh = () => {
  if (refreshTimer) {
    clearInterval(refreshTimer)
    refreshTimer = null
  }
}

const syncAutoRefresh = () => {
  if (isTaskRunning(task.value)) {
    if (!refreshTimer) {
      refreshTimer = setInterval(loadTask, 5000)
    }
    return
  }

  stopAutoRefresh()
}

const loadTask = async () => {
  if (!route.params.id) return
  loading.value = true
  try {
    const taskData = await taskAPI.get(route.params.id)
    const executionsRes = await taskAPI.executions(route.params.id, { page: 1, page_size: 10 }).catch(() => ({ data: [] }))

    task.value = taskData || {}
    executions.value = executionsRes?.data || []
    syncAutoRefresh()
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

const handleStartContinuous = async () => {
  if (task.value?.desired_state === 'paused') {
    await taskAPI.resume(route.params.id)
    ElMessage.success(t('transfer.taskDetail.taskResumed'))
  } else {
    await taskAPI.start(route.params.id)
    ElMessage.success(t('transfer.taskDetail.executeSubmitted'))
  }
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
      console.error('停止持续同步任务失败:', error)
    }
  }
}

const handleEdit = () => {
  if (!canEditTask.value) {
    ElMessage.warning(t('transfer.taskDetail.taskRunning'))
    return
  }
  router.push(`/tasks/${route.params.id}/edit`)
}

const viewExecution = (executionId) => {
  router.push(`/executions/${executionId}`)
}

const retryExecution = async (executionId) => {
  await executionAPI.retry(executionId)
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
  const loc = parseTransferLocator(role === 'target' ? endpoint.parent_locator : endpoint.locator)
  addItem(items, t('transfer.taskDetail.reviewEngineId'), loc.engineID)
  addItem(items, t('transfer.taskDetail.connectionType'), loc.type)
  addItem(items, t('transfer.taskDetail.dataType'), endpoint.data_type)
  addItem(items, t('transfer.taskDetail.representation'), endpoint.representation)
  addItem(items, t('transfer.taskDetail.path'), formatEndpointPath(endpoint, role), 2)

  if (role === 'source' && endpoint.change_stream) {
    addItem(items, t('transfer.taskDetail.messageFormat'), `${endpoint.change_stream.envelope || '-'} / ${endpoint.change_stream.encoding || '-'}`)
    addItem(items, t('transfer.taskDetail.keyFields'), endpoint.change_stream.key?.fields)
    addItem(items, t('transfer.taskDetail.initialPosition'), endpoint.change_stream.start?.initial)
    addItem(items, t('transfer.taskDetail.pollBatchSize'), endpoint.change_stream.poll_batch_size)
  }

  if (role === 'target') {
    addItem(items, t('transfer.taskDetail.format'), endpoint.format)
    addItem(items, t('transfer.taskDetail.writeMode'), endpoint.policy?.apply_mode)
    addItem(items, t('transfer.taskDetail.options'), endpoint.options, 2)
  }

  return items
}

function addItem(items, label, value, span) {
  items.push({ label, value: formatValue(value), span })
}

function formatEndpointPath(endpoint, role) {
  if (role !== 'target') {
    return formatLocatorDisplayPath(endpoint?.locator, endpoint?.representation)
  }
  const parent = parseTransferLocator(endpoint?.parent_locator)
  const name = String(endpoint?.name || '').trim()
  if (endpoint?.representation === 'native') {
    return [parent.path[parent.path.length - 1], name].filter(Boolean).join('.')
  }
  return [...parent.path, name].filter(Boolean).join('/')
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

onMounted(() => {
  loadTask()
})

onBeforeUnmount(() => {
  stopAutoRefresh()
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
