<template>
  <div class="vectorization">
    <el-card>
      <template #header>
        <div class="card-header">
          <span>{{ t('manager.vectorization.title') }}</span>
          <div class="header-actions">
            <el-button v-if="activeTab === 'tasks'" type="primary" :icon="Plus" @click="openCreateDialog">
              {{ t('manager.vectorization.create') }}
            </el-button>
            <el-button :icon="Refresh" circle @click="refreshActiveTab" />
          </div>
        </div>
      </template>

      <el-tabs v-model="activeTab" @tab-change="handleTabChange">
        <el-tab-pane :label="t('manager.vectorization.resultsTab')" name="results">
          <div class="filter-bar">
            <el-input-number
              v-model="resultFilters.engine_id"
              :min="0"
              :placeholder="t('manager.vectorization.engine')"
              controls-position="right"
            />
            <el-input-number
              v-model="resultFilters.item_id"
              :min="0"
              :placeholder="t('manager.vectorization.item')"
              controls-position="right"
            />
            <el-input-number
              v-model="resultFilters.node_id"
              :min="0"
              :placeholder="t('manager.vectorization.node')"
              controls-position="right"
            />
            <el-select v-model="resultFilters.status" clearable :placeholder="t('manager.vectorization.resultStatus')">
              <el-option
                v-for="status in embeddingStatuses"
                :key="status"
                :label="embeddingStatusLabel(status)"
                :value="status"
              />
            </el-select>
            <el-input
              v-model="resultFilters.q"
              clearable
              :placeholder="t('manager.vectorization.keywordPlaceholder')"
              @keyup.enter="applyResultFilters"
            />
            <el-button type="primary" @click="applyResultFilters">{{ t('manager.vectorization.search') }}</el-button>
            <el-button @click="resetResultFilters">{{ t('manager.vectorization.reset') }}</el-button>
          </div>

          <el-table :data="results" v-loading="resultsLoading" stripe>
            <el-table-column prop="item_id" :label="t('manager.vectorization.item')" width="100" />
            <el-table-column prop="engine_id" :label="t('manager.vectorization.engine')" width="100" />
            <el-table-column prop="locator" :label="t('manager.vectorization.locator')" min-width="260" show-overflow-tooltip />
            <el-table-column :label="t('manager.vectorization.resultStatus')" width="130">
              <template #default="{ row }">
                <el-tag :type="embeddingStatusTagType(row.status)">
                  {{ embeddingStatusLabel(row.status) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="model" :label="t('manager.vectorization.model')" min-width="150" show-overflow-tooltip />
            <el-table-column prop="dimension" :label="t('manager.vectorization.dimension')" width="110" />
            <el-table-column :label="t('manager.vectorization.vectorizedAt')" width="180">
              <template #default="{ row }">
                {{ formatDateTime(row.vectorized_at) }}
              </template>
            </el-table-column>
            <el-table-column prop="last_execution_id" :label="t('manager.vectorization.lastExecutionId')" min-width="180" show-overflow-tooltip />
            <el-table-column prop="error_message" :label="t('manager.vectorization.error')" min-width="180" show-overflow-tooltip />
            <el-table-column :label="t('manager.vectorization.actions')" width="170" fixed="right">
              <template #default="{ row }">
                <el-button size="small" :loading="revectorizingId === row.id" @click="revectorizeResult(row)">
                  {{ t('manager.vectorization.revectorize') }}
                </el-button>
                <el-button size="small" type="danger" @click="deleteResult(row)">
                  {{ t('manager.vectorization.delete') }}
                </el-button>
              </template>
            </el-table-column>
          </el-table>

          <el-pagination
            v-model:current-page="resultsPage"
            v-model:page-size="resultsPageSize"
            :total="resultsTotal"
            :page-sizes="[10, 20, 50, 100]"
            layout="total, sizes, prev, pager, next, jumper"
            class="pagination"
            @size-change="handleResultsSizeChange"
            @current-change="loadResults"
          />
        </el-tab-pane>

        <el-tab-pane :label="t('manager.vectorization.tasksTab')" name="tasks">
          <el-table :data="tasks" v-loading="tasksLoading" stripe>
            <el-table-column prop="name" :label="t('manager.vectorization.name')" min-width="180" show-overflow-tooltip />
            <el-table-column :label="t('manager.vectorization.engine')" width="100">
              <template #default="{ row }">
                {{ row.target?.engine_id || '-' }}
              </template>
            </el-table-column>
            <el-table-column :label="t('manager.vectorization.node')" width="100">
              <template #default="{ row }">
                {{ row.target?.node_id || '-' }}
              </template>
            </el-table-column>
            <el-table-column :label="t('manager.vectorization.locator')" min-width="240" show-overflow-tooltip>
              <template #default="{ row }">
                {{ row.target?.locator || '-' }}
              </template>
            </el-table-column>
            <el-table-column :label="t('manager.vectorization.recursive')" width="100">
              <template #default="{ row }">
                <el-tag :type="row.target?.recursive ? 'success' : 'info'">
                  {{ row.target?.recursive ? t('common.yes') : t('common.no') }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="t('manager.vectorization.schedule')" min-width="140" show-overflow-tooltip>
              <template #default="{ row }">
                {{ row.schedule || '-' }}
              </template>
            </el-table-column>
            <el-table-column :label="t('manager.vectorization.enabled')" width="100">
              <template #default="{ row }">
                <el-tag :type="row.enabled ? 'success' : 'info'">
                  {{ row.enabled ? t('manager.vectorization.enabledYes') : t('manager.vectorization.enabledNo') }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="t('manager.vectorization.lastStatus')" width="120">
              <template #default="{ row }">
                <el-tag :type="executionStatusTagType(row.last_execution_status)">
                  {{ executionStatusLabel(row.last_execution_status) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="t('manager.vectorization.lastRunAt')" width="180">
              <template #default="{ row }">
                {{ formatDateTime(row.last_run_at) }}
              </template>
            </el-table-column>
            <el-table-column :label="t('manager.vectorization.actions')" width="340" fixed="right">
              <template #default="{ row }">
                <el-button type="primary" size="small" :loading="executingId === row.id" @click="executeTask(row)">
                  {{ t('manager.vectorization.execute') }}
                </el-button>
                <el-button size="small" @click="openEditDialog(row)">
                  {{ t('manager.vectorization.edit') }}
                </el-button>
                <el-button size="small" @click="viewTaskResults(row)">
                  {{ t('manager.vectorization.results') }}
                </el-button>
                <el-button size="small" :disabled="!row.last_execution_id" @click="openTaskExecution(row)">
                  {{ t('manager.vectorization.monitor') }}
                </el-button>
                <el-button size="small" @click="showTaskDetail(row)">
                  {{ t('manager.vectorization.detail') }}
                </el-button>
                <el-button size="small" type="danger" @click="deleteTask(row)">
                  {{ t('manager.vectorization.delete') }}
                </el-button>
              </template>
            </el-table-column>
          </el-table>

          <el-pagination
            v-model:current-page="tasksPage"
            v-model:page-size="tasksPageSize"
            :total="tasksTotal"
            :page-sizes="[10, 20, 50, 100]"
            layout="total, sizes, prev, pager, next, jumper"
            class="pagination"
            @size-change="handleTasksSizeChange"
            @current-change="loadTasks"
          />
        </el-tab-pane>
      </el-tabs>
    </el-card>

    <el-dialog v-model="formDialogVisible" :title="formTitle" width="720px">
      <el-form ref="formRef" :model="form" :rules="rules" label-width="140px">
        <el-form-item :label="t('manager.vectorization.name')" prop="name">
          <el-input v-model="form.name" />
        </el-form-item>
        <el-form-item :label="t('manager.vectorization.description')">
          <el-input v-model="form.description" type="textarea" :rows="2" />
        </el-form-item>
        <el-form-item :label="t('manager.vectorization.engine')" prop="target.engine_id">
          <el-input-number v-model="form.target.engine_id" :min="1" />
        </el-form-item>
        <el-form-item :label="t('manager.vectorization.node')" prop="target.node_id">
          <el-input-number v-model="form.target.node_id" :min="1" />
        </el-form-item>
        <el-form-item :label="t('manager.vectorization.locator')" prop="target.locator">
          <el-input v-model="form.target.locator" />
        </el-form-item>
        <el-form-item :label="t('manager.vectorization.recursive')">
          <el-switch v-model="form.target.recursive" />
        </el-form-item>
        <el-form-item :label="t('manager.vectorization.enabled')">
          <el-switch v-model="form.enabled" />
        </el-form-item>
        <el-form-item :label="t('manager.vectorization.schedule')">
          <el-input v-model="form.schedule" :placeholder="t('manager.vectorization.schedulePlaceholder')" />
        </el-form-item>
        <el-form-item :label="t('manager.vectorization.extensions')">
          <el-input v-model="form.filters.extensions" :placeholder="t('manager.vectorization.extensionsPlaceholder')" />
        </el-form-item>
        <el-form-item :label="t('manager.vectorization.maxFileSize')">
          <el-input-number v-model="form.filters.max_file_size_mb" :min="0" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="formDialogVisible = false">{{ t('manager.vectorization.cancel') }}</el-button>
        <el-button type="primary" :loading="saving" @click="saveTask">{{ t('manager.vectorization.save') }}</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="detailDialogVisible" :title="t('manager.vectorization.dialogTitle')" width="760px">
      <el-descriptions v-if="selectedTask" :column="2" border>
        <el-descriptions-item :label="t('manager.vectorization.id')">{{ selectedTask.id }}</el-descriptions-item>
        <el-descriptions-item :label="t('manager.vectorization.name')">{{ selectedTask.name }}</el-descriptions-item>
        <el-descriptions-item :label="t('manager.vectorization.description')" :span="2">
          {{ selectedTask.description || '-' }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('manager.vectorization.engine')">{{ selectedTask.target?.engine_id || '-' }}</el-descriptions-item>
        <el-descriptions-item :label="t('manager.vectorization.node')">{{ selectedTask.target?.node_id || '-' }}</el-descriptions-item>
        <el-descriptions-item :label="t('manager.vectorization.locator')" :span="2">{{ selectedTask.target?.locator || '-' }}</el-descriptions-item>
        <el-descriptions-item :label="t('manager.vectorization.model')">
          {{ selectedTask.config?.embedding?.model || '-' }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('manager.vectorization.dimension')">
          {{ selectedTask.config?.embedding?.dimension || '-' }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('manager.vectorization.schedule')">{{ selectedTask.schedule || '-' }}</el-descriptions-item>
        <el-descriptions-item :label="t('manager.vectorization.nextRunAt')">{{ formatDateTime(selectedTask.next_run_at) }}</el-descriptions-item>
        <el-descriptions-item :label="t('manager.vectorization.lastExecutionId')" :span="2">
          {{ selectedTask.last_execution_id || '-' }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('manager.vectorization.lastStatus')">
          <el-tag :type="executionStatusTagType(selectedTask.last_execution_status)">
            {{ executionStatusLabel(selectedTask.last_execution_status) }}
          </el-tag>
        </el-descriptions-item>
        <el-descriptions-item :label="t('manager.vectorization.lastRunAt')">
          {{ formatDateTime(selectedTask.last_run_at) }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('manager.vectorization.createdAt')">
          {{ formatDateTime(selectedTask.created_at) }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('manager.vectorization.updatedAt')">
          {{ formatDateTime(selectedTask.updated_at) }}
        </el-descriptions-item>
      </el-descriptions>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Plus, Refresh } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { openMonitorExecution } from '@addp/common-frontend'
import client from '../api/client'
import { formatDateTime } from '../utils/formatters'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()

const activeTab = ref('results')

const results = ref([])
const resultsLoading = ref(false)
const resultsPage = ref(1)
const resultsPageSize = ref(20)
const resultsTotal = ref(0)
const revectorizingId = ref(null)
const resultFilters = reactive(defaultResultFilters())

const tasks = ref([])
const tasksLoading = ref(false)
const tasksPage = ref(1)
const tasksPageSize = ref(20)
const tasksTotal = ref(0)
const executingId = ref(null)

const formRef = ref(null)
const formDialogVisible = ref(false)
const saving = ref(false)
const editingId = ref(null)
const form = reactive(defaultForm())

const detailDialogVisible = ref(false)
const selectedTask = ref(null)
const embeddingStatuses = ['ready', 'outdated', 'failed', 'unsupported', 'missing_source']

const formTitle = computed(() => editingId.value ? t('manager.vectorization.editTitle') : t('manager.vectorization.createTitle'))
const rules = computed(() => ({
  name: [{ required: true, message: t('manager.vectorization.nameRequired'), trigger: 'blur' }],
  'target.engine_id': [{ required: true, message: t('manager.vectorization.engineRequired'), trigger: 'change' }],
  'target.node_id': [{ required: true, message: t('manager.vectorization.nodeRequired'), trigger: 'change' }],
  'target.locator': [{ required: true, message: t('manager.vectorization.locatorRequired'), trigger: 'blur' }]
}))

function defaultForm() {
  return {
    name: '',
    description: '',
    enabled: true,
    schedule: '',
    target: {
      engine_id: 1,
      node_id: 1,
      locator: '',
      recursive: true
    },
    filters: {
      extensions: '',
      max_file_size_mb: 0
    }
  }
}

function defaultResultFilters() {
  return {
    engine_id: null,
    node_id: null,
    item_id: null,
    status: '',
    q: ''
  }
}

const resetForm = (task = null) => {
  Object.assign(form, defaultForm())
  if (task) {
    const config = task.config || {}
    const target = task.target || config.target || {}
    const filters = config.filters || {}
    Object.assign(form, {
      name: task.name || '',
      description: task.description || '',
      enabled: task.enabled !== false,
      schedule: task.schedule || '',
      target: {
        engine_id: Number(target.engine_id || 1),
        node_id: Number(target.node_id || 1),
        locator: target.locator || '',
        recursive: target.recursive !== false
      },
      filters: {
        extensions: Array.isArray(filters.extensions) ? filters.extensions.join(',') : '',
        max_file_size_mb: Number(filters.max_file_size_mb || 0)
      }
    })
  }
  editingId.value = task?.id || null
}

const taskPayload = () => {
  const extensions = String(form.filters.extensions || '')
    .split(',')
    .map((item) => item.trim())
    .filter(Boolean)
  const filters = {}
  if (extensions.length) {
    filters.extensions = extensions
  }
  if (Number(form.filters.max_file_size_mb) > 0) {
    filters.max_file_size_mb = Number(form.filters.max_file_size_mb)
  }
  const config = {
    target: {
      scope: 'node',
      engine_id: Number(form.target.engine_id),
      node_id: Number(form.target.node_id),
      locator: String(form.target.locator || '').trim(),
      recursive: form.target.recursive !== false
    }
  }
  if (Object.keys(filters).length) {
    config.filters = filters
  }
  return {
    name: String(form.name || '').trim(),
    description: String(form.description || '').trim(),
    enabled: form.enabled !== false,
    schedule: String(form.schedule || '').trim(),
    config
  }
}

const loadResults = async () => {
  resultsLoading.value = true
  try {
    const params = {
      page: resultsPage.value,
      page_size: resultsPageSize.value
    }
    if (Number(resultFilters.engine_id) > 0) {
      params.engine_id = Number(resultFilters.engine_id)
    }
    if (Number(resultFilters.item_id) > 0) {
      params.item_id = Number(resultFilters.item_id)
    }
    if (Number(resultFilters.node_id) > 0) {
      params.node_id = Number(resultFilters.node_id)
    }
    if (String(resultFilters.status || '').trim()) {
      params.status = String(resultFilters.status).trim()
    }
    if (String(resultFilters.q || '').trim()) {
      params.q = String(resultFilters.q).trim()
    }
    const response = await client.get('/manager/embeddings', {
      params
    })
    results.value = response.data || []
    resultsTotal.value = response.total || 0
  } catch (error) {
    console.error('加载向量化结果失败:', error)
    ElMessage.error(t('manager.vectorization.loadResultsFailed'))
  } finally {
    resultsLoading.value = false
  }
}

const loadTasks = async () => {
  tasksLoading.value = true
  try {
    const response = await client.get('/manager/embedding_tasks', {
      params: {
        page: tasksPage.value,
        page_size: tasksPageSize.value
      }
    })
    tasks.value = response.data || []
    tasksTotal.value = response.total || 0
  } catch (error) {
    console.error('加载向量化任务定义失败:', error)
    ElMessage.error(t('manager.vectorization.loadTasksFailed'))
  } finally {
    tasksLoading.value = false
  }
}

const refreshActiveTab = () => {
  if (activeTab.value === 'results') {
    loadResults()
  } else {
    loadTasks()
  }
}

const handleTabChange = () => {
  refreshActiveTab()
}

const handleResultsSizeChange = () => {
  resultsPage.value = 1
  loadResults()
}

const applyResultFilters = () => {
  resultsPage.value = 1
  loadResults()
}

const resetResultFilters = () => {
  Object.assign(resultFilters, defaultResultFilters())
  applyResultFilters()
}

const handleTasksSizeChange = () => {
  tasksPage.value = 1
  loadTasks()
}

const openCreateDialog = () => {
  resetForm()
  formDialogVisible.value = true
}

const openEditDialog = (task) => {
  resetForm(task)
  formDialogVisible.value = true
}

const saveTask = async () => {
  await formRef.value?.validate()
  saving.value = true
  try {
    if (editingId.value) {
      await client.put(`/manager/embedding_tasks/${editingId.value}`, taskPayload())
      ElMessage.success(t('manager.vectorization.updateSuccess'))
    } else {
      await client.post('/manager/embedding_tasks', taskPayload())
      ElMessage.success(t('manager.vectorization.createSuccess'))
    }
    formDialogVisible.value = false
    await loadTasks()
  } catch (error) {
    console.error('保存向量化任务失败:', error)
    ElMessage.error(error.response?.data?.message || t('manager.vectorization.saveFailed'))
  } finally {
    saving.value = false
  }
}

const showTaskDetail = (task) => {
  selectedTask.value = task
  detailDialogVisible.value = true
}

const openTaskFromQuery = async () => {
  const taskId = Number(route.query.task_id || 0)
  if (!taskId) return
  activeTab.value = 'tasks'
  try {
    const response = await client.get(`/manager/embedding_tasks/${taskId}`)
    selectedTask.value = response.data || response
    detailDialogVisible.value = true
  } catch (error) {
    console.error('加载向量化任务详情失败:', error)
    ElMessage.error(t('manager.vectorization.loadTasksFailed'))
  } finally {
    router.replace({ query: { ...route.query, task_id: undefined } })
  }
}

const executeTask = async (task) => {
  executingId.value = task.id
  try {
    const response = await client.post(`/manager/tasks/embedding/${task.id}/execute`, {
      trigger_type: 'manual',
      source: 'manager'
    })
    ElMessage.success(t('manager.vectorization.executeSubmitted', { id: response.execution_id || '-' }))
    await loadTasks()
    await openMonitorExecution(response.execution_id)
  } catch (error) {
    console.error('执行向量化任务失败:', error)
    ElMessage.error(t('manager.vectorization.executeFailed'))
  } finally {
    executingId.value = null
  }
}

const viewTaskResults = async (task) => {
  Object.assign(resultFilters, defaultResultFilters(), {
    engine_id: Number(task.target?.engine_id || 0) || null,
    node_id: Number(task.target?.node_id || 0) || null
  })
  resultsPage.value = 1
  activeTab.value = 'results'
  await loadResults()
}

const openTaskExecution = async (task) => {
  if (!task.last_execution_id) return
  await openMonitorExecution(task.last_execution_id)
}

const deleteTask = async (task) => {
  await ElMessageBox.confirm(t('manager.vectorization.deleteTaskConfirm'), t('manager.vectorization.delete'), {
    type: 'warning'
  })
  await client.delete(`/manager/embedding_tasks/${task.id}`)
  ElMessage.success(t('manager.vectorization.deleteSuccess'))
  await loadTasks()
}

const revectorizeResult = async (result) => {
  revectorizingId.value = result.id
  try {
    const response = await client.post('/manager/embedding_executions', {
      scope: 'item',
      target: {
        engine_id: result.engine_id,
        item_id: result.item_id,
        locator: result.locator
      }
    })
    ElMessage.success(t('manager.vectorization.executeSubmitted', { id: response.execution_id || '-' }))
    await openMonitorExecution(response.execution_id)
  } catch (error) {
    console.error('重新向量化失败:', error)
    ElMessage.error(t('manager.vectorization.executeFailed'))
  } finally {
    revectorizingId.value = null
  }
}

const deleteResult = async (result) => {
  await ElMessageBox.confirm(t('manager.vectorization.deleteResultConfirm'), t('manager.vectorization.delete'), {
    type: 'warning'
  })
  await client.delete(`/manager/embeddings/${result.id}`)
  ElMessage.success(t('manager.vectorization.deleteSuccess'))
  await loadResults()
}

const executionStatusTagType = (status) => {
  switch (status) {
    case 'success':
      return 'success'
    case 'failed':
    case 'timeout':
      return 'danger'
    case 'running':
    case 'pending':
      return 'warning'
    case 'cancelled':
      return 'info'
    default:
      return 'info'
  }
}

const executionStatusLabel = (status) => {
  if (!status) return t('manager.vectorization.statusNeverRun')
  if (!['pending', 'running', 'success', 'failed', 'timeout', 'cancelled'].includes(status)) {
    return status
  }
  return t(`manager.vectorization.status.${status}`)
}

const embeddingStatusTagType = (status) => {
  switch (status) {
    case 'ready':
      return 'success'
    case 'outdated':
      return 'warning'
    case 'failed':
    case 'missing_source':
      return 'danger'
    case 'unsupported':
      return 'info'
    default:
      return 'info'
  }
}

const embeddingStatusLabel = (status) => {
  if (!status) return '-'
  if (!embeddingStatuses.includes(status)) {
    return status
  }
  return t(`manager.vectorization.embeddingStatus.${status}`)
}

onMounted(async () => {
  await loadResults()
  await loadTasks()
  await openTaskFromQuery()
})
</script>

<style scoped>
.vectorization {
  padding: 20px;
}

.card-header,
.header-actions {
  display: flex;
  align-items: center;
}

.card-header {
  justify-content: space-between;
}

.header-actions {
  gap: 8px;
}

.pagination {
  margin-top: 20px;
  justify-content: center;
}

.filter-bar {
  display: grid;
  grid-template-columns: 140px 140px 140px 170px minmax(220px, 1fr) auto auto;
  gap: 10px;
  margin-bottom: 14px;
}

@media (max-width: 900px) {
  .filter-bar {
    grid-template-columns: 1fr 1fr;
  }
}
</style>
