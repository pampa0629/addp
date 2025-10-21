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
              一键扫描未扫描资源
            </el-button>
          </div>
          <el-table
            ref="resourceTableRef"
            :data="resources"
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
      width="460px"
      @close="resetScheduleForm"
    >
      <el-form :model="scheduleForm" label-width="100px">
        <el-form-item label="执行频率">
          <el-radio-group v-model="scheduleForm.scheduleType">
            <el-radio-button label="daily">每天</el-radio-button>
            <el-radio-button label="weekly">每周</el-radio-button>
            <el-radio-button label="monthly">每月</el-radio-button>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="执行时间">
          <el-time-picker
            v-model="scheduleForm.scheduleTime"
            format="HH:mm"
            value-format="HH:mm"
            placeholder="选择时间"
          />
        </el-form-item>
        <el-form-item label="执行星期" v-if="scheduleForm.scheduleType === 'weekly'">
          <el-select
            v-model="scheduleForm.weekdays"
            multiple
            collapse-tags
            collapse-tags-tooltip
            placeholder="选择星期"
          >
            <el-option
              v-for="item in weekdayOptions"
              :key="item.value"
              :label="item.label"
              :value="item.value"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="执行日期" v-if="scheduleForm.scheduleType === 'monthly'">
          <el-select
            v-model="scheduleForm.monthDays"
            multiple
            collapse-tags
            collapse-tags-tooltip
            placeholder="选择日期"
          >
            <el-option
              v-for="day in monthDayOptions"
              :key="day"
              :label="`${day}日`"
              :value="day"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="是否启用">
          <el-switch v-model="scheduleForm.enabled" />
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
import { Search, Refresh } from '@element-plus/icons-vue'
import metaApi from '../api/meta'

const AUTO_SCHEDULE_DESC_MARK = '[PortalAutoSchedule]'

// 资源列表
const resources = ref([])
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
const scheduleForm = reactive({
  scheduleType: 'daily',
  scheduleTime: '02:00',
  weekdays: [1],
  monthDays: [1],
  enabled: true
})

const leftPanelWidth = ref(560)
const isResizing = ref(false)
const minLeftPanelWidth = 440
const minRightPanelWidth = 240
let resizeStartX = 0
let resizeStartWidth = leftPanelWidth.value

const weekdayOptions = [
  { label: '周日', value: 0 },
  { label: '周一', value: 1 },
  { label: '周二', value: 2 },
  { label: '周三', value: 3 },
  { label: '周四', value: 4 },
  { label: '周五', value: 5 },
  { label: '周六', value: 6 }
]

const monthDayOptions = Array.from({ length: 31 }, (_, index) => index + 1)

const resourcePlanMap = computed(() => {
  const map = {}
  for (const task of allScanTasks.value) {
    if (!task || typeof task.resource_id !== 'number') continue
    const desc = typeof task.description === 'string' ? task.description : ''
    if (!desc.includes(AUTO_SCHEDULE_DESC_MARK)) continue
    map[task.resource_id] = {
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
      if (!task || task.resource_id !== selectedResource.value.id) return false
      const desc = typeof task.description === 'string' ? task.description : ''
      return desc.includes(AUTO_SCHEDULE_DESC_MARK)
    }) || null
  )
})

// 加载资源列表
const loadResources = async () => {
  loadingResources.value = true
  try {
    const res = await metaApi.getResources()
    // client.js 的响应拦截器已经返回 response.data，所以这里直接是 res.data
    resources.value = res.data || []
    if (!selectedResource.value && resources.value.length) {
      selectedResource.value = resources.value[0]
      await nextTick()
      resourceTableRef.value?.setCurrentRow(selectedResource.value)
      await Promise.all([loadSchemas(), loadScanTasks()])
    }
    if (!resources.value.length) {
      selectedResource.value = null
      await nextTick()
      resourceTableRef.value?.setCurrentRow(null)
      allScanTasks.value = []
    } else if (!allScanTasks.value.length) {
      await loadScanTasks()
    }
    enforceBounds()
  } catch (error) {
    ElMessage.error('加载资源列表失败: ' + (error.response?.data?.error || error.message))
  } finally {
    loadingResources.value = false
  }
}

// 选择资源
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
  try {
    // 先获取数据库中实际存在的schemas
    const availableRes = await metaApi.listAvailableSchemas(selectedResource.value.id)
    const availableSchemas = availableRes.data || []

    // 再获取已扫描的schema状态信息
    const scannedRes = await metaApi.getSchemas(selectedResource.value.id)
    const scannedSchemas = scannedRes.data || []

    // 合并两个列表：available schemas作为基础，补充扫描状态信息
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
  } catch (error) {
    ElMessage.error('加载Schema列表失败: ' + (error.response?.data?.error || error.message))
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

const resetScheduleForm = () => {
  scheduleForm.scheduleType = 'daily'
  scheduleForm.scheduleTime = '02:00'
  scheduleForm.weekdays = [1]
  scheduleForm.monthDays = [1]
  scheduleForm.enabled = true
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
  const type = ['daily', 'weekly', 'monthly'].includes(task.schedule_type) ? task.schedule_type : 'daily'
  const config = task.schedule_config || {}
  const values = normalizeScheduleValue(config.value)

  scheduleForm.scheduleType = type
  scheduleForm.scheduleTime = config.time || '02:00'
  scheduleForm.enabled = !!task.enabled

  if (type === 'weekly') {
    scheduleForm.weekdays = values.length ? values : [1]
    scheduleForm.monthDays = [1]
  } else if (type === 'monthly') {
    scheduleForm.monthDays = values.length ? values : [1]
    scheduleForm.weekdays = [1]
  } else {
    scheduleForm.weekdays = [1]
    scheduleForm.monthDays = [1]
  }
}

// 一键自动扫描
const handleAutoScan = async () => {
  try {
    await ElMessageBox.confirm(
      '将自动扫描所有未扫描的资源，这可能需要一些时间。是否继续？',
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

    // 刷新资源列表
    await loadResources()
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

    const res = await metaApi.scanResource(selectedResource.value.id, schemaNames)
    clearInterval(progressInterval)
    scanProgress.value = 100

    scanResult.value = res.data
    ElMessage.success('批量扫描完成')

    // 刷新Schema列表
    await loadSchemas()
    await loadResources()
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

  const type = scheduleForm.scheduleType
  if (!scheduleForm.scheduleTime) {
    ElMessage.error('请选择执行时间')
    return
  }
  if (type === 'weekly' && (!scheduleForm.weekdays || !scheduleForm.weekdays.length)) {
    ElMessage.error('请至少选择一个执行星期')
    return
  }
  if (type === 'monthly' && (!scheduleForm.monthDays || !scheduleForm.monthDays.length)) {
    ElMessage.error('请至少选择一个执行日期')
    return
  }

  const scheduleValue =
    type === 'weekly'
      ? normalizeScheduleValue(scheduleForm.weekdays)
      : type === 'monthly'
        ? normalizeScheduleValue(scheduleForm.monthDays)
        : []

  const overrides = {
    schedule_type: type,
    schedule_time: scheduleForm.scheduleTime,
    schedule_value: scheduleValue,
    cron_expression: '',
    enabled: scheduleForm.enabled
  }

  savingSchedule.value = true
  try {
    const existing = autoScheduleTask.value
    if (existing) {
      const payload = buildTaskPayloadFromTask(existing, overrides)
      payload.name = existing.name || getAutoScheduleTaskName()
      payload.description = ensureAutoScheduleDescription(existing.description)
      if (!payload.schema_names || !payload.schema_names.length) {
        payload.schema_names = deriveAutoTaskSchemas()
      }
      if (!Array.isArray(payload.object_paths)) {
        payload.object_paths = []
      }
      if (!payload.scan_depth) {
        payload.scan_depth = 'deep'
      }
      await metaApi.updateScanTask(selectedResource.value.id, existing.id, payload)
    } else {
      const payload = {
        name: getAutoScheduleTaskName(),
        description: ensureAutoScheduleDescription(''),
        schema_names: deriveAutoTaskSchemas(),
        object_paths: [],
        scan_depth: 'deep',
        schedule_type: overrides.schedule_type,
        schedule_time: overrides.schedule_time,
        schedule_value: overrides.schedule_value,
        cron_expression: '',
        enabled: overrides.enabled
      }
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
    await metaApi.scanResource(selectedResource.value.id, [schemaName])
    ElMessage.success(`Schema "${schemaName}" 扫描完成`)

    // 刷新列表
    await loadSchemas()
    await loadResources()
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

function normalizeScheduleValue(value) {
  if (!Array.isArray(value)) return []
  return value
    .map(item => Number(item))
    .filter(item => Number.isFinite(item))
}

const buildTaskPayloadFromTask = (task, overrides = {}) => {
  const params = task.parameters || {}
  const config = task.schedule_config || {}
  return {
    name: overrides.name ?? task.name,
    description: overrides.description ?? task.description ?? '',
    schema_names: Array.isArray(params.schema_names) ? params.schema_names : [],
    object_paths: Array.isArray(params.object_paths) ? params.object_paths : [],
    scan_depth: typeof params.scan_depth === 'string' ? params.scan_depth : 'deep',
    schedule_type: overrides.schedule_type ?? task.schedule_type,
    schedule_time: overrides.schedule_time ?? (config.time || ''),
    schedule_value: overrides.schedule_value ?? normalizeScheduleValue(config.value),
    cron_expression: (overrides.cron_expression ?? task.cron_expression) || '',
    enabled: overrides.enabled ?? task.enabled
  }
}

function formatScheduleDescription(task) {
  const config = task.schedule_config || {}
  const time = config.time || '02:00'
  const values = normalizeScheduleValue(config.value)
  switch (task.schedule_type) {
    case 'daily':
      return `每天 ${time}`
    case 'weekly': {
      const labels = values
        .map(val => weekdayOptions.find(item => item.value === val)?.label)
        .filter(Boolean)
      return labels.length ? `每周 ${labels.join('、')} ${time}` : `每周（未设置星期） ${time}`
    }
    case 'monthly': {
      const labels = values.map(val => `${val}日`)
      return labels.length ? `每月 ${labels.join('、')} ${time}` : `每月（未设置日期） ${time}`
    }
    case 'cron':
      return task.cron_expression ? `Cron: ${task.cron_expression}` : 'Cron 调度（未配置表达式）'
    case 'manual':
    default:
      return '手动触发'
  }
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

watch(
  () => scheduleForm.scheduleType,
  type => {
    if (type === 'weekly' && (!scheduleForm.weekdays || scheduleForm.weekdays.length === 0)) {
      scheduleForm.weekdays = [1]
    } else if (type === 'monthly' && (!scheduleForm.monthDays || scheduleForm.monthDays.length === 0)) {
      scheduleForm.monthDays = [1]
    }
  }
)

onMounted(() => {
  loadResources()
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
