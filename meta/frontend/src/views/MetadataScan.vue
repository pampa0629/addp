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
            :data="filteredEngines"
            v-loading="loadingResources"
            highlight-current-row
            @row-click="handleSelectResource"
            height="600"
          >
            <el-table-column label="引擎信息" min-width="220">
              <template #default="{ row }">
                <div class="engine-info">
                  <!-- 第一行：类型标签 + 名称 -->
                  <div class="engine-name">
                    <el-tag size="small" class="engine-type">{{ row.resource_type }}</el-tag>
                    <span class="name-text">{{ row.name }}</span>
                  </div>

                  <!-- 第二行：连接状态 -->
                  <div class="engine-connection">
                    <el-tooltip :content="getConnectionTooltip(row)" placement="top">
                      <div class="connection-status">
                        <el-icon :size="14" :color="getConnectionIconColor(row.connection_status)">
                          <component :is="getConnectionIcon(row.connection_status)" />
                        </el-icon>
                        <span>{{ getConnectionStatusLabel(row.connection_status) }}</span>
                      </div>
                    </el-tooltip>
                  </div>

                  <!-- 第三行：Schema统计（tooltip显示） -->
                  <el-tooltip placement="top">
                    <template #content>
                      总数: {{ row.total_schemas || 0 }}<br>
                      已扫描: {{ row.scanned_schemas || 0 }}<br>
                      未扫描: {{ row.unscanned_schemas || 0 }}
                    </template>
                    <div class="engine-stats">
                      {{ row.total_schemas || 0 }}个{{ getSchemaTerminology(row.resource_type) }}
                      <span class="stat-scanned" v-if="row.scanned_schemas">({{ row.scanned_schemas }}已扫)</span>
                      <span class="stat-unscanned" v-if="row.unscanned_schemas">/{{ row.unscanned_schemas }}未扫</span>
                    </div>
                  </el-tooltip>
                </div>
              </template>
            </el-table-column>
            <el-table-column label="状态概览" width="180">
              <template #default="{ row }">
                <div class="status-overview">
                  <!-- 调度状态 -->
                  <div class="schedule-status">
                    <el-tooltip
                      v-if="resourcePlanMap[row.id]"
                      :content="`${resourcePlanMap[row.id].description}\n下次执行：${resourcePlanMap[row.id].nextRun}`"
                      placement="top"
                    >
                      <div class="schedule-indicator">
                        <el-icon :color="resourcePlanMap[row.id].enabled ? 'var(--el-color-success)' : 'var(--addp-text-tertiary)'">
                          <Clock />
                        </el-icon>
                        <span>{{ resourcePlanMap[row.id].enabled ? '调度已启用' : '调度未启用' }}</span>
                      </div>
                    </el-tooltip>
                    <div v-else class="schedule-indicator schedule-none">
                      <el-icon color="#C0C4CC"><Clock /></el-icon>
                      <span>未配置调度</span>
                    </div>
                  </div>

                  <!-- 上次扫描 -->
                  <div class="last-scan" v-if="row.scanned_at">
                    <span class="label">上次扫描：</span>
                    <span class="time">{{ formatShortTime(row.scanned_at) }}</span>
                  </div>
                </div>
              </template>
            </el-table-column>
            <el-table-column label="操作" width="140" fixed="right">
              <template #default="{ row }">
                <div class="engine-actions">
                  <el-button
                    type="success"
                    size="default"
                    plain
                    @click.stop="handleScheduleClick(row)"
                  >
                    <el-icon><Clock /></el-icon>
                    调度
                  </el-button>
                </div>
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
            <h3>{{ rightPanelTitle }}</h3>
            <div v-if="selectedResource" class="schema-actions-bar">
              <!-- 选中提示 -->
              <div v-if="selectedSchemas.length" class="selection-info">
                已选中 <strong>{{ selectedSchemas.length }}</strong> 个{{ getSchemaTerminology(selectedResource.resource_type) }}
              </div>

              <!-- 批量操作按钮 -->
              <el-button
                type="primary"
                size="default"
                @click="handleBatchScan"
                :disabled="!selectedSchemas.length"
                :loading="scanning"
              >
                <el-icon><Search /></el-icon>
                批量扫描
              </el-button>

              <!-- 刷新按钮 -->
              <el-button
                @click="loadSchemas"
                :loading="loadingSchemas"
                size="default"
              >
                <el-icon><Refresh /></el-icon>
                刷新
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
              <el-table-column :label="schemaColumnLabel" width="250">
                <template #default="{ row }">
                  <div class="schema-info">
                    <!-- 第一行：名称 + 状态标签 + 调度图标 -->
                    <div class="schema-header">
                      <span class="schema-name">{{ row.name }}</span>
                      <el-tag
                        size="small"
                        :type="row.scan_status === '已扫描' ? 'success' : row.scan_status === '扫描中' ? 'warning' : 'info'"
                      >
                        {{ row.scan_status }}
                      </el-tag>

                      <!-- 调度状态图标 -->
                      <el-tooltip
                        v-if="getSchemaPlan(row)"
                        :content="`独立调度：${getSchemaPlan(row).description}\n下次执行：${getSchemaPlan(row).nextRun}`"
                        placement="top"
                      >
                        <el-icon color="var(--el-color-primary)" :size="16" class="schedule-icon">
                          <Clock />
                        </el-icon>
                      </el-tooltip>
                      <el-tooltip
                        v-else-if="hasEngineSchedule"
                        :content="`继承引擎调度：${engineScheduleDesc}`"
                        placement="top"
                      >
                        <el-icon color="var(--addp-text-tertiary)" :size="16" class="schedule-icon">
                          <Link />
                        </el-icon>
                      </el-tooltip>
                    </div>

                    <!-- 第二行：次要信息（小字灰色） -->
                    <div class="schema-details">
                      <span v-if="row.table_count !== undefined">
                        <el-icon :size="12"><Document /></el-icon>
                        {{ row.table_count }}张表
                      </span>
                      <span v-if="row.scanned_at" class="detail-separator">·</span>
                      <span v-if="row.scanned_at">
                        上次扫描：{{ formatShortTime(row.scanned_at) }}
                      </span>
                    </div>
                  </div>
                </template>
              </el-table-column>
              <el-table-column label="操作" width="240" fixed="right">
                <template #default="{ row }">
                  <div class="schema-actions">
                    <el-button
                      type="primary"
                      size="default"
                      @click.stop="handleScanSchema(row)"
                      :loading="scanningSchemas[row.id ?? (row.schema_name || row.name)]"
                    >
                      <el-icon><Search /></el-icon>
                      {{ row.scan_status === '已扫描' ? '重新扫描' : '扫描' }}
                    </el-button>
                    <el-button
                      type="success"
                      size="default"
                      plain
                      @click.stop="handleSchemaSchedule(row)"
                    >
                      <el-icon><Clock /></el-icon>
                      调度
                    </el-button>
                  </div>
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
      title="引擎定时扫描设置"
      width="600px"
      @close="resetScheduleForm"
    >
      <!-- 继承关系统计 -->
      <el-alert
        v-if="inheritanceInfo && inheritanceInfo.independent > 0"
        type="info"
        :closable="false"
        style="margin-bottom: 16px"
      >
        当前引擎下共 {{ inheritanceInfo.total }} 个{{ getSchemaTerminology(selectedResource.resource_type) }}：
        <ul style="margin: 8px 0 0 20px">
          <li>{{ inheritanceInfo.independent }} 个已配置独立调度</li>
          <li>{{ inheritanceInfo.inherited }} 个将继承引擎调度</li>
        </ul>
        <div style="margin-top: 8px; color: var(--addp-text-tertiary)">
          引擎调度只会扫描未配置独立调度的{{ getSchemaTerminology(selectedResource.resource_type) }}
        </div>
      </el-alert>

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

    <!-- Schema调度设置对话框 -->
    <el-dialog
      v-model="schemaScheduleDialogVisible"
      :title="`${currentSchema?.name || ''} - 定时扫描设置`"
      width="600px"
    >
      <!-- 继承说明 -->
      <el-alert
        v-if="hasEngineSchedule && !currentSchemaTask"
        type="info"
        :closable="false"
        style="margin-bottom: 16px"
      >
        当前继承引擎级调度：{{ engineScheduleDesc }}
        <br />配置独立调度后将不再继承引擎设置
      </el-alert>

      <!-- 调度配置 -->
      <ScheduleConfig v-model="schemaScheduleCron" />

      <el-form label-width="100px" style="margin-top: 20px">
        <el-form-item label="扫描深度">
          <el-radio-group v-model="schemaScheduleDepth">
            <el-radio value="basic">基础扫描（仅结构）</el-radio>
            <el-radio value="deep">深度扫描（含数据统计）</el-radio>
          </el-radio-group>
        </el-form-item>

        <el-form-item label="是否启用">
          <el-switch v-model="schemaScheduleEnabled" />
        </el-form-item>
      </el-form>

      <template #footer>
        <el-button @click="schemaScheduleDialogVisible = false">取消</el-button>
        <el-button
          type="primary"
          @click="submitSchemaSchedule"
          :loading="savingSchedule"
        >
          保存
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, reactive, watch, nextTick, onBeforeUnmount } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Search, Refresh, CircleCheck, CircleClose, Warning, QuestionFilled, Clock, Link, Document } from '@element-plus/icons-vue'
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

// Schema调度相关
const schemaScheduleDialogVisible = ref(false)
const currentSchema = ref(null)
const currentSchemaTask = ref(null)
const schemaScheduleCron = ref('')
const schemaScheduleDepth = ref('deep')
const schemaScheduleEnabled = ref(true)

const leftPanelWidth = ref(560)
const isResizing = ref(false)
const minLeftPanelWidth = 440
const minRightPanelWidth = 240
let resizeStartX = 0
let resizeStartWidth = leftPanelWidth.value

// 计算属性：过滤后的引擎列表（当前不进行筛选，直接返回所有引擎）
const filteredEngines = computed(() => {
  return engines.value
})

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

// Schema调度相关computed
const getSchemaPlan = (schema) => {
  const schemaName = schema.schema_name || schema.name
  const task = allScanTasks.value.find(task => {
    if (task.engine_id !== selectedResource.value.id) return false
    const params = task.parameters || {}
    const schemas = params.schema_names || []
    const paths = params.object_paths || []
    // 精确匹配：该任务只扫描这一个schema/bucket
    const isObjectStorage = isObjectStorageType(selectedResource.value.resource_type)
    if (isObjectStorage) {
      const path = schema.path || schema.name
      return paths.length === 1 && paths[0] === path
    } else {
      return schemas.length === 1 && schemas[0] === schemaName
    }
  })

  if (!task) return null

  return {
    enabled: task.enabled,
    description: describeCron(task.schedule),
    nextRun: task.next_run_at ? formatDateTime(task.next_run_at) : '',
    taskId: task.id
  }
}

const hasEngineSchedule = computed(() => {
  return !!autoScheduleTask.value?.enabled
})

const engineScheduleDesc = computed(() => {
  if (!autoScheduleTask.value) return ''
  return describeCron(autoScheduleTask.value.schedule)
})

const inheritanceInfo = computed(() => {
  if (!selectedResource.value || !schemas.value.length) return null

  const allSchemas = schemas.value.length
  const withOwnSchedule = schemas.value.filter(s => getSchemaPlan(s)).length
  const inheritedCount = allSchemas - withOwnSchedule

  return {
    total: allSchemas,
    independent: withOwnSchedule,
    inherited: inheritedCount,
    hasEngineSchedule: hasEngineSchedule.value
  }
})

// 计算右侧面板标题（根据引擎类型显示 Schema 或 Collection 或 Bucket）
const rightPanelTitle = computed(() => {
  if (!selectedResource.value) return 'Schema列表'
  const terminology = getSchemaTerminology(selectedResource.value.resource_type)
  return `${terminology}列表 - ${selectedResource.value.name}`
})

// 计算表格列标题（根据引擎类型显示 Schema信息 或 Collection信息 或 Bucket信息）
const schemaColumnLabel = computed(() => {
  if (!selectedResource.value) return 'Schema信息'
  const terminology = getSchemaTerminology(selectedResource.value.resource_type)
  return `${terminology}信息`
})

// 加载引擎列表
const loadEngines = async () => {
  loadingResources.value = true
  try {
    const res = await metaApi.getResources()
    // createAPIClient 提取了 axios 的 response.data，后端直接返回数组
    engines.value = Array.isArray(res) ? res : []
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

// 判断是否为对象存储类型
const isObjectStorageType = (resourceType) => {
  if (!resourceType) return false
  const type = resourceType.toLowerCase()
  return ['s3', 'minio', 'oss', 'object_storage', 'object-storage'].includes(type)
}

// 判断是否为 NoSQL 数据库类型
const isNoSQLType = (resourceType) => {
  if (!resourceType) return false
  const type = resourceType.toLowerCase()
  return ['mongodb'].includes(type)
}

// 获取 Schema/Collection/Bucket 的术语
const getSchemaTerminology = (resourceType, plural = false) => {
  if (isObjectStorageType(resourceType)) {
    return plural ? 'Bucket' : 'Bucket'
  }
  if (isNoSQLType(resourceType)) {
    return plural ? 'Collection' : 'Collection'
  }
  return plural ? 'Schema' : 'Schema'
}

// 加载Schema列表
const loadSchemas = async () => {
  if (!selectedResource.value) return

  loadingSchemas.value = true
  let availableSchemas = []
  let connectionError = null

  // 判断是否为对象存储类型
  const isObjectStorage = isObjectStorageType(selectedResource.value.resource_type)

  try {
    // 检查引擎连接状态，如果已知离线，直接跳过实际连接
    if (selectedResource.value.connection_status === 'offline') {
      connectionError = new Error(`引擎离线: ${selectedResource.value.check_message || '连接失败'}`)
      console.warn('资源已标记为离线，跳过实际连接:', selectedResource.value.name)
    } else {
      // 引擎在线或状态未知，尝试获取实际Schema/Bucket列表
      try {
        if (isObjectStorage) {
          // 对象存储：获取节点列表（buckets）
          const nodesRes = await metaApi.listObjectStorageNodes(selectedResource.value.id)
          const nodes = Array.isArray(nodesRes) ? nodesRes : []
          // 转换为 schema 格式以兼容现有UI
          availableSchemas = nodes.map(node => ({
            name: node.name,
            node_type: node.node_type,
            path: node.path || node.name
          }))
        } else {
          // 关系型数据库：获取Schema列表
          const availableRes = await metaApi.listAvailableSchemas(selectedResource.value.id)
          availableSchemas = Array.isArray(availableRes) ? availableRes : []
        }
      } catch (error) {
        // 捕获连接错误，但不阻止后续加载
        connectionError = error
        console.warn('获取可用Schema/Bucket失败（可能存储引擎离线）:', error.response?.data?.error || error.message)
      }
    }
  } catch (error) {
    // 不应该到这里，但保险起见
    connectionError = error
  }

  try {
    // 再获取已扫描的schema状态信息
    const scannedRes = await metaApi.getSchemas(selectedResource.value.id)
    const scannedSchemas = Array.isArray(scannedRes) ? scannedRes : []

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
        scanned_at: scanned.scanned_at || '',
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
          scanned_at: scanned?.scanned_at || '',
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
    'online': 'var(--el-color-success)',
    'offline': 'var(--el-color-danger)',
    'unknown': 'var(--addp-text-tertiary)',
    'checking': 'var(--el-color-warning)'
  }
  return colorMap[status] || 'var(--addp-text-tertiary)'
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

    scanResult.value = res
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

  const terminology = getSchemaTerminology(selectedResource.value.resource_type)
  const isObjectStorage = isObjectStorageType(selectedResource.value.resource_type)

  try {
    await ElMessageBox.confirm(
      `将扫描 ${selectedSchemas.value.length} 个${terminology}，是否继续？`,
      `确认批量扫描`,
      { type: 'warning' }
    )

    scanning.value = true
    showScanDialog.value = true
    scanProgress.value = 0
    scanMessage.value = '正在扫描...'
    scanResult.value = null

    let schemaNames = null
    let objectPaths = null

    if (isObjectStorage) {
      // 对象存储：传递路径列表
      objectPaths = selectedSchemas.value.map(item => item.path || item.name)
    } else {
      // 关系型数据库：传递Schema名称列表
      schemaNames = selectedSchemas.value.map(item => item.schema_name || item.name)
    }

    // 模拟进度
    const progressInterval = setInterval(() => {
      if (scanProgress.value < 90) {
        scanProgress.value += 10
      }
    }, 500)

    const res = await metaApi.scanEngine(selectedResource.value.id, schemaNames, objectPaths)
    clearInterval(progressInterval)
    scanProgress.value = 100

    scanResult.value = res
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

  // 允许 cron 为空（清除调度）

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
      schedule: scheduleCron.value,  // 允许为空字符串
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

// Schema调度相关方法
const handleSchemaSchedule = async (schema) => {
  currentSchema.value = schema
  const schemaName = schema.schema_name || schema.name
  const isObjectStorage = isObjectStorageType(selectedResource.value.resource_type)

  // 查找该Schema的调度任务
  currentSchemaTask.value = allScanTasks.value.find(task => {
    if (task.engine_id !== selectedResource.value.id) return false
    const params = task.parameters || {}
    const schemas = params.schema_names || []
    const paths = params.object_paths || []

    if (isObjectStorage) {
      const path = schema.path || schema.name
      return paths.length === 1 && paths[0] === path
    } else {
      return schemas.length === 1 && schemas[0] === schemaName
    }
  })

  // 预填表单
  if (currentSchemaTask.value) {
    schemaScheduleCron.value = currentSchemaTask.value.schedule || ''
    schemaScheduleDepth.value = currentSchemaTask.value.parameters?.scan_depth || 'deep'
    schemaScheduleEnabled.value = currentSchemaTask.value.enabled
  } else {
    // 默认继承引擎设置或使用默认值
    schemaScheduleCron.value = autoScheduleTask.value?.schedule || '0 2 * * *'
    schemaScheduleDepth.value = 'deep'
    schemaScheduleEnabled.value = true
  }

  schemaScheduleDialogVisible.value = true
}

const submitSchemaSchedule = async () => {
  if (!currentSchema.value) return

  const schemaName = currentSchema.value.schema_name || currentSchema.value.name
  const isObjectStorage = isObjectStorageType(selectedResource.value.resource_type)

  savingSchedule.value = true
  try {
    const terminology = getSchemaTerminology(selectedResource.value.resource_type)
    const payload = {
      name: `${selectedResource.value.name} - ${schemaName}`,
      description: `${terminology} ${schemaName} 的定时扫描`,
      schema_names: isObjectStorage ? [] : [schemaName],
      object_paths: isObjectStorage ? [currentSchema.value.path || schemaName] : [],
      scan_depth: schemaScheduleDepth.value,
      schedule_type: 'cron',
      schedule: schemaScheduleCron.value,
      enabled: schemaScheduleEnabled.value
    }

    if (currentSchemaTask.value) {
      // 更新现有任务
      await metaApi.updateScanTask(
        selectedResource.value.id,
        currentSchemaTask.value.id,
        payload
      )
      ElMessage.success('调度设置已更新')
    } else {
      // 创建新任务
      await metaApi.createScanTask(selectedResource.value.id, payload)
      ElMessage.success('调度设置已创建')
    }

    schemaScheduleDialogVisible.value = false
    await loadScanTasks()
  } catch (error) {
    ElMessage.error('保存失败: ' + (error.response?.data?.error || error.message))
  } finally {
    savingSchedule.value = false
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

// 格式化简短时间（只显示日期，完整时间在tooltip中）
function formatShortTime(datetime) {
  if (!datetime) return ''
  const date = new Date(datetime)
  const now = new Date()
  const diffDays = Math.floor((now - date) / (1000 * 60 * 60 * 24))

  if (diffDays === 0) return '今天'
  if (diffDays === 1) return '昨天'
  if (diffDays < 7) return `${diffDays}天前`

  return date.toLocaleDateString('zh-CN', {
    month: '2-digit',
    day: '2-digit'
  })
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

/* ========== 引擎信息列 ========== */
.engine-info {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 4px 0;
}

.engine-name {
  display: flex;
  align-items: center;
  gap: 8px;
}

.engine-type {
  flex-shrink: 0;
}

.name-text {
  font-weight: 500;
  color: var(--addp-text-primary);
  font-size: 14px;
}

.engine-connection {
  display: flex;
  align-items: center;
}

.connection-status {
  display: flex;
  align-items: center;
  gap: 4px;
  font-size: 12px;
  color: var(--addp-text-secondary);
}

.engine-stats {
  font-size: 12px;
  color: var(--addp-text-tertiary);
  cursor: help;
}

.stat-scanned {
  color: var(--el-color-success);
  margin-left: 4px;
}

.stat-unscanned {
  color: var(--el-color-warning);
  margin-left: 4px;
}

/* ========== 状态概览列 ========== */
.status-overview {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.schedule-status {
  display: flex;
  align-items: center;
}

.schedule-indicator {
  display: flex;
  align-items: center;
  gap: 4px;
  font-size: 12px;
  color: var(--addp-text-secondary);
  cursor: help;
}

.schedule-none {
  color: #C0C4CC;
}

.last-scan {
  font-size: 12px;
  color: var(--addp-text-tertiary);
}

.last-scan .label {
  color: #C0C4CC;
}

.last-scan .time {
  color: var(--addp-text-secondary);
}

/* ========== 引擎操作列 ========== */
.engine-actions {
  display: flex;
  gap: 8px;
}

/* ========== Schema信息列 ========== */
.schema-info {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 4px 0;
}

.schema-header {
  display: flex;
  align-items: center;
  gap: 8px;
}

.schema-name {
  font-weight: 500;
  color: var(--addp-text-primary);
  font-size: 14px;
}

.schedule-icon {
  cursor: help;
  flex-shrink: 0;
}

.schema-details {
  font-size: 12px;
  color: var(--addp-text-tertiary);
  display: flex;
  align-items: center;
  gap: 4px;
}

.detail-separator {
  color: var(--addp-border-color);
}

/* ========== Schema操作列 ========== */
.schema-actions {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}

/* ========== 批量操作栏 ========== */
.schema-actions-bar {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}

.selection-info {
  font-size: 14px;
  color: var(--addp-text-secondary);
  padding: 0 8px;
}

.selection-info strong {
  color: var(--el-color-primary);
  font-size: 16px;
}

/* ========== 表格整体优化 ========== */
.left-panel :deep(.el-table) {
  font-size: 13px;
}

.right-panel :deep(.el-table) {
  font-size: 13px;
}

/* 高亮当前行 */
.left-panel :deep(.el-table__row.current-row) {
  background-color: #ecf5ff;
}

/* 按钮大小调整 */
.engine-actions .el-button,
.schema-actions .el-button {
  min-width: 80px;
}

/* ========== 原有样式保留 ========== */
.left-panel {
  flex: 0 0 auto;
  padding-right: 12px;
  border-right: 1px solid #f2f3f5;
  box-sizing: border-box;
}

.panel-resizer {
  flex: 0 0 6px;
  cursor: col-resize;
  background: linear-gradient(180deg, var(--addp-border-color) 0%, #c0c4cc 100%);
  border-radius: 3px;
  align-self: stretch;
  margin: 0 4px;
}

.panel-resizer:hover {
  background: linear-gradient(180deg, #c0c4cc 0%, var(--addp-text-tertiary) 100%);
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
  color: var(--addp-text-tertiary);
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
