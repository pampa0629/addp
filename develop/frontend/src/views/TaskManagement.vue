<template>
  <div class="task-management-page">
    <!-- 工具栏 -->
    <div class="toolbar">
      <div class="toolbar-left">
        <h2>{{ t('develop.taskManagement.title') }}</h2>
        <el-button type="primary" @click="handleCreate">
          <el-icon><Plus /></el-icon>
          {{ t('develop.taskManagement.newTask') }}
        </el-button>
        <el-button @click="handleRefresh">
          <el-icon><Refresh /></el-icon>
          {{ t('develop.taskManagement.refresh') }}
        </el-button>
      </div>
      <div class="toolbar-right">
        <!-- 筛选器 -->
        <el-input
          v-model="filters.keyword"
          :placeholder="t('develop.taskManagement.searchPlaceholder')"
          clearable
          style="width: 200px; margin-right: 10px;"
        >
          <template #prefix>
            <el-icon><Search /></el-icon>
          </template>
        </el-input>
        <el-select
          v-model="filters.dev_type"
          :placeholder="t('develop.taskManagement.filterType')"
          clearable
          style="width: 120px; margin-right: 10px;"
        >
          <el-option :label="t('develop.taskManagement.typeQuery')" value="query" />
          <el-option :label="t('develop.taskManagement.typeWorkflow')" value="workflow" />
          <el-option :label="t('develop.taskManagement.typeScript')" value="script" />
        </el-select>
        <el-select
          v-model="filters.status"
          :placeholder="t('develop.taskManagement.filterStatus')"
          clearable
          style="width: 120px;"
        >
          <el-option :label="t('develop.queryTasks.statusActive')" value="active" />
          <el-option :label="t('develop.queryTasks.statusInactive')" value="inactive" />
          <el-option :label="t('develop.queryTasks.statusArchived')" value="archived" />
        </el-select>
      </div>
    </div>

    <!-- 任务列表 -->
    <div class="content-area">
      <el-table
        v-loading="loading"
        :data="tasks"
        stripe
        style="width: 100%"
        height="100%"
      >
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column prop="name" :label="t('develop.taskManagement.colName')" min-width="150" show-overflow-tooltip />
        <el-table-column :label="t('develop.taskManagement.colType')" width="100">
          <template #default="{ row }">
            <el-tag :type="getTypeColor(row.dev_type)">
              {{ getTypeLabel(row.dev_type) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('develop.taskManagement.colResource')" width="150">
          <template #default="{ row }">
            {{ getResourceName(row.engine_id) }}
          </template>
        </el-table-column>
        <el-table-column :label="t('develop.taskManagement.colStatus')" width="100">
          <template #default="{ row }">
            <el-tag :type="getStatusColor(row.status)">
              {{ getStatusLabel(row.status) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('develop.taskManagement.colLastExecution')" width="180">
          <template #default="{ row }">
            <div v-if="row.last_executed_at">
              <div>{{ formatTime(row.last_executed_at) }}</div>
              <el-tag
                v-if="row.last_execution_status"
                :type="getExecutionStatusColor(row.last_execution_status)"
                size="small"
                style="margin-top: 4px;"
              >
                {{ row.last_execution_status }}
              </el-tag>
            </div>
            <span v-else class="text-muted">{{ t('develop.taskManagement.notExecuted') }}</span>
          </template>
        </el-table-column>
        <el-table-column :label="t('develop.taskManagement.colCreatedAt')" width="160">
          <template #default="{ row }">
            {{ formatTime(row.created_at) }}
          </template>
        </el-table-column>
        <el-table-column :label="t('develop.taskManagement.colActions')" width="260" fixed="right">
          <template #default="{ row }">
            <el-button
              type="primary"
              size="small"
              @click="handleExecute(row)"
            >
              <el-icon><VideoPlay /></el-icon>
              {{ t('develop.taskManagement.execute') }}
            </el-button>
            <el-button
              type="default"
              size="small"
              @click="handleEdit(row)"
            >
              <el-icon><Edit /></el-icon>
              {{ t('develop.taskManagement.edit') }}
            </el-button>
            <el-button
              type="info"
              size="small"
              @click="handleView(row)"
            >
              <el-icon><View /></el-icon>
              {{ t('develop.taskManagement.detail') }}
            </el-button>
            <el-button
              type="danger"
              size="small"
              @click="handleDelete(row)"
            >
              <el-icon><Delete /></el-icon>
              {{ t('develop.taskManagement.delete') }}
            </el-button>
          </template>
        </el-table-column>
      </el-table>

      <!-- 分页 -->
      <div class="pagination">
        <el-pagination
          v-model:current-page="pagination.page"
          v-model:page-size="pagination.page_size"
          :total="pagination.total"
          :page-sizes="[10, 20, 50, 100]"
          layout="total, sizes, prev, pager, next, jumper"
          @size-change="handlePageSizeChange"
          @current-change="handlePageChange"
        />
      </div>
    </div>

    <!-- 创建/编辑对话框 -->
    <el-dialog
      v-model="dialogVisible"
      :title="dialogMode === 'create' ? t('develop.taskManagement.createDialogTitle') : t('develop.taskManagement.editDialogTitle')"
      width="600px"
    >
      <el-form :model="formData" label-width="100px">
        <el-form-item :label="t('develop.taskManagement.fieldName')" required>
          <el-input v-model="formData.name" :placeholder="t('develop.taskManagement.namePlaceholder')" />
        </el-form-item>
        <el-form-item :label="t('develop.taskManagement.fieldDisplayName')">
          <el-input v-model="formData.display_name" :placeholder="t('develop.taskManagement.optional')" />
        </el-form-item>
        <el-form-item :label="t('develop.taskManagement.fieldType')" required>
          <el-select v-model="formData.dev_type" style="width: 100%;">
            <el-option :label="t('develop.taskManagement.typeQuery')" value="query" />
            <el-option :label="t('develop.taskManagement.typeWorkflow')" value="workflow" />
            <el-option :label="t('develop.taskManagement.typeScript')" value="script" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('develop.taskManagement.fieldResource')">
          <el-select
            v-model="formData.engine_id"
            style="width: 100%;"
            clearable
            :placeholder="t('develop.taskManagement.resourcePlaceholder')"
          >
            <el-option
              v-for="res in engines"
              :key="res.id"
              :label="res.name"
              :value="res.id"
            />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('develop.taskManagement.fieldDescription')">
          <el-input
            v-model="formData.description"
            type="textarea"
            :rows="3"
            :placeholder="t('develop.taskManagement.descriptionPlaceholder')"
          />
        </el-form-item>
        <el-form-item :label="t('develop.taskManagement.fieldTags')">
          <el-tag
            v-for="tag in formData.tags"
            :key="tag"
            closable
            @close="removeTag(tag)"
            style="margin-right: 8px;"
          >
            {{ tag }}
          </el-tag>
          <el-input
            v-if="tagInputVisible"
            ref="tagInputRef"
            v-model="tagInputValue"
            size="small"
            style="width: 100px;"
            @blur="handleTagInputConfirm"
            @keyup.enter="handleTagInputConfirm"
          />
          <el-button
            v-else
            size="small"
            @click="showTagInput"
          >
            + {{ t('develop.taskManagement.addTag') }}
          </el-button>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">{{ t('develop.taskManagement.cancel') }}</el-button>
        <el-button type="primary" @click="handleSave">{{ t('develop.taskManagement.save') }}</el-button>
      </template>
    </el-dialog>

    <!-- 执行对话框 -->
    <el-dialog
      v-model="executeDialogVisible"
      :title="t('develop.taskManagement.executeDialogTitle')"
      width="500px"
    >
      <el-form label-width="100px">
        <el-form-item :label="t('develop.taskManagement.fieldName')">
          <el-input :value="currentTask?.name" disabled />
        </el-form-item>
        <el-form-item :label="t('develop.taskManagement.fieldType')">
          <el-tag :type="getTypeColor(currentTask?.dev_type)">
            {{ getTypeLabel(currentTask?.dev_type) }}
          </el-tag>
        </el-form-item>
        <el-form-item :label="t('develop.taskManagement.execParams')">
          <el-input
            v-model="executeInputs"
            type="textarea"
            :rows="5"
            placeholder='{"key": "value"}'
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="executeDialogVisible = false">{{ t('develop.taskManagement.cancel') }}</el-button>
        <el-button
          type="primary"
          @click="confirmExecute"
          :loading="executing"
        >
          {{ t('develop.taskManagement.execute') }}
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted, watch, nextTick } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  Plus,
  Refresh,
  Search,
  VideoPlay,
  Edit,
  View,
  Delete
} from '@element-plus/icons-vue'
import {
  listDevTasks,
  createDevTask,
  updateDevTask,
  deleteDevTask,
  executeDevTask,
  listEngines
} from '@/api/devTask'
import { openMonitorExecution } from '@addp/common-frontend'

const router = useRouter()
const route = useRoute()
const { t } = useI18n()
const supportedDevTypes = ['query', 'workflow', 'script']

// 状态管理
const loading = ref(false)
const tasks = ref([])
const engines = ref([])

// 筛选器
const filters = reactive({
  keyword: '',
  dev_type: '',
  status: '',
  engine_id: null,
  tag: ''
})

// 分页
const pagination = reactive({
  page: 1,
  page_size: 20,
  total: 0
})

// 对话框状态
const dialogVisible = ref(false)
const dialogMode = ref('create') // 'create' | 'edit'
const formData = reactive({
  name: '',
  display_name: '',
  dev_type: 'query',
  engine_id: null,
  description: '',
  tags: [],
  content: {}
})

// 标签输入
const tagInputVisible = ref(false)
const tagInputValue = ref('')
const tagInputRef = ref(null)

// 执行对话框
const executeDialogVisible = ref(false)
const currentTask = ref(null)
const executeInputs = ref('{}')
const executing = ref(false)

// 加载任务列表
const loadTasks = async () => {
  loading.value = true
  try {
    const params = {
      page: pagination.page,
      page_size: pagination.page_size,
      ...filters
    }
    const data = await listDevTasks(params)
    tasks.value = data.items || []
    pagination.total = data.total || 0
  } catch (error) {
    console.error('加载任务失败:', error)
    ElMessage.error(t('develop.taskManagement.loadFailed') + (error.response?.data?.error || error.message))
  } finally {
    loading.value = false
  }
}

// 加载资源列表
const loadEngines = async () => {
  try {
    const data = await listEngines()
    engines.value = data || []
  } catch (error) {
    console.error('加载资源失败:', error)
  }
}

// 工具函数
const getTypeLabel = (type) => {
  const labels = {
    query: t('develop.taskManagement.typeQuery'),
    workflow: t('develop.taskManagement.typeWorkflow'),
    script: t('develop.taskManagement.typeScript')
  }
  return labels[type] || type
}

const getTypeColor = (type) => {
  const colors = { query: 'primary', workflow: 'success', script: 'info' }
  return colors[type] || 'info'
}

const getStatusLabel = (status) => {
  const labels = {
    active: t('develop.queryTasks.statusActive'),
    inactive: t('develop.queryTasks.statusInactive'),
    archived: t('develop.queryTasks.statusArchived')
  }
  return labels[status] || status
}

const getStatusColor = (status) => {
  const colors = { active: 'success', inactive: 'warning', archived: 'info' }
  return colors[status] || 'info'
}

const getExecutionStatusColor = (status) => {
  const colors = {
    success: 'success',
    failed: 'danger',
    running: 'primary',
    timeout: 'warning',
    cancelled: 'info'
  }
  return colors[status] || 'info'
}

const getResourceName = (engineId) => {
  if (!engineId) return '-'
  const resource = engines.value.find(r => r.id === engineId)
  return resource?.name || `ID: ${engineId}`
}

const formatTime = (time) => {
  if (!time) return '-'
  return new Date(time).toLocaleString('zh-CN')
}

// 操作函数
const normalizeDevType = (type) => {
  return supportedDevTypes.includes(type) ? type : 'query'
}

const resetFormData = (devType = 'query') => {
  Object.assign(formData, {
    id: undefined,
    name: '',
    display_name: '',
    dev_type: normalizeDevType(devType),
    engine_id: null,
    description: '',
    tags: [],
    content: {}
  })
}

const handleCreate = (devType = 'query') => {
  dialogMode.value = 'create'
  resetFormData(devType)
  dialogVisible.value = true
}

const handleEdit = (row) => {
  dialogMode.value = 'edit'
  Object.assign(formData, {
    id: row.id,
    name: row.name,
    display_name: row.display_name,
    dev_type: row.dev_type,
    engine_id: row.engine_id,
    description: row.description,
    tags: row.tags || [],
    content: row.content || {}
  })
  dialogVisible.value = true
}

const handleView = (row) => {
  // 根据类型跳转到对应编辑器,传递任务 ID
  if (row.dev_type === 'query') {
    router.push({ path: '/sql', query: { taskId: row.id } })
  } else if (row.dev_type === 'workflow') {
    router.push({ path: '/workflow', query: { taskId: row.id } })
  } else {
    ElMessage.info(t('develop.taskManagement.viewNotSupported'))
  }
}

const handleRouteAction = () => {
  const action = String(route.query.action || '')
  if (action === 'create') {
    handleCreate(String(route.query.task_type || 'query'))
    return
  }

  if (action === 'edit' && route.query.id) {
    const targetID = Number(route.query.id)
    const target = tasks.value.find(task => Number(task.id) === targetID)
    if (target) {
      handleEdit(target)
    } else {
      ElMessage.warning(t('develop.taskManagement.taskNotFound'))
    }
  }
}

const handleExecute = (row) => {
  currentTask.value = row
  executeInputs.value = '{}'
  executeDialogVisible.value = true
}

const confirmExecute = async () => {
  if (!currentTask.value) return

  try {
    executing.value = true
    let inputs = {}
    try {
      inputs = JSON.parse(executeInputs.value)
    } catch {
      ElMessage.warning(t('develop.taskManagement.invalidJson'))
      return
    }

    const execution = await executeDevTask(currentTask.value.id, inputs)
    ElMessage.success(t('develop.taskManagement.executeSubmitted'))
    executeDialogVisible.value = false
    loadTasks() // 刷新列表
    await openMonitorExecution(execution.execution_id)
  } catch (error) {
    console.error('执行任务失败:', error)
    ElMessage.error(t('develop.taskManagement.executeFailed') + (error.response?.data?.error || error.message))
  } finally {
    executing.value = false
  }
}

const handleDelete = async (row) => {
  try {
    await ElMessageBox.confirm(
      t('develop.taskManagement.deleteConfirmMsg', { name: row.name }),
      t('develop.taskManagement.deleteConfirmTitle'),
      { type: 'warning' }
    )
    await deleteDevTask(row.id)
    ElMessage.success(t('develop.taskManagement.deleteSuccess'))
    loadTasks()
  } catch (error) {
    if (error !== 'cancel') {
      console.error('删除任务失败:', error)
      ElMessage.error(t('develop.taskManagement.deleteFailed') + (error.response?.data?.error || error.message))
    }
  }
}

const handleSave = async () => {
  if (!formData.name) {
    ElMessage.warning(t('develop.taskManagement.nameRequired'))
    return
  }

  try {
    if (dialogMode.value === 'create') {
      await createDevTask(formData)
      ElMessage.success(t('develop.taskManagement.createSuccess'))
    } else {
      await updateDevTask(formData.id, formData)
      ElMessage.success(t('develop.taskManagement.updateSuccess'))
    }
    dialogVisible.value = false
    loadTasks()
  } catch (error) {
    console.error('保存任务失败:', error)
    ElMessage.error(t('develop.taskManagement.saveFailed') + (error.response?.data?.error || error.message))
  }
}

const handleRefresh = () => {
  loadTasks()
}

const handlePageChange = () => {
  loadTasks()
}

const handlePageSizeChange = () => {
  pagination.page = 1
  loadTasks()
}

// 标签操作
const showTagInput = () => {
  tagInputVisible.value = true
  nextTick(() => {
    tagInputRef.value?.focus()
  })
}

const handleTagInputConfirm = () => {
  if (tagInputValue.value) {
    if (!formData.tags.includes(tagInputValue.value)) {
      formData.tags.push(tagInputValue.value)
    }
  }
  tagInputVisible.value = false
  tagInputValue.value = ''
}

const removeTag = (tag) => {
  formData.tags.splice(formData.tags.indexOf(tag), 1)
}

// 监听筛选器变化
watch([filters], () => {
  pagination.page = 1
  loadTasks()
}, { deep: true })

watch(() => route.query, () => {
  handleRouteAction()
}, { deep: true })

// 初始化
onMounted(async () => {
  await loadTasks()
  await loadEngines()
  handleRouteAction()
})
</script>

<style scoped>
.task-management-page {
  display: flex;
  flex-direction: column;
  height: 100vh;
  background: var(--addp-bg-secondary);
}

.toolbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 12px 20px;
  background: var(--addp-bg-primary);
  border-bottom: 1px solid var(--addp-border-color);
}

.toolbar-left,
.toolbar-right {
  display: flex;
  align-items: center;
  gap: 12px;
}

.toolbar h2 {
  margin: 0;
  font-size: 18px;
  color: var(--addp-text-primary);
  font-weight: 500;
  margin-right: 20px;
}

.content-area {
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  padding: 16px;
}

.pagination {
  margin-top: 16px;
  display: flex;
  justify-content: flex-end;
}

.text-muted {
  color: var(--addp-text-tertiary);
  font-style: italic;
}
</style>
