<template>
  <div class="metadata-scan">
    <el-card>
      <div class="scan-container" ref="containerRef">
        <!-- 左侧：存储引擎列表 -->
        <div class="left-panel" :style="{ width: leftPanelWidth + 'px' }">
          <div class="panel-header">
            <h3>存储引擎列表</h3>
            <el-button
              type="primary"
              @click="handleAutoScan"
              :loading="autoScanning"
              class="auto-scan-button"
            >
              <el-icon><Search /></el-icon>
              一键扫描未扫描引擎
            </el-button>
          </div>
          <el-table
            ref="resourceTableRef"
            :data="engines"
            v-loading="loadingResources"
            highlight-current-row
            @row-click="handleSelectResource"
            height="600"
          >
            <el-table-column type="index" label="#" width="50" />
            <el-table-column prop="name" label="名称" width="150" />
            <el-table-column prop="resource_type" label="类型" width="100">
              <template #default="{ row }">
                <el-tag>{{ row.resource_type }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column label="连接" width="60" align="center">
              <template #default="{ row }">
                <el-tooltip
                  :content="getConnectionTooltip(row)"
                  placement="top"
                  :disabled="!row.connection_status"
                >
                  <el-icon :size="18" :color="getConnectionIconColor(row.connection_status)">
                    <component :is="getConnectionIcon(row.connection_status)" />
                  </el-icon>
                </el-tooltip>
              </template>
            </el-table-column>
            <el-table-column label="Schema统计" width="120">
              <template #default="{ row }">
                <div>总数: {{ row.total_schemas || 0 }}</div>
                <div style="color: #67C23A">已扫: {{ row.scanned_schemas || 0 }}</div>
                <div style="color: #E6A23C">未扫: {{ row.unscanned_schemas || 0 }}</div>
              </template>
            </el-table-column>
            <el-table-column prop="last_scan_at" label="上次扫描" width="150" />
            <el-table-column label="定时计划" min-width="220">
              <template #default="{ row }">
                <div v-if="resourcePlanMap[row.id]" class="plan-summary">
                  <div class="plan-summary__status">
                    <el-tag :type="resourcePlanMap[row.id].enabled ? 'success' : 'info'" size="small">
                      {{ resourcePlanMap[row.id].enabled ? '已启用' : '未启用' }}
                    </el-tag>
                    <span class="plan-summary__text">{{ resourcePlanMap[row.id].description }}</span>
                  </div>
                  <div class="plan-summary__next" v-if="resourcePlanMap[row.id].nextRun">
                    下次执行：{{ resourcePlanMap[row.id].nextRun }}
                  </div>
                </div>
                <div v-else class="plan-summary plan-summary--empty">未配置</div>
              </template>
            </el-table-column>
            <el-table-column label="操作" width="140">
              <template #default="{ row }">
                <el-button type="success" plain size="small" @click.stop="handleScheduleClick(row)">
                  定时设置
                </el-button>
              </template>
            </el-table-column>
          </el-table>
        </div>

        <div
          class="panel-resizer"
          @mousedown.prevent="startResizing"
          title="拖拽调整左右区域宽度"
        />

        <!-- 右侧：Schema列表 -->
        <div class="right-panel">
          <div class="panel-header">
            <h3>Schema列表 {{ selectedResource ? `- ${selectedResource.name}` : '' }}</h3>
            <div v-if="selectedResource" class="schema-actions">
              <el-button @click="loadSchemas" :loading="loadingSchemas">
                <el-icon><Refresh /></el-icon> 刷新
              </el-button>
              <el-button
                type="primary"
                @click="handleBatchScan"
                :disabled="!selectedSchemas.length"
                :loading="scanning"
              >
                <el-icon><Search /></el-icon>
                批量扫描选中Schema ({{ selectedSchemas.length }})
              </el-button>
            </div>
          </div>

          <div v-if="!selectedResource" class="empty-state">
            <el-empty description="请从左侧选择一个存储引擎" />
          </div>

          <div v-else class="schema-table-wrapper">
            <el-table
              class="schema-table"
              :data="schemas"
              v-loading="loadingSchemas"
              height="600"
              @selection-change="handleSchemaSelectionChange"
              style="min-width: 720px"
            >
              <el-table-column type="selection" width="55" />
              <el-table-column prop="name" label="Schema名称" width="200" />
              <el-table-column label="扫描状态" width="120">
                <template #default="{ row }">
                  <el-tag
                    :type="row.scan_status === '已扫描' ? 'success' : row.scan_status === '扫描中' ? 'warning' : 'info'"
                  >
                    {{ row.scan_status }}
                  </el-tag>
                </template>
              </el-table-column>
              <el-table-column prop="table_count" label="表数量" width="100" />
              <el-table-column prop="last_scan_at" label="上次扫描" width="150" />
              <el-table-column label="操作" width="150">
                <template #default="{ row }">
                  <el-button
                    size="small"
                    @click.stop="handleScanSchema(row)"
                    :loading="scanningSchemas[row.id ?? (row.schema_name || row.name)]"
                  >
                    {{ row.scan_status === '已扫描' ? '重新扫描' : '扫描' }}
                  </el-button>
                </template>
              </el-table-column>
            </el-table>
          </div>
        </div>
      </div>
    </el-card>

    <!-- 扫描进度对话框 -->
    <el-dialog v-model="showScanDialog" title="扫描进度" width="500px" :close-on-click-modal="false">
      <div v-if="scanning">
        <el-progress :percentage="scanProgress" :status="scanProgress === 100 ? 'success' : undefined" />
        <p style="margin-top: 20px; text-align: center; color: #999">{{ scanMessage }}</p>
      </div>
      <div v-else-if="scanResult">
        <el-result
          :icon="scanResult.status === 'success' ? 'success' : 'error'"
          :title="scanResult.status === 'success' ? '扫描完成' : '扫描失败'"
        >
          <template #sub-title>
            <div>扫描了 {{ scanResult.schemas_scanned }} 个Schema</div>
            <div>发现 {{ scanResult.tables_scanned }} 个表</div>
            <div>扫描 {{ scanResult.fields_scanned }} 个字段</div>
            <div>耗时: {{ scanResult.duration_ms }}ms</div>
          </template>
        </el-result>
      </div>
      <template #footer>
        <el-button @click="closeScanDialog">关闭</el-button>
      </template>
    </el-dialog>

    <el-dialog
      v-model="scheduleDialogVisible"
      title="定时扫描设置"
      width="600px"
      @close="resetScheduleForm"
    >
      <ScheduleConfig v-model="scheduleCron" />

      <el-form label-width="100px" style="margin-top: 20px">
        <el-form-item label="是否启用">
          <el-switch v-model="scheduleEnabled" />
        </el-form-item>
        <div class="schedule-hint">
          提交后会为当前存储引擎创建（或更新）一个定时扫描任务，按设定频率自动执行。
        </div>
      </el-form>
      <template #footer>
        <el-button @click="scheduleDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="submitScheduleForm" :loading="savingSchedule">
          保存
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, reactive, watch, nextTick, onBeforeUnmount } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Search, Refresh, CircleCheck, CircleClose, Warning, QuestionFilled } from '@element-plus/icons-vue'
import { ScheduleConfig, describeCron, decodeScheduleToForm } from '@common-ui'
import metaApi from '../api/meta'

const AUTO_SCHEDULE_DESC_MARK = '[PortalAutoSchedule]'

// 引擎列表
const engines = ref([])
const resourceTableRef = ref(null)
const loadingResources = ref(false)
const selectedResource = ref(null)
const containerRef = ref(null)

// Schema列表
const schemas = ref([])
const loadingSchemas = ref(false)
const selectedSchemas = ref([])

// 扫描状态
const autoScanning = ref(false)
const scanning = ref(false)
const scanningSchemas = reactive({})
const showScanDialog = ref(false)
const scanProgress = ref(0)
const scanMessage = ref('')
const scanResult = ref(null)

const allScanTasks = ref([])
const scheduleDialogVisible = ref(false)
const savingSchedule = ref(false)
const scheduleCron = ref('') // Cron 表达式
const scheduleEnabled = ref(true) // 是否启用

const leftPanelWidth = ref(560)
const isResizing = ref(false)
const minLeftPanelWidth = 440
const minRightPanelWidth = 240
let resizeStartX = 0
let resizeStartWidth = leftPanelWidth.value

const resourcePlanMap = computed(() => {
  const map = {}
  for (const task of allScanTasks.value) {
    if (!task || typeof task.engine_id !== 'number') continue
    const desc = typeof task.description === 'string' ? task.description : ''
    if (!desc.includes(AUTO_SCHEDULE_DESC_MARK)) continue
    map[task.engine_id] = {
      enabled: !!task.enabled,
      description: formatScheduleDescription(task),
      nextRun: task.next_run_at ? formatDateTime(task.next_run_at) : ''
    }
  }
  return map
})

const autoScheduleTask = computed(() => {
  if (!selectedResource.value) return null
  return (
    allScanTasks.value.find(task => {
      if (!task || task.engine_id !== selectedResource.value.id) return false
      const desc = typeof task.description === 'string' ? task.description : ''
      return desc.includes(AUTO_SCHEDULE_DESC_MARK)
    }) || null
  )
})

// 加载引擎列表
const loadEngines = async () => {
  loadingResources.value = true
  try {
    const res = await metaApi.getResources()
    // createAPIClient 提取了 axios 的 response.data（后端响应体 {"data": [...]}）
    // 需要再提取业务数据的 .data 字段才能得到数组
    engines.value = res.data || []
    if (!selectedResource.value && engines.value.length) {
      selectedResource.value = engines.value[0]
      await nextTick()
      resourceTableRef.value?.setCurrentRow(selectedResource.value)
      await Promise.all([loadSchemas(), loadScanTasks()])
    }
    if (!engines.value.length) {
      selectedResource.value = null
      await nextTick()
      resourceTableRef.value?.setCurrentRow(null)
      allScanTasks.value = []
    } else if (!allScanTasks.value.length) {
      await loadScanTasks()
    }
    enforceBounds()
  } catch (error) {
    ElMessage.error('加载引擎列表失败: ' + (error.response?.data?.error || error.message))
  } finally {
    loadingResources.value = false
  }
}

// 选择引擎
const handleSelectResource = async (row) => {
  selectedResource.value = row
  await nextTick()
  resourceTableRef.value?.setCurrentRow(row)
  await loadSchemas()
  enforceBounds()
}

const handleScheduleClick = async row => {
  if (!row) return
  if (!selectedResource.value || selectedResource.value.id !== row.id) {
    await handleSelectResource(row)
  }
  await loadScanTasks()
  prefillScheduleForm(autoScheduleTask.value)
  scheduleDialogVisible.value = true
}

const computeBounds = () => {
  const containerWidth = containerRef.value?.getBoundingClientRect().width || window.innerWidth
  const maxWidth = Math.max(minLeftPanelWidth, containerWidth - minRightPanelWidth)
  return {
    min: minLeftPanelWidth,
    max: maxWidth
  }
}

const enforceBounds = () => {
  const { min, max } = computeBounds()
  leftPanelWidth.value = Math.min(Math.max(leftPanelWidth.value, min), max)
}

const startResizing = event => {
  if (!event) return
  isResizing.value = true
  resizeStartX = event.clientX
  resizeStartWidth = leftPanelWidth.value
  document.body.style.userSelect = 'none'
  window.addEventListener('mousemove', onResizing)
  window.addEventListener('mouseup', stopResizing)
}

const onResizing = event => {
  if (!isResizing.value) return
  const delta = event.clientX - resizeStartX
  const desired = resizeStartWidth + delta
  const { min, max } = computeBounds()
  leftPanelWidth.value = Math.min(Math.max(desired, min), max)
}

const stopResizing = () => {
  if (!isResizing.value) return
  isResizing.value = false
  document.body.style.userSelect = ''
  window.removeEventListener('mousemove', onResizing)
  window.removeEventListener('mouseup', stopResizing)
  enforceBounds()
}

// 加载Schema列表
const loadSchemas = async () => {
  if (!selectedResource.value) return

  loadingSchemas.value = true
  let availableSchemas = []
  let connectionError = null

  try {
    // 检查引擎连接状态，如果已知离线，直接跳过实际连接
    if (selectedResource.value.connection_status === 'offline') {
      connectionError = new Error(`引擎离线: ${selectedResource.value.check_message || '连接失败'}`)
      console.warn('资源已标记为离线，跳过实际连接:', selectedResource.value.name)
    } else {
      // 引擎在线或状态未知，尝试获取实际Schema列表
      try {
        const availableRes = await metaApi.listAvailableSchemas(selectedResource.value.id)
        availableSchemas = availableRes.data || []
      } catch (error) {
        // 捕获连接错误，但不阻止后续加载
        connectionError = error
        console.warn('获取可用Schema失败（可能存储引擎离线）:', error.response?.data?.error || error.message)
      }
    }
  } catch (error) {
    // 不应该到这里，但保险起见
    connectionError = error
  }

  try {
    // 再获取已扫描的schema状态信息
    const scannedRes = await metaApi.getSchemas(selectedResource.value.id)
    const scannedSchemas = scannedRes.data || []

    if (connectionError && scannedSchemas.length === 0) {
      // 如果连接失败且没有已扫描的schema，显示空列表
      // 用户已经能从左侧图标看到引擎离线状态，无需重复提示
      schemas.value = []
    } else if (connectionError) {
      // 连接失败但有历史扫描数据，使用历史数据并标记状态
      schemas.value = scannedSchemas.map(scanned => ({
        id: scanned.id,
        name: scanned.schema_name,
        schema_name: scanned.schema_name,
        scan_status: '连接失败 - ' + scanned.scan_status,
        table_count: scanned.table_count || 0,
        last_scan_at: scanned.last_scan_at || '',
        total_size_bytes: scanned.total_size_bytes || 0
      }))
      // 已通过左侧连接状态图标显示，无需额外提示
    } else {
      // 正常情况：合并两个列表
      schemas.value = availableSchemas.map(available => {
        const scanned = scannedSchemas.find(s => s.schema_name === available.name)
        return {
          ...available,
          id: scanned?.id,
          schema_name: available.name,  // 保持兼容
          scan_status: scanned?.scan_status || '未扫描',
          table_count: scanned?.table_count || 0,
          last_scan_at: scanned?.last_scan_at || '',
          total_size_bytes: scanned?.total_size_bytes || 0
        }
      })
    }
  } catch (error) {
    ElMessage.error('加载Schema列表失败: ' + (error.response?.data?.error || error.message))
    schemas.value = []
  } finally {
    loadingSchemas.value = false
  }
}

// Schema选择变化
const handleSchemaSelectionChange = (selection) => {
  selectedSchemas.value = selection
}

const loadScanTasks = async () => {
  try {
    allScanTasks.value = await metaApi.getScanTasks()
  } catch (error) {
    ElMessage.error('加载扫描任务失败: ' + (error.response?.data?.error || error.message))
  }
}

// 连接状态辅助函数
const getConnectionIcon = (status) => {
  const iconMap = {
    'online': CircleCheck,
    'offline': CircleClose,
    'unknown': QuestionFilled,
    'checking': Warning
  }
  return iconMap[status] || QuestionFilled
}

const getConnectionIconColor = (status) => {
  const colorMap = {
    'online': '#67C23A',
    'offline': '#F56C6C',
    'unknown': '#909399',
    'checking': '#E6A23C'
  }
  return colorMap[status] || '#909399'
}

const getConnectionStatusLabel = (status) => {
  const labelMap = {
    'online': '在线',
    'offline': '离线',
    'unknown': '未知',
    'checking': '检测中'
  }
  return labelMap[status] || '未检测'
}

const getConnectionTooltip = (row) => {
  if (!row.connection_status) return '未检测'

  let tooltip = `状态: ${getConnectionStatusLabel(row.connection_status)}`

  if (row.last_check_at) {
    tooltip += `\n检测时间: ${row.last_check_at}`
  }

  if (row.check_message) {
    tooltip += `\n详情: ${row.check_message}`
  }

  return tooltip
}

const resetScheduleForm = () => {
  scheduleCron.value = ''
  scheduleEnabled.value = true
}

const deriveAutoTaskSchemas = () => {
  if (!Array.isArray(schemas.value) || !schemas.value.length) return []
  return schemas.value
    .map(item => item.schema_name || item.name)
    .filter(Boolean)
}

const getAutoScheduleTaskName = () => {
  if (selectedResource.value?.name) {
    return `${selectedResource.value.name} 定时扫描`
  }
  return '定时扫描任务'
}

const ensureAutoScheduleDescription = desc => {
  const text = typeof desc === 'string' ? desc : ''
  if (text.includes(AUTO_SCHEDULE_DESC_MARK)) {
    return text
  }
  const suffix = text.trim().length ? ` ${text.trim()}` : ' 由模块自动创建'
  return `${AUTO_SCHEDULE_DESC_MARK}${suffix}`
}

const prefillScheduleForm = task => {
  if (!task) {
    resetScheduleForm()
    return
  }
  scheduleCron.value = task.schedule || ''
  scheduleEnabled.value = !!task.enabled
}

// 一键自动扫描
const handleAutoScan = async () => {
  try {
    await ElMessageBox.confirm(
      '将自动扫描所有未扫描的引擎，这可能需要一些时间。是否继续？',
      '确认自动扫描',
      { type: 'warning' }
    )

    autoScanning.value = true
    showScanDialog.value = true
    scanProgress.value = 0
    scanMessage.value = '正在扫描...'
    scanResult.value = null

    // 模拟进度
    const progressInterval = setInterval(() => {
      if (scanProgress.value < 90) {
        scanProgress.value += 10
      }
    }, 500)

    const res = await metaApi.autoScan()
    clearInterval(progressInterval)
    scanProgress.value = 100

    scanResult.value = res.data
    ElMessage.success('自动扫描完成')

    // 刷新引擎列表
    await loadEngines()
    if (selectedResource.value) {
      await loadSchemas()
    }
  } catch (error) {
    if (error !== 'cancel') {
      ElMessage.error('自动扫描失败: ' + (error.response?.data?.error || error.message))
    }
  } finally {
    autoScanning.value = false
  }
}

// 批量扫描Schema
const handleBatchScan = async () => {
  if (!selectedSchemas.value.length) return

  try {
    await ElMessageBox.confirm(
      `将扫描 ${selectedSchemas.value.length} 个Schema，是否继续？`,
      '确认批量扫描',
      { type: 'warning' }
    )

    scanning.value = true
    showScanDialog.value = true
    scanProgress.value = 0
    scanMessage.value = '正在扫描...'
    scanResult.value = null

    const schemaNames = selectedSchemas.value.map(item => item.schema_name || item.name)

    // 模拟进度
    const progressInterval = setInterval(() => {
      if (scanProgress.value < 90) {
        scanProgress.value += 10
      }
    }, 500)

    const res = await metaApi.scanEngine(selectedResource.value.id, schemaNames)
    clearInterval(progressInterval)
    scanProgress.value = 100

    scanResult.value = res.data
    ElMessage.success('批量扫描完成')

    // 刷新Schema列表
    await loadSchemas()
    await loadEngines()
  } catch (error) {
    if (error !== 'cancel') {
      ElMessage.error('批量扫描失败: ' + (error.response?.data?.error || error.message))
    }
  } finally {
    scanning.value = false
  }
}

const submitScheduleForm = async () => {
  if (!selectedResource.value) {
    ElMessage.warning('请先选择存储引擎')
    return
  }

  if (!scheduleCron.value) {
    ElMessage.error('请配置调度计划')
    return
  }

  savingSchedule.value = true
  try {
    const existing = autoScheduleTask.value

    // 统一使用 cron 类型，直接传递 Cron 表达式
    const payload = {
      name: existing?.name || getAutoScheduleTaskName(),
      description: ensureAutoScheduleDescription(existing?.description || ''),
      schema_names: existing?.parameters?.schema_names || deriveAutoTaskSchemas(),
      object_paths: existing?.parameters?.object_paths || [],
      scan_depth: existing?.parameters?.scan_depth || 'deep',
      schedule_type: 'cron',  // 统一使用 cron 类型
      schedule_time: '',
      schedule_value: [],
      schedule: scheduleCron.value,  // 直接使用 ScheduleConfig 生成的 Cron 表达式
      enabled: scheduleEnabled.value
    }

    if (existing) {
      await metaApi.updateScanTask(selectedResource.value.id, existing.id, payload)
    } else {
      await metaApi.createScanTask(selectedResource.value.id, payload)
    }

    ElMessage.success('定时扫描设置已保存')
    scheduleDialogVisible.value = false
    await loadScanTasks()
  } catch (error) {
    ElMessage.error('保存失败: ' + (error.response?.data?.error || error.message))
  } finally {
    savingSchedule.value = false
  }
}

// 扫描单个Schema
const handleScanSchema = async (schema) => {
  const schemaName = schema.schema_name || schema.name
  const key = schema.id ?? schemaName
  scanningSchemas[key] = true

  try {
    await metaApi.scanEngine(selectedResource.value.id, [schemaName])
    ElMessage.success(`Schema "${schemaName}" 扫描完成`)

    // 刷新列表
    await loadSchemas()
    await loadEngines()
  } catch (error) {
    ElMessage.error('扫描失败: ' + (error.response?.data?.error || error.message))
  } finally {
    scanningSchemas[key] = false
  }
}

// 关闭扫描对话框
const closeScanDialog = () => {
  showScanDialog.value = false
  scanProgress.value = 0
  scanMessage.value = ''
  scanResult.value = null
}

function formatScheduleDescription(task) {
  if (!task.schedule) {
    return '手动触发'
  }
  return describeCron(task.schedule)
}

function formatDateTime(datetime) {
  if (!datetime) return ''
  return new Date(datetime).toLocaleString('zh-CN')
}

watch(selectedResource, () => {
  selectedSchemas.value = []
  scheduleDialogVisible.value = false
  resetScheduleForm()
})

onMounted(() => {
  loadEngines()
  window.addEventListener('resize', enforceBounds)
})

onBeforeUnmount(() => {
  stopResizing()
  window.removeEventListener('resize', enforceBounds)
})
</script>

<style scoped>
.metadata-scan {
  padding: 20px;
}

.scan-container {
  display: flex;
  gap: 16px;
}

.left-panel {
  flex: 0 0 auto;
  padding-right: 12px;
  border-right: 1px solid #f2f3f5;
  box-sizing: border-box;
}

.plan-summary {
  display: flex;
  flex-direction: column;
  gap: 4px;
  font-size: 12px;
  color: #606266;
}

.plan-summary__status {
  display: flex;
  align-items: center;
  gap: 6px;
}

.plan-summary__text {
  color: #606266;
}

.plan-summary__next {
  color: #909399;
}

.plan-summary--empty {
  color: #909399;
  font-style: italic;
}

.panel-resizer {
  flex: 0 0 6px;
  cursor: col-resize;
  background: linear-gradient(180deg, #dcdfe6 0%, #c0c4cc 100%);
  border-radius: 3px;
  align-self: stretch;
  margin: 0 4px;
}

.panel-resizer:hover {
  background: linear-gradient(180deg, #c0c4cc 0%, #909399 100%);
}

.right-panel {
  flex: 1;
}

.panel-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  flex-wrap: wrap;
  gap: 12px;
  margin-bottom: 15px;
}

.panel-header h3 {
  margin: 0;
  font-size: 16px;
}

.auto-scan-button {
  white-space: nowrap;
}

.schema-actions {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}

.schedule-hint {
  margin-left: 100px;
  margin-top: -8px;
  font-size: 12px;
  color: #909399;
  line-height: 1.5;
}

.empty-state {
  display: flex;
  align-items: center;
  justify-content: center;
  height: 600px;
}

.schema-table-wrapper {
  width: 100%;
  overflow-x: auto;
}
</style>
