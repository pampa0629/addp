<template>
  <div class="metadata-container">
    <el-card shadow="never">
      <template #header>
        <div class="card-header">
          <h2>元数据管理</h2>
          <p class="subtitle">浏览和管理数据库表及对象存储文件</p>
        </div>
      </template>

      <div class="datasource-selector">
        <el-select
          v-model="selectedDataSourceId"
          placeholder="选择数据源"
          @change="handleDataSourceChange"
          size="large"
          style="width: 300px"
        >
          <el-option
            v-for="ds in dataSources"
            :key="ds.id"
            :label="ds.name"
            :value="ds.id"
          >
            <span>{{ ds.name }}</span>
            <el-tag size="small" type="info" style="margin-left: 8px">{{ ds.resource_type }}</el-tag>
          </el-option>
        </el-select>

        <el-button
          type="primary"
          @click="handleScanMetadata"
          :loading="scanning"
          :disabled="!selectedDataSourceId"
        >
          <el-icon><Refresh /></el-icon>
          扫描元数据
        </el-button>
      </div>

      <el-tabs v-model="activeTab" class="metadata-tabs">
        <el-tab-pane label="元数据" name="metadata">
          <div
            v-if="selectedDataSource && selectedDataSource.resource_type === 'postgresql'"
            class="table-section"
          >
            <div class="section-header">
              <h3>数据库表</h3>
              <div class="filter-group">
                <el-radio-group v-model="tableFilter" @change="loadTables">
                  <el-radio-button label="all">全部 ({{ scanResult?.total_items || 0 }})</el-radio-button>
                  <el-radio-button label="managed">已纳管 ({{ scanResult?.managed_items || 0 }})</el-radio-button>
                  <el-radio-button label="unmanaged">未纳管 ({{ scanResult?.unmanaged_items || 0 }})</el-radio-button>
                </el-radio-group>
              </div>
            </div>

            <el-table
              :data="tables"
              v-loading="loadingTables"
              stripe
              style="width: 100%; margin-top: 16px"
            >
              <el-table-column prop="full_name" label="表名" min-width="200">
                <template #default="{ row }">
                  <el-tooltip :content="`${row.schema_name}.${row.table_name}`" placement="top">
                    <span>{{ row.full_name }}</span>
                  </el-tooltip>
                </template>
              </el-table-column>
              <el-table-column prop="table_type" label="类型" width="120">
                <template #default="{ row }">
                  <el-tag size="small" :type="row.table_type === 'BASE TABLE' ? '' : 'info'">
                    {{ row.table_type }}
                  </el-tag>
                </template>
              </el-table-column>
              <el-table-column prop="table_size" label="大小" width="120">
                <template #default="{ row }">
                  {{ formatBytes(row.table_size) }}
                </template>
              </el-table-column>
              <el-table-column prop="row_count" label="行数" width="120">
                <template #default="{ row }">
                  {{ row.row_count !== null && row.row_count !== undefined ? row.row_count.toLocaleString() : '-' }}
                </template>
              </el-table-column>
              <el-table-column prop="is_managed" label="状态" width="100">
                <template #default="{ row }">
                  <el-tag :type="row.is_managed ? 'success' : 'info'" size="small">
                    {{ row.is_managed ? '已纳管' : '未纳管' }}
                  </el-tag>
                </template>
              </el-table-column>
              <el-table-column prop="last_scanned" label="最后扫描" width="180">
                <template #default="{ row }">
                  {{ row.last_scanned ? formatDateTime(row.last_scanned) : '-' }}
                </template>
              </el-table-column>
              <el-table-column label="操作" width="200" fixed="right">
                <template #default="{ row }">
                  <el-button
                    v-if="!row.is_managed"
                    type="primary"
                    size="small"
                    @click="handleManageTable(row)"
                    :loading="managingTableId === row.id"
                  >
                    纳管
                  </el-button>
                  <el-button
                    v-else
                    type="warning"
                    size="small"
                    @click="handleUnmanageTable(row)"
                    :loading="managingTableId === row.id"
                  >
                    取消纳管
                  </el-button>
                  <el-button
                    v-if="row.is_managed"
                    type="info"
                    size="small"
                    @click="handleViewMetadata(row)"
                  >
                    查看元数据
                  </el-button>
                </template>
              </el-table-column>
            </el-table>
          </div>

          <div
            v-if="selectedDataSource && selectedDataSource.resource_type === 'minio'"
            class="minio-section"
          >
            <el-empty description="对象存储元数据管理功能开发中">
              <el-tag type="info">MinIO 元数据扫描功能即将推出</el-tag>
            </el-empty>
          </div>

          <div v-if="!selectedDataSourceId" class="empty-state">
            <el-empty description="请先选择一个数据源进行元数据管理" />
          </div>
        </el-tab-pane>

        <el-tab-pane label="扫描任务" name="tasks">
          <div v-if="selectedDataSourceId" class="tasks-section">
            <div class="tasks-toolbar">
              <div class="tasks-actions">
                <el-button type="primary" @click="openTaskDialog">
                  新建任务
                </el-button>
                <el-button type="success" @click="handleManualScan" :loading="manualScanning">
                  <el-icon><Refresh /></el-icon>
                  立即扫描
                </el-button>
              </div>
              <div class="tasks-refresh">
                <el-button text @click="loadScanTasks(true)" :loading="loadingTasks">刷新任务</el-button>
                <el-button text @click="loadScanRuns()" :loading="loadingRuns">刷新运行</el-button>
              </div>
            </div>

            <el-table
              :data="scanTasks"
              v-loading="loadingTasks"
              class="tasks-table"
              empty-text="暂无扫描任务"
              style="width: 100%"
            >
              <el-table-column label="任务名称" min-width="220">
                <template #default="{ row }">
                  <div class="task-name">
                    <div class="task-name__title">{{ row.name }}</div>
                    <div v-if="row.description" class="task-name__desc">{{ row.description }}</div>
                  </div>
                </template>
              </el-table-column>
              <el-table-column label="调度" min-width="240">
                <template #default="{ row }">
                  {{ row.schedule ? describeCron(row.schedule) : '手动触发' }}
                </template>
              </el-table-column>
              <el-table-column label="状态" width="120">
                <template #default="{ row }">
                  <el-tag :type="row.enabled ? 'success' : 'info'" size="small">
                    {{ row.enabled ? '已启用' : '已停用' }}
                  </el-tag>
                </template>
              </el-table-column>
              <el-table-column label="上次执行" width="170">
                <template #default="{ row }">
                  {{ row.last_run_at ? formatDateTime(row.last_run_at) : '-' }}
                </template>
              </el-table-column>
              <el-table-column label="下次执行" width="170">
                <template #default="{ row }">
                  {{ row.next_run_at ? formatDateTime(row.next_run_at) : '-' }}
                </template>
              </el-table-column>
              <el-table-column label="操作" width="240" fixed="right">
                <template #default="{ row }">
                  <el-button type="primary" link @click="openTaskDialog(row)">编辑</el-button>
                  <el-button
                    type="warning"
                    link
                    @click="handleToggleTask(row)"
                    :loading="togglingTaskId === row.id"
                  >
                    {{ row.enabled ? '停用' : '启用' }}
                  </el-button>
                  <el-button
                    type="success"
                    link
                    @click="handleTriggerTask(row)"
                    :loading="triggeringTaskId === row.id"
                  >
                    触发
                  </el-button>
                  <el-button
                    type="danger"
                    link
                    @click="handleDeleteTask(row)"
                    :loading="deletingTaskId === row.id"
                  >
                    删除
                  </el-button>
                </template>
              </el-table-column>
            </el-table>

            <el-divider />

            <div class="runs-header">
              <h3>运行监控</h3>
              <el-tag type="info" size="small">自动每 8 秒刷新一次</el-tag>
            </div>

            <el-table
              :data="scanRuns"
              v-loading="loadingRuns"
              class="runs-table"
              empty-text="暂无运行记录"
              style="width: 100%"
            >
              <el-table-column label="任务" min-width="220">
                <template #default="{ row }">
                  <div class="run-task">
                    <div class="run-task__name">{{ runDisplayName(row) }}</div>
                    <div class="run-task__meta">
                      <el-tag size="small" type="info" effect="plain" class="run-trigger-tag">
                        {{ formatTriggerType(row.trigger_type) }}
                      </el-tag>
                      <el-tag
                        v-if="row.storage_type"
                        size="small"
                        type="info"
                        effect="plain"
                      >
                        {{ row.storage_type }}
                      </el-tag>
                    </div>
                  </div>
                </template>
              </el-table-column>
              <el-table-column label="状态" width="140">
                <template #default="{ row }">
                  <el-tag :type="statusTagType(row.status)" size="small">
                    {{ formatRunStatus(row.status) }}
                  </el-tag>
                </template>
              </el-table-column>
              <el-table-column label="进度" width="220">
                <template #default="{ row }">
                  <el-progress
                    :text-inside="true"
                    :stroke-width="18"
                    :percentage="calculateProgress(row)"
                    :status="progressStatus(row)"
                  />
                </template>
              </el-table-column>
              <el-table-column label="开始时间" width="170">
                <template #default="{ row }">
                  {{ row.started_at ? formatDateTime(row.started_at) : '-' }}
                </template>
              </el-table-column>
              <el-table-column label="完成时间" width="170">
                <template #default="{ row }">
                  {{ row.completed_at ? formatDateTime(row.completed_at) : '-' }}
                </template>
              </el-table-column>
              <el-table-column label="最后状态" min-width="200">
                <template #default="{ row }">
                  {{ row.progress_message || '-' }}
                </template>
              </el-table-column>
              <el-table-column label="操作" width="120" fixed="right">
                <template #default="{ row }">
                  <el-button type="primary" link @click="viewRunDetail(row)">详情</el-button>
                </template>
              </el-table-column>
            </el-table>
          </div>
          <div v-else class="empty-state">
            <el-empty description="请先选择一个数据源进行配置" />
          </div>
        </el-tab-pane>
      </el-tabs>
    </el-card>

    <el-dialog
      v-model="metadataDialogVisible"
      title="表元数据详情"
      width="80%"
      destroy-on-close
    >
      <div v-if="selectedTable" class="metadata-detail">
        <el-descriptions :column="2" border>
          <el-descriptions-item label="完整表名">{{ selectedTable.full_name }}</el-descriptions-item>
          <el-descriptions-item label="表类型">{{ selectedTable.table_type }}</el-descriptions-item>
          <el-descriptions-item label="Schema">{{ selectedTable.schema_name }}</el-descriptions-item>
          <el-descriptions-item label="表名">{{ selectedTable.table_name }}</el-descriptions-item>
          <el-descriptions-item label="行数" :span="2">
            {{ selectedTable.row_count?.toLocaleString() || '-' }}
          </el-descriptions-item>
          <el-descriptions-item label="大小">{{ formatBytes(selectedTable.table_size) }}</el-descriptions-item>
          <el-descriptions-item label="纳管时间" :span="2">
            {{ selectedTable.last_managed ? formatDateTime(selectedTable.last_managed) : '-' }}
          </el-descriptions-item>
          <el-descriptions-item label="注释" :span="2">
            {{ selectedTable.comment || '无' }}
          </el-descriptions-item>
        </el-descriptions>

        <el-divider>字段信息</el-divider>
        <el-table
          v-if="selectedTable.schema"
          :data="parseSchema(selectedTable.schema)"
          border
          style="width: 100%"
        >
          <el-table-column prop="name" label="字段名" width="200" />
          <el-table-column prop="data_type" label="数据类型" width="150" />
          <el-table-column prop="is_nullable" label="可空" width="80">
            <template #default="{ row }">
              <el-tag :type="row.is_nullable ? 'info' : 'warning'" size="small">
                {{ row.is_nullable ? '是' : '否' }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="is_primary_key" label="主键" width="80">
            <template #default="{ row }">
              <el-icon v-if="row.is_primary_key" color="#67C23A"><Key /></el-icon>
            </template>
          </el-table-column>
          <el-table-column prop="default_value" label="默认值" min-width="150">
            <template #default="{ row }">
              {{ row.default_value || '-' }}
            </template>
          </el-table-column>
          <el-table-column prop="comment" label="注释" min-width="200">
            <template #default="{ row }">
              {{ row.comment || '-' }}
            </template>
          </el-table-column>
        </el-table>

        <el-divider>采样数据 (前10行)</el-divider>
        <div v-if="selectedTable.sample_data" class="sample-data">
          <el-table
            :data="parseSampleData(selectedTable.sample_data)"
            border
            max-height="400"
            style="width: 100%"
          >
            <el-table-column
              v-for="column in getSampleDataColumns(selectedTable.sample_data)"
              :key="column"
              :prop="column"
              :label="column"
              min-width="120"
              show-overflow-tooltip
            >
              <template #default="{ row }">
                {{ formatCellValue(row[column]) }}
              </template>
            </el-table-column>
          </el-table>
        </div>
      </div>
    </el-dialog>

    <el-dialog
      v-model="taskDialogVisible"
      :title="taskDialogTitle"
      width="580px"
      @close="handleTaskDialogClose"
    >
      <el-form ref="taskFormRef" :model="taskForm" :rules="taskFormRules" label-width="100px">
        <el-form-item label="任务名称" prop="name">
          <el-input v-model="taskForm.name" placeholder="请输入任务名称" />
        </el-form-item>
        <el-form-item label="描述">
          <el-input
            v-model="taskForm.description"
            type="textarea"
            :rows="2"
            placeholder="任务用途，可选"
          />
        </el-form-item>
        <el-form-item label="Schema 列表">
          <el-input
            v-model="taskForm.schemaInput"
            placeholder="逗号分隔，例如: public, sales"
          />
        </el-form-item>
        <el-form-item label="对象路径">
          <el-input
            v-model="taskForm.objectPathInput"
            placeholder="对象存储路径，逗号分隔"
          />
        </el-form-item>
        <el-form-item label="扫描深度">
          <el-select v-model="taskForm.scanDepth" placeholder="选择扫描深度">
            <el-option
              v-for="item in scanDepthOptions"
              :key="item.value"
              :label="item.label"
              :value="item.value"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="定时调度">
          <ScheduleConfig
            v-model="taskForm.schedule"
            :allow-custom-cron="true"
          />
        </el-form-item>
        <el-form-item label="是否启用">
          <el-switch v-model="taskForm.enabled" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="taskDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="submitTaskForm" :loading="savingTask">保存</el-button>
      </template>
    </el-dialog>

    <el-dialog
      v-model="runDetailVisible"
      title="运行详情"
      width="640px"
    >
      <div v-if="viewingRun" class="run-detail">
        <el-descriptions :column="2" border>
          <el-descriptions-item label="运行 ID">{{ viewingRun.id }}</el-descriptions-item>
          <el-descriptions-item label="任务 ID">
            {{ viewingRun.task_id || '即时扫描' }}
          </el-descriptions-item>
          <el-descriptions-item label="状态">{{ formatRunStatus(viewingRun.status) }}</el-descriptions-item>
          <el-descriptions-item label="触发方式">{{ formatTriggerType(viewingRun.trigger_type) }}</el-descriptions-item>
          <el-descriptions-item label="开始时间" :span="2">
            {{ viewingRun.started_at ? formatDateTime(viewingRun.started_at) : '-' }}
          </el-descriptions-item>
          <el-descriptions-item label="完成时间" :span="2">
            {{ viewingRun.completed_at ? formatDateTime(viewingRun.completed_at) : '-' }}
          </el-descriptions-item>
        </el-descriptions>

        <el-divider />
        <div class="run-dialog-section">
          <h4>进度信息</h4>
          <p>{{ viewingRun.progress_message || '暂无进度描述' }}</p>
        </div>

        <div v-if="viewingRun.error_message" class="run-dialog-section">
          <h4>错误信息</h4>
          <pre class="json-block">{{ viewingRun.error_message }}</pre>
        </div>

        <div class="run-dialog-section">
          <h4>结果摘要</h4>
          <div v-if="viewingRun.result_summary && Object.keys(viewingRun.result_summary).length">
            <pre class="json-block">{{ formatJSON(viewingRun.result_summary) }}</pre>
          </div>
          <el-empty v-else description="暂无结果数据" />
        </div>
      </div>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, computed, watch, onMounted, onBeforeUnmount } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { managerAPI } from '../api/manager'
import { ScheduleConfig, describeCron } from '@common-ui'

const dataSources = ref([])
const selectedDataSourceId = ref(null)
const selectedDataSource = computed(() => {
  return dataSources.value.find(ds => ds.id === selectedDataSourceId.value)
})

const activeTab = ref('metadata')

const scanning = ref(false)
const scanResult = ref(null)

const tableFilter = ref('all')
const tables = ref([])
const loadingTables = ref(false)
const managingTableId = ref(null)

const metadataDialogVisible = ref(false)
const selectedTable = ref(null)

const scanTasks = ref([])
const scanRuns = ref([])
const loadingTasks = ref(false)
const loadingRuns = ref(false)
const manualScanning = ref(false)
const togglingTaskId = ref(null)
const triggeringTaskId = ref(null)
const deletingTaskId = ref(null)
const savingTask = ref(false)

const taskDialogVisible = ref(false)
const taskFormRef = ref(null)
const editingTaskId = ref(null)

const taskForm = reactive({
  name: '',
  description: '',
  schedule: '',
  enabled: true,
  schemaInput: '',
  objectPathInput: '',
  scanDepth: 'deep'
})

const taskFormRules = {
  name: [{ required: true, message: '请输入任务名称', trigger: 'blur' }]
}

const taskDialogTitle = computed(() => (editingTaskId.value ? '编辑扫描任务' : '新建扫描任务'))

const runDetailVisible = ref(false)
const viewingRun = ref(null)

const taskMap = computed(() => {
  const map = {}
  for (const task of scanTasks.value) {
    map[task.id] = task
  }
  return map
})

const runDisplayName = (run) => {
  if (!run) return ''
  if (run.name && run.name.trim()) {
    return run.name
  }
  if (run.task_id) {
    return taskMap.value[run.task_id]?.name || `任务 #${run.task_id}`
  }
  return '即时扫描'
}

const scanDepthOptions = [
  { label: '基础 (basic)', value: 'basic' },
  { label: '深入 (deep)', value: 'deep' },
  { label: '全量 (full)', value: 'full' }
]

let runPollingTimer = null

const loadDataSources = async () => {
  try {
    const response = await managerAPI.getDataSources(1, 100)
    dataSources.value = response.data.data || []

    if (dataSources.value.length === 1) {
      selectedDataSourceId.value = dataSources.value[0].id
      handleDataSourceChange()
    }
  } catch (error) {
    console.error('加载数据源失败:', error)
    ElMessage.error('加载数据源失败: ' + (error.response?.data?.error || error.message))
  }
}

const handleDataSourceChange = () => {
  scanResult.value = null
  tables.value = []
  tableFilter.value = 'all'
  scanTasks.value = []
  scanRuns.value = []
  stopRunPolling()

  if (selectedDataSourceId.value) {
    loadTables()
    if (activeTab.value === 'tasks') {
      loadScanTasks()
      loadScanRuns()
      startRunPolling()
    }
  }
}

const handleScanMetadata = async () => {
  if (!selectedDataSourceId.value) return

  scanning.value = true
  try {
    const response = await managerAPI.scanDataSource(selectedDataSourceId.value)
    scanResult.value = response.data

    ElMessage.success(`扫描完成! 共发现 ${response.data.total_items} 个表`)

    loadTables()
    if (activeTab.value === 'tasks') {
      loadScanRuns()
    }
  } catch (error) {
    console.error('扫描元数据失败:', error)
    ElMessage.error('扫描失败: ' + (error.response?.data?.error || error.message))
  } finally {
    scanning.value = false
  }
}

const loadTables = async () => {
  if (!selectedDataSourceId.value) return

  loadingTables.value = true
  try {
    const isManaged = tableFilter.value === 'all' ? null : tableFilter.value === 'managed'
    const response = await managerAPI.getTables(selectedDataSourceId.value, isManaged)
    tables.value = response.data.data || []

    if (!scanResult.value) {
      const allTables = await managerAPI.getTables(selectedDataSourceId.value, null)
      const managedTables = await managerAPI.getTables(selectedDataSourceId.value, true)
      scanResult.value = {
        total_items: allTables.data.total,
        managed_items: managedTables.data.total,
        unmanaged_items: allTables.data.total - managedTables.data.total
      }
    }
  } catch (error) {
    console.error('加载表列表失败:', error)
    ElMessage.error('加载失败: ' + (error.response?.data?.error || error.message))
  } finally {
    loadingTables.value = false
  }
}

const handleManageTable = async (table) => {
  managingTableId.value = table.id
  try {
    await managerAPI.manageTable(table.id)
    ElMessage.success('表已纳管，正在提取详细元数据...')
    await loadTables()
  } catch (error) {
    console.error('纳管表失败:', error)
    ElMessage.error('纳管失败: ' + (error.response?.data?.error || error.message))
  } finally {
    managingTableId.value = null
  }
}

const handleUnmanageTable = async (table) => {
  managingTableId.value = table.id
  try {
    await managerAPI.unmanageTable(table.id)
    ElMessage.success('已取消纳管')
    await loadTables()
  } catch (error) {
    console.error('取消纳管失败:', error)
    ElMessage.error('取消纳管失败: ' + (error.response?.data?.error || error.message))
  } finally {
    managingTableId.value = null
  }
}

const handleViewMetadata = (table) => {
  selectedTable.value = table
  metadataDialogVisible.value = true
}

const loadScanTasks = async (silent = false) => {
  if (!selectedDataSourceId.value) return
  if (!silent) {
    loadingTasks.value = true
  }
  try {
    const response = await managerAPI.getScanTasks(selectedDataSourceId.value)
    scanTasks.value = response.data.data || []
  } catch (error) {
    console.error('加载扫描任务失败:', error)
    if (!silent) {
      ElMessage.error('加载扫描任务失败: ' + (error.response?.data?.error || error.message))
    }
  } finally {
    if (!silent) {
      loadingTasks.value = false
    }
  }
}

const loadScanRuns = async (silent = false) => {
  if (!selectedDataSourceId.value) return
  if (!silent) {
    loadingRuns.value = true
  }
  try {
    const response = await managerAPI.getScanRuns(selectedDataSourceId.value, { limit: 50 })
    scanRuns.value = response.data.data || []
  } catch (error) {
    console.error('加载扫描运行失败:', error)
    if (!silent) {
      ElMessage.error('加载运行记录失败: ' + (error.response?.data?.error || error.message))
    }
  } finally {
    if (!silent) {
      loadingRuns.value = false
    }
  }
}

const startRunPolling = () => {
  if (runPollingTimer || !selectedDataSourceId.value) return
  runPollingTimer = window.setInterval(() => {
    if (activeTab.value !== 'tasks' || !selectedDataSourceId.value) {
      stopRunPolling()
      return
    }
    loadScanRuns(true)
  }, 8000)
}

const stopRunPolling = () => {
  if (runPollingTimer) {
    window.clearInterval(runPollingTimer)
    runPollingTimer = null
  }
}

const openTaskDialog = (task = null) => {
  if (task) {
    editingTaskId.value = task.id
    populateTaskForm(task)
  } else {
    editingTaskId.value = null
    resetTaskForm()
  }
  taskDialogVisible.value = true
}

const resetTaskForm = () => {
  Object.assign(taskForm, {
    name: '',
    description: '',
    scheduleType: 'daily',
    scheduleTime: '00:00',
    scheduleValue: [],
    schedule: '',
    enabled: true,
    schemaInput: '',
    objectPathInput: '',
    scanDepth: 'deep'
  })
}

const populateTaskForm = (task) => {
  const params = task.parameters || {}
  taskForm.name = task.name || ''
  taskForm.description = task.description || ''
  taskForm.schedule = task.schedule || ''
  taskForm.enabled = !!task.enabled
  taskForm.schemaInput = Array.isArray(params.schema_names) ? params.schema_names.join(', ') : ''
  taskForm.objectPathInput = Array.isArray(params.object_paths) ? params.object_paths.join(', ') : ''
  taskForm.scanDepth = typeof params.scan_depth === 'string' ? params.scan_depth : 'deep'
}

const handleTaskDialogClose = () => {
  taskDialogVisible.value = false
  editingTaskId.value = null
  resetTaskForm()
  taskFormRef.value?.clearValidate?.()
}

const submitTaskForm = () => {
  taskFormRef.value?.validate(async (valid) => {
    if (!valid) return

    const payload = buildTaskPayloadFromForm()
    savingTask.value = true
    try {
      if (editingTaskId.value) {
        await managerAPI.updateScanTask(selectedDataSourceId.value, editingTaskId.value, payload)
        ElMessage.success('任务已更新')
      } else {
        await managerAPI.createScanTask(selectedDataSourceId.value, payload)
        ElMessage.success('任务已创建')
      }
      handleTaskDialogClose()
      await loadScanTasks(true)
      if (activeTab.value === 'tasks') {
        loadScanRuns()
      }
    } catch (error) {
      console.error('保存扫描任务失败:', error)
      ElMessage.error('保存失败: ' + (error.response?.data?.error || error.message))
    } finally {
      savingTask.value = false
    }
  })
}

const normalizeListInput = (value) => {
  if (!value) return []
  return value
    .split(',')
    .map((item) => item.trim())
    .filter(Boolean)
}

const buildTaskPayloadFromForm = () => {
  const schemaNames = normalizeListInput(taskForm.schemaInput)
  const objectPaths = normalizeListInput(taskForm.objectPathInput)

  return {
    name: taskForm.name,
    description: taskForm.description,
    schema_names: schemaNames,
    object_paths: objectPaths,
    scan_depth: taskForm.scanDepth || 'deep',
    schedule_type: taskForm.schedule ? 'cron' : 'manual',
    schedule: taskForm.schedule || '',
    enabled: taskForm.enabled
  }
}

const buildTaskPayloadFromTask = (task, overrides = {}) => {
  const params = task.parameters || {}

  return {
    name: overrides.name ?? task.name,
    description: overrides.description ?? task.description ?? '',
    schema_names: Array.isArray(params.schema_names) ? params.schema_names : [],
    object_paths: Array.isArray(params.object_paths) ? params.object_paths : [],
    scan_depth: typeof params.scan_depth === 'string' ? params.scan_depth : 'deep',
    schedule_type: overrides.schedule !== undefined
      ? (overrides.schedule ? 'cron' : 'manual')
      : (task.schedule ? 'cron' : 'manual'),
    schedule: overrides.schedule ?? (task.schedule || ''),
    enabled: overrides.enabled ?? task.enabled
  }
}

const handleToggleTask = async (task) => {
  if (!selectedDataSourceId.value) return
  togglingTaskId.value = task.id
  try {
    const payload = buildTaskPayloadFromTask(task, { enabled: !task.enabled })
    await managerAPI.updateScanTask(selectedDataSourceId.value, task.id, payload)
    ElMessage.success(task.enabled ? '任务已停用' : '任务已启用')
    await loadScanTasks(true)
  } catch (error) {
    console.error('更新任务状态失败:', error)
    ElMessage.error('更新任务状态失败: ' + (error.response?.data?.error || error.message))
  } finally {
    togglingTaskId.value = null
  }
}

const handleTriggerTask = async (task) => {
  if (!selectedDataSourceId.value) return
  triggeringTaskId.value = task.id
  try {
    await managerAPI.triggerScanTask(selectedDataSourceId.value, task.id)
    ElMessage.success('任务已触发执行')
    await loadScanRuns()
    startRunPolling()
  } catch (error) {
    console.error('触发任务失败:', error)
    ElMessage.error('触发任务失败: ' + (error.response?.data?.error || error.message))
  } finally {
    triggeringTaskId.value = null
  }
}

const handleDeleteTask = async (task) => {
  if (!selectedDataSourceId.value) return
  try {
    await ElMessageBox.confirm(`确认删除任务 “${task.name}” 吗？`, '删除确认', {
      type: 'warning',
      confirmButtonText: '删除',
      cancelButtonText: '取消'
    })
  } catch (cancel) {
    return
  }

  deletingTaskId.value = task.id
  try {
    await managerAPI.deleteScanTask(selectedDataSourceId.value, task.id)
    ElMessage.success('任务已删除')
    await loadScanTasks(true)
  } catch (error) {
    console.error('删除任务失败:', error)
    ElMessage.error('删除任务失败: ' + (error.response?.data?.error || error.message))
  } finally {
    deletingTaskId.value = null
  }
}

const handleManualScan = async () => {
  if (!selectedDataSourceId.value) return
  manualScanning.value = true
  try {
    await managerAPI.createManualScanRun(selectedDataSourceId.value, { scan_depth: 'deep', scan_type: 'manual' })
    ElMessage.success('已提交即时扫描，稍后更新运行状态')
    await loadScanRuns()
    startRunPolling()
  } catch (error) {
    console.error('提交即时扫描失败:', error)
    ElMessage.error('提交即时扫描失败: ' + (error.response?.data?.error || error.message))
  } finally {
    manualScanning.value = false
  }
}

const formatRunStatus = (status) => {
  switch (status) {
    case 'pending':
      return '待执行'
    case 'running':
      return '执行中'
    case 'success':
      return '已完成'
    case 'failed':
      return '失败'
    case 'canceled':
      return '已取消'
    default:
      return status || '-'
  }
}

const statusTagType = (status) => {
  switch (status) {
    case 'pending':
      return 'info'
    case 'running':
      return 'primary'
    case 'success':
      return 'success'
    case 'failed':
      return 'danger'
    case 'canceled':
      return 'warning'
    default:
      return 'info'
  }
}

const formatTriggerType = (type) => {
  switch (type) {
    case 'manual':
      return '手动'
    case 'scheduled':
      return '定时'
    case 'system':
      return '系统'
    default:
      return type || '-'
  }
}

const clampPercent = (value) => {
  if (!Number.isFinite(value)) return 0
  return Math.min(100, Math.max(0, Math.round(value)))
}

const calculateProgress = (run) => {
  if (Number.isFinite(run.progress_percent) && run.progress_percent > 0) {
    return clampPercent(run.progress_percent)
  }
  if (run.progress_total > 0) {
    return clampPercent((run.progress_current / run.progress_total) * 100)
  }
  return run.status === 'success' ? 100 : 0
}

const progressStatus = (run) => {
  if (run.status === 'failed') return 'exception'
  if (run.status === 'success') return 'success'
  if (run.status === 'running') return 'active'
  return undefined
}

const viewRunDetail = (run) => {
  viewingRun.value = JSON.parse(JSON.stringify(run))
  runDetailVisible.value = true
}

const formatJSON = (value) => {
  try {
    return JSON.stringify(value, null, 2)
  } catch (error) {
    return String(value)
  }
}

const formatBytes = (bytes) => {
  if (!bytes) return '-'
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
  if (bytes === 0) return '0 B'
  const i = Math.floor(Math.log(bytes) / Math.log(1024))
  return Math.round((bytes / Math.pow(1024, i)) * 100) / 100 + ' ' + sizes[i]
}

const formatDateTime = (datetime) => {
  if (!datetime) return '-'
  return new Date(datetime).toLocaleString('zh-CN')
}

const parseSchema = (schemaJson) => {
  if (!schemaJson) return []
  try {
    if (typeof schemaJson === 'string') {
      return JSON.parse(schemaJson)
    }
    return schemaJson
  } catch (e) {
    console.error('解析 schema 失败:', e)
    return []
  }
}

const parseSampleData = (sampleDataJson) => {
  if (!sampleDataJson) return []
  try {
    if (typeof sampleDataJson === 'string') {
      return JSON.parse(sampleDataJson)
    }
    return sampleDataJson
  } catch (e) {
    console.error('解析采样数据失败:', e)
    return []
  }
}

const getSampleDataColumns = (sampleDataJson) => {
  const data = parseSampleData(sampleDataJson)
  if (!data || data.length === 0) return []
  return Object.keys(data[0])
}

const formatCellValue = (value) => {
  if (value === null) return 'NULL'
  if (value === undefined) return '-'
  if (typeof value === 'object') return JSON.stringify(value)
  return String(value)
}

watch(activeTab, (tab) => {
  if (tab === 'tasks') {
    if (selectedDataSourceId.value) {
      loadScanTasks()
      loadScanRuns()
      startRunPolling()
    }
  } else {
    stopRunPolling()
  }
})

watch(selectedDataSourceId, (newVal) => {
  if (activeTab.value === 'tasks') {
    if (newVal) {
      loadScanTasks()
      loadScanRuns()
      startRunPolling()
    } else {
      stopRunPolling()
    }
  }
})

onMounted(() => {
  loadDataSources()
})

onBeforeUnmount(() => {
  stopRunPolling()
})
</script>

<style scoped>
.metadata-container {
  padding: 20px;
}

.card-header {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.card-header h2 {
  margin: 0;
  font-size: 20px;
  color: var(--addp-text-primary);
}

.subtitle {
  margin: 0;
  font-size: 14px;
  color: var(--addp-text-tertiary);
}

.datasource-selector {
  display: flex;
  gap: 16px;
  align-items: center;
  margin-bottom: 16px;
}

.metadata-tabs {
  margin-top: 8px;
}

.table-section,
.minio-section {
  margin-top: 16px;
}

.section-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 16px;
}

.section-header h3 {
  margin: 0;
  font-size: 16px;
  color: var(--addp-text-primary);
}

.filter-group {
  display: flex;
  gap: 8px;
  align-items: center;
}

.empty-state {
  margin-top: 48px;
}

.metadata-detail {
  padding: 20px 0;
}

.sample-data {
  max-height: 400px;
  overflow: auto;
}

.tasks-section {
  display: flex;
  flex-direction: column;
  gap: 16px;
  margin-top: 8px;
}

.tasks-toolbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  flex-wrap: wrap;
  gap: 12px;
}

.tasks-actions {
  display: flex;
  gap: 12px;
  align-items: center;
}

.tasks-refresh {
  display: flex;
  gap: 8px;
  align-items: center;
}

.task-name {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.task-name__title {
  font-weight: 600;
  color: var(--addp-text-primary);
}

.task-name__meta {
  display: flex;
  gap: 8px;
  align-items: center;
}

.task-name__desc {
  font-size: 12px;
  color: var(--addp-text-tertiary);
  line-height: 1.4;
}

.runs-header {
  display: flex;
  align-items: center;
  gap: 12px;
}

.run-task {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 4px;
}

.run-task__name {
  font-weight: 600;
  color: var(--addp-text-primary);
}

.run-task__meta {
  display: flex;
  align-items: center;
  gap: 6px;
  color: var(--addp-text-tertiary);
  font-size: 12px;
}

.run-trigger-tag {
  margin-left: 0;
}

.run-dialog-section {
  margin-top: 12px;
}

.json-block {
  background: var(--addp-bg-secondary);
  color: var(--addp-text-secondary);
  border-radius: 6px;
  padding: 12px;
  font-size: 12px;
  line-height: 1.6;
  white-space: pre-wrap;
  overflow: auto;
}
</style>
