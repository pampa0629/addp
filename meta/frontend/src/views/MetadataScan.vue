<template>
  <div class="metadata-scan">
    <el-card>
      <div
        v-if="activeScan.visible"
        class="scan-status"
      >
        <div class="scan-status__header">
          <span class="scan-status__title">{{ activeScan.title }}</span>
          <span class="scan-status__percent">{{ activeScan.percent }}%</span>
        </div>
        <el-progress
          :percentage="activeScan.percent"
          :status="activeScan.status"
          :stroke-width="6"
          :show-text="false"
        />
        <div class="scan-status__detail">{{ activeScan.detail }}</div>
      </div>

      <div class="scan-container" ref="containerRef">
        <!-- 左侧：存储引擎列表 -->
        <div class="left-panel" :style="{ width: leftPanelWidth + 'px' }">
          <div class="panel-header">
            <h3>{{ t('meta.scan.storageEngineList') }}</h3>
            <el-button
              type="primary"
              @click="handleCreateUnscannedScanRuns"
              :loading="unscannedScanning"
              class="unscanned-scan-button"
            >
              <el-icon><Search /></el-icon>
              {{ t('meta.scan.unscannedScan') }}
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
            <el-table-column :label="t('meta.scan.engineInfo')" min-width="220">
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

                  <!-- 第三行：顶层资源统计（tooltip显示） -->
                  <el-tooltip placement="top">
                    <template #content>
                      {{ t('meta.scan.totalCount', { term: getCatalogEntryTerminology(row), n: row.total_catalog_nodes || 0 }) }}<br>
                      {{ t('meta.scan.scannedCount', { term: getCatalogEntryTerminology(row), n: row.scanned_catalog_nodes || 0 }) }}<br>
                      {{ t('meta.scan.unscannedCount', { term: getCatalogEntryTerminology(row), n: row.unscanned_catalog_nodes || 0 }) }}
                    </template>
                    <div class="engine-stats">
                      {{ row.total_catalog_nodes || 0 }}{{ getCatalogEntryTerminology(row) }}
                      <span class="stat-scanned" v-if="row.scanned_catalog_nodes">({{ row.scanned_catalog_nodes }}{{ t('meta.scan.scannedSuffix', { n: '' }).replace('{n}', '') }})</span>
                      <span class="stat-unscanned" v-if="row.unscanned_catalog_nodes">/{{ row.unscanned_catalog_nodes }}{{ t('meta.scan.unscannedSuffix', { n: '' }).replace('{n}', '') }}</span>
                    </div>
                  </el-tooltip>
                </div>
              </template>
            </el-table-column>
            <el-table-column :label="t('meta.scan.statusOverview')" width="180">
              <template #default="{ row }">
                <div class="status-overview">
                  <!-- 调度状态 -->
                  <div class="schedule-status">
                    <el-tooltip
                      v-if="resourcePlanMap[row.id]"
                      :content="`${resourcePlanMap[row.id].description}\n${t('meta.scan.nextRun', { time: resourcePlanMap[row.id].nextRun })}`"
                      placement="top"
                    >
                      <div class="schedule-indicator">
                        <el-icon :color="resourcePlanMap[row.id].enabled ? 'var(--el-color-success)' : 'var(--addp-text-tertiary)'">
                          <Clock />
                        </el-icon>
                        <span>{{ resourcePlanMap[row.id].enabled ? t('meta.scan.scheduleEnabled') : t('meta.scan.scheduleDisabled') }}</span>
                      </div>
                    </el-tooltip>
                    <div v-else class="schedule-indicator schedule-none">
                      <el-icon color="#C0C4CC"><Clock /></el-icon>
                      <span>{{ t('meta.scan.noSchedule') }}</span>
                    </div>
                  </div>

                  <!-- 上次扫描 -->
                  <div class="last-scan" v-if="row.scanned_at">
                    <span class="label">{{ t('meta.scan.lastScan') }}</span>
                    <span class="time">{{ formatShortTime(row.scanned_at) }}</span>
                  </div>
                </div>
              </template>
            </el-table-column>
            <el-table-column :label="t('meta.scan.actions')" width="140" fixed="right">
              <template #default="{ row }">
                <div class="engine-actions">
                  <el-button
                    type="success"
                    size="default"
                    plain
                    @click.stop="handleScheduleClick(row)"
                  >
                    <el-icon><Clock /></el-icon>
                    {{ t('meta.scan.schedule') }}
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

        <!-- 右侧：顶层资源列表 -->
        <div class="right-panel">
          <div class="panel-header">
            <h3>{{ rightPanelTitle }}</h3>
            <div v-if="selectedResource" class="catalogEntry-actions-bar">
              <!-- 选中提示 -->
              <div v-if="selectedCatalogEntries.length" class="selection-info">
                {{ t('meta.scan.selectedCount', { n: selectedCatalogEntries.length, term: getCatalogEntryTerminology(selectedResource) }) }}
              </div>

              <!-- 批量操作按钮 -->
              <el-button
                type="primary"
                size="default"
                @click="handleBatchScan"
                :disabled="!selectedCatalogEntries.length"
                :loading="scanning"
              >
                <el-icon><Search /></el-icon>
                {{ t('meta.scan.batchScan') }}
              </el-button>

              <!-- 刷新按钮 -->
              <el-button
                @click="loadCatalogEntries"
                :loading="loadingCatalogEntries"
                size="default"
              >
                <el-icon><Refresh /></el-icon>
                {{ t('meta.scan.refresh') }}
              </el-button>
            </div>
          </div>

          <div v-if="!selectedResource" class="empty-state">
            <el-empty :description="t('meta.scan.selectEngineHint')" />
          </div>

          <div v-else class="catalogEntry-table-wrapper">
            <el-table
              class="catalogEntry-table"
              :data="catalogEntries"
              v-loading="loadingCatalogEntries"
              height="600"
              @selection-change="handleCatalogEntrySelectionChange"
              style="min-width: 720px"
            >
              <el-table-column type="selection" width="55" />
              <el-table-column :label="catalogEntryColumnLabel" width="250">
                <template #default="{ row }">
                  <div class="catalogEntry-info">
                    <!-- 第一行：名称 + 状态标签 + 调度图标 -->
                    <div class="catalogEntry-header">
                      <span class="catalogEntry-name">{{ row.name }}</span>
                      <el-tag
                        size="small"
                        :type="row.scan_status === 'completed' ? 'success' : row.scan_status === 'running' ? 'warning' : 'info'"
                      >
                        {{ t(`meta.status.${row.scan_status}`) || row.scan_status }}
                      </el-tag>

                      <!-- 调度状态图标 -->
                      <el-tooltip
                        v-if="getCatalogEntryPlan(row)"
                        :content="t('meta.scan.independentScheduleTooltip', { desc: getCatalogEntryPlan(row).description, next: getCatalogEntryPlan(row).nextRun })"
                        placement="top"
                      >
                        <el-icon color="var(--el-color-primary)" :size="16" class="schedule-icon">
                          <Clock />
                        </el-icon>
                      </el-tooltip>
                      <el-tooltip
                        v-else-if="hasEngineSchedule"
                        :content="t('meta.scan.inheritEngineScheduleTooltip', { desc: engineScheduleDesc })"
                        placement="top"
                      >
                        <el-icon color="var(--addp-text-tertiary)" :size="16" class="schedule-icon">
                          <Link />
                        </el-icon>
                      </el-tooltip>
                    </div>

                    <!-- 第二行：次要信息（小字灰色） -->
                    <div class="catalogEntry-details">
                      <span v-if="row.leaf_count !== undefined">
                        <el-icon :size="12"><Document /></el-icon>
                        {{ row.leaf_count }}{{ t('meta.scan.tables') }}
                      </span>
                      <span v-if="row.scanned_at" class="detail-separator">·</span>
                      <span v-if="row.scanned_at">
                        {{ t('meta.scan.lastScanTime', { time: formatShortTime(row.scanned_at) }) }}
                      </span>
                    </div>
                  </div>
                </template>
              </el-table-column>
              <el-table-column :label="t('meta.scan.actions')" width="240" fixed="right">
                <template #default="{ row }">
                  <div class="catalogEntry-actions">
                    <el-button
                      type="primary"
                      size="default"
                      @click.stop="handleScanCatalogEntry(row)"
                      :loading="scanningCatalogEntries[row.id ?? catalogEntryNameOf(row)]"
                    >
                      <el-icon><Search /></el-icon>
                      {{ row.scan_status === 'completed' ? t('meta.scan.rescan') : t('meta.scan.scan') }}
                    </el-button>
                    <el-button
                      type="success"
                      size="default"
                      plain
                      @click.stop="handleCatalogEntrySchedule(row)"
                    >
                      <el-icon><Clock /></el-icon>
                      {{ t('meta.scan.schedule') }}
                    </el-button>
                  </div>
                </template>
              </el-table-column>
            </el-table>
          </div>
        </div>
      </div>
    </el-card>

    <el-dialog
      v-model="scheduleDialogVisible"
      :title="t('meta.scan.engineScheduleSettings')"
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
        {{ t('meta.scan.engineEntryCount', { n: inheritanceInfo.total, term: getCatalogEntryTerminology(selectedResource) }) }}
        <ul style="margin: 8px 0 0 20px">
          <li>{{ inheritanceInfo.independent }}{{ t('meta.scan.withIndependentSchedule') }}</li>
          <li>{{ inheritanceInfo.inherited }}{{ t('meta.scan.willInheritSchedule') }}</li>
        </ul>
        <div style="margin-top: 8px; color: var(--addp-text-tertiary)">
          {{ t('meta.scan.engineScheduleNote', { term: getCatalogEntryTerminology(selectedResource) }) }}
        </div>
      </el-alert>

      <ScheduleConfig v-model="scheduleCron" />

      <el-form label-width="100px" style="margin-top: 20px">
        <el-form-item :label="t('meta.scan.enableSchedule')">
          <el-switch v-model="scheduleEnabled" />
        </el-form-item>
        <div class="schedule-hint">
          {{ t('meta.scan.scheduleHint') }}
        </div>
      </el-form>
      <template #footer>
        <el-button @click="scheduleDialogVisible = false">{{ t('meta.scan.cancel') }}</el-button>
        <el-button type="primary" @click="submitScheduleForm" :loading="savingSchedule">
          {{ t('meta.scan.save') }}
        </el-button>
      </template>
    </el-dialog>

    <!-- 顶层资源调度设置对话框 -->
    <el-dialog
      v-model="catalogEntryScheduleDialogVisible"
      :title="`${currentCatalogEntry?.name || ''}${t('meta.scan.entryScheduleTitleSuffix')}`"
      width="600px"
    >
      <!-- 继承说明 -->
      <el-alert
        v-if="hasEngineSchedule && !currentCatalogEntryTask"
        type="info"
        :closable="false"
        style="margin-bottom: 16px"
      >
        {{ t('meta.scan.inheritEngineSchedule', { desc: engineScheduleDesc }) }}
        <br />{{ t('meta.scan.independentScheduleNote') }}
      </el-alert>

      <!-- 调度配置 -->
      <ScheduleConfig v-model="catalogEntryScheduleCron" />

      <el-form label-width="100px" style="margin-top: 20px">
        <el-form-item :label="t('meta.scan.scanDepth')">
          <el-radio-group v-model="catalogEntryScheduleDepth">
            <el-radio value="basic">{{ t('meta.scan.basicScan') }}</el-radio>
            <el-radio value="deep">{{ t('meta.scan.deepScan') }}</el-radio>
          </el-radio-group>
        </el-form-item>

        <el-form-item :label="t('meta.scan.enableSchedule')">
          <el-switch v-model="catalogEntryScheduleEnabled" />
        </el-form-item>
      </el-form>

      <template #footer>
        <el-button @click="catalogEntryScheduleDialogVisible = false">{{ t('meta.scan.cancel') }}</el-button>
        <el-button
          type="primary"
          @click="submitCatalogEntrySchedule"
          :loading="savingSchedule"
        >
          {{ t('meta.scan.save') }}
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, reactive, watch, nextTick, onBeforeUnmount } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { Search, Refresh, CircleCheck, CircleClose, Warning, QuestionFilled, Clock, Link, Document } from '@element-plus/icons-vue'
import { ScheduleConfig, describeCron, decodeScheduleToForm } from '@common-ui'
import metaApi from '../api/meta'

const { t } = useI18n()

const AUTO_SCHEDULE_DESC_MARK = '[PortalAutoSchedule]'
const SCAN_RUN_POLL_INTERVAL_MS = 2000
const SCAN_STATUS_HIDE_DELAY_MS = 5000
const ACTIVE_SCAN_STATUSES = new Set(['pending', 'running'])
const SUCCESS_SCAN_STATUSES = new Set(['success'])
const FAILED_SCAN_STATUSES = new Set(['failed', 'timeout', 'cancelled', 'canceled'])

// 引擎列表
const engines = ref([])
const resourceTableRef = ref(null)
const loadingResources = ref(false)
const selectedResource = ref(null)
const containerRef = ref(null)

// 引擎顶层资源列表
const catalogEntries = ref([])
const loadingCatalogEntries = ref(false)
const selectedCatalogEntries = ref([])

// 扫描状态
const unscannedScanning = ref(false)
const scanning = ref(false)
const scanningCatalogEntries = reactive({})
const activeScan = ref({
  visible: false,
  title: '',
  detail: '',
  percent: 0,
  status: ''
})

const allScanTasks = ref([])
const scheduleDialogVisible = ref(false)
const savingSchedule = ref(false)
const scheduleCron = ref('') // Cron 表达式
const scheduleEnabled = ref(true) // 是否启用

// 顶层资源调度相关
const catalogEntryScheduleDialogVisible = ref(false)
const currentCatalogEntry = ref(null)
const currentCatalogEntryTask = ref(null)
const catalogEntryScheduleCron = ref('')
const catalogEntryScheduleDepth = ref('deep')
const catalogEntryScheduleEnabled = ref(true)

const leftPanelWidth = ref(560)
const isResizing = ref(false)
const minLeftPanelWidth = 440
const minRightPanelWidth = 240
let resizeStartX = 0
let resizeStartWidth = leftPanelWidth.value
let disposed = false
let scanStatusTimer = 0

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

// 顶层资源调度相关 computed
const catalogEntryNameOf = (catalogEntry) => catalogEntry?.name || ''

const catalogEntryTargetOf = (catalogEntry) => {
  if (!catalogEntry) return ''
  if (usesCatalogPathTargets(selectedResource.value)) {
    return catalogEntry.path || catalogEntry.name || ''
  }
  return catalogEntryNameOf(catalogEntry)
}

const getCatalogEntryPlan = (catalogEntry) => {
  const catalogEntryName = catalogEntryNameOf(catalogEntry)
  const task = allScanTasks.value.find(task => {
    if (task.engine_id !== selectedResource.value.id) return false
    const params = task.parameters || {}
    const paths = params.catalog_paths || []
    // 精确匹配：该任务只扫描这一个顶层资源。
    const target = catalogEntryTargetOf(catalogEntry) || catalogEntryName
    return paths.length === 1 && paths[0] === target
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
  if (!selectedResource.value || !catalogEntries.value.length) return null

  const totalEntries = catalogEntries.value.length
  const withOwnSchedule = catalogEntries.value.filter(entry => getCatalogEntryPlan(entry)).length
  const inheritedCount = totalEntries - withOwnSchedule

  return {
    total: totalEntries,
    independent: withOwnSchedule,
    inherited: inheritedCount,
    hasEngineSchedule: hasEngineSchedule.value
  }
})

// 计算右侧面板标题（根据引擎类型显示 Schema、数据库、Bucket 或目录）
const rightPanelTitle = computed(() => {
  if (!selectedResource.value) return t('meta.scan.catalogEntryList')
  const terminology = getCatalogEntryTerminology(selectedResource.value)
  return `${terminology}${t('meta.scan.entryListSuffix')} - ${selectedResource.value.name}`
})

// 计算表格列标题（根据引擎类型显示 Schema、数据库、Bucket 或目录信息）
const catalogEntryColumnLabel = computed(() => {
  if (!selectedResource.value) return t('meta.scan.entryInfo')
  const terminology = getCatalogEntryTerminology(selectedResource.value)
  return `${terminology}${t('meta.scan.entryInfoSuffix')}`
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
      await Promise.all([loadCatalogEntries(), loadScanTasks()])
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
    ElMessage.error(t('meta.scan.loadEnginesFailed', { msg: error.response?.data?.error || error.message }))
  } finally {
    loadingResources.value = false
  }
}

// 选择引擎
const handleSelectResource = async (row) => {
  selectedResource.value = row
  await nextTick()
  resourceTableRef.value?.setCurrentRow(row)
  await loadCatalogEntries()
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

// 判断扫描目标是否需要传递完整路径。依据引擎声明的叶子术语，不列举具体引擎 type。
const usesCatalogPathTargets = (resource) => {
  const itemTerm = String(resource?.catalog_leaf_term || '').toLowerCase()
  return itemTerm === 'object' || itemTerm === 'file'
}

// 判断是否为 NoSQL 数据库类型
const isNoSQLType = (resourceType) => {
  if (!resourceType) return false
  const type = resourceType.toLowerCase()
  return ['mongodb'].includes(type)
}

// 获取引擎原生的顶层资源术语，避免把内部抽象暴露给用户。
const getCatalogEntryTerminology = (resource, plural = false) => {
  if (!resource) return t('meta.scan.defaultCatalogEntryTerm')
  if (typeof resource === 'string') {
    resource = { resource_type: resource }
  }
  const topTerm = String(resource.catalog_top_term || '').toLowerCase()
  const topI18nKey = String(resource.catalog_top_i18n_key || '')
  if (topI18nKey) {
    const translated = t(topI18nKey)
    if (translated !== topI18nKey) {
      return translated
    }
  }
  const itemTerm = String(resource.catalog_leaf_term || '').toLowerCase()
  const rootTerm = String(resource.catalog_root_term || '').toLowerCase()
  switch (topTerm) {
    case 'schema':
      return 'Schema'
    case 'database':
      return t('meta.scan.databaseTerm')
    case 'bucket':
      return 'Bucket'
    case 'directory':
      return t('meta.scan.directoryTerm')
    case 'collection':
      return 'Collection'
  }
  if (itemTerm === 'file' || rootTerm === 'root') {
    return t('meta.scan.directoryTerm')
  }
  if (itemTerm === 'object' || rootTerm === 'service') {
    return 'Bucket'
  }
  const type = String(resource.resource_type || '').toLowerCase()
  if (isNoSQLType(type)) {
    return plural ? 'Collection' : 'Collection'
  }
  return t('meta.scan.defaultCatalogEntryTerm')
}

// 加载引擎顶层资源列表
const loadCatalogEntries = async () => {
  if (!selectedResource.value) return

  loadingCatalogEntries.value = true
  let availableEntries = []
  let connectionError = null

  try {
    // 检查引擎连接状态，如果已知离线，直接跳过实际连接
    if (selectedResource.value.connection_status === 'offline') {
      connectionError = new Error(`引擎离线: ${selectedResource.value.check_message || '连接失败'}`)
      console.warn('资源已标记为离线，跳过实际连接:', selectedResource.value.name)
    } else {
      // 引擎在线或状态未知，统一通过 System 实时资源 API 获取顶层资源
      try {
        const availableRes = await metaApi.listCatalogTopNodes(selectedResource.value.id)
        availableEntries = Array.isArray(availableRes) ? availableRes : []
      } catch (error) {
        // 捕获连接错误，但不阻止后续加载
        connectionError = error
        console.warn('获取可用顶层资源失败（可能存储引擎离线）:', error.response?.data?.error || error.message)
      }
    }
  } catch (error) {
    // 不应该到这里，但保险起见
    connectionError = error
  }

  try {
    // 再获取已扫描的顶层资源状态信息
    const scannedRes = await metaApi.getScannedCatalogTopNodes(selectedResource.value.id)
    const scannedEntries = Array.isArray(scannedRes) ? scannedRes : []

    if (connectionError && scannedEntries.length === 0) {
      // 如果连接失败且没有已扫描的节点，显示空列表
      // 用户已经能从左侧图标看到引擎离线状态，无需重复提示
      catalogEntries.value = []
    } else if (connectionError) {
      // 连接失败但有历史扫描数据，使用历史数据并标记状态
      catalogEntries.value = scannedEntries.map(scanned => ({
        id: scanned.id,
        name: catalogEntryNameOf(scanned),
        scan_status: t('meta.scan.connectionFailed', { status: scanned.scan_status }),
        item_count: scanned.item_count || 0,
        scanned_at: scanned.scanned_at || '',
        total_size_bytes: scanned.total_size_bytes || 0
      }))
      // 已通过左侧连接状态图标显示，无需额外提示
    } else {
      // 正常情况：合并两个列表
      catalogEntries.value = availableEntries
        .filter(available => available?.role === 'branch')
        .map(available => {
        const availableName = catalogEntryNameOf(available)
        const scanned = scannedEntries.find(entry => catalogEntryNameOf(entry) === availableName)
        return {
          ...available,
          name: availableName,
          id: scanned?.id,
          scan_status: scanned?.scan_status || 'pending',
          leaf_count: available.leaf_count,
          item_count: scanned?.item_count || 0,
          scanned_at: scanned?.scanned_at || '',
          total_size_bytes: scanned?.total_size_bytes || 0
        }
      })
    }
  } catch (error) {
    ElMessage.error(t('meta.scan.loadCatalogEntriesFailed', { msg: error.response?.data?.error || error.message }))
    catalogEntries.value = []
  } finally {
    loadingCatalogEntries.value = false
  }
}

// 顶层资源选择变化
const handleCatalogEntrySelectionChange = (selection) => {
  selectedCatalogEntries.value = selection
}

const loadScanTasks = async () => {
  try {
    allScanTasks.value = await metaApi.getScanTasks()
  } catch (error) {
    ElMessage.error(t('meta.scan.loadTasksFailed', { msg: error.response?.data?.error || error.message }))
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
    'online': t('meta.scan.online'),
    'offline': t('meta.scan.offline'),
    'unknown': t('meta.scan.unknown'),
    'checking': t('meta.scan.checking')
  }
  return labelMap[status] || t('meta.scan.notChecked')
}

const getConnectionTooltip = (row) => {
  if (!row.connection_status) return t('meta.scan.notChecked')

  let tooltip = t('meta.scan.statusLabel', { status: getConnectionStatusLabel(row.connection_status) })

  if (row.last_check_at) {
    tooltip += `\n${t('meta.scan.checkTime', { time: row.last_check_at })}`
  }

  if (row.check_message) {
    tooltip += `\n${t('meta.scan.details', { msg: row.check_message })}`
  }

  return tooltip
}

const resetScheduleForm = () => {
  scheduleCron.value = ''
  scheduleEnabled.value = true
}

const deriveAutoTaskEntryPaths = () => {
  if (!Array.isArray(catalogEntries.value) || !catalogEntries.value.length) return []
  return catalogEntries.value
    .map(item => catalogEntryTargetOf(item))
    .filter(Boolean)
}

const getAutoScheduleTaskName = () => {
  if (selectedResource.value?.name) {
    return `${selectedResource.value.name}${t('meta.scan.engineScheduledScan')}`
  }
  return t('meta.scan.scheduledScanTask')
}

const ensureAutoScheduleDescription = desc => {
  const text = typeof desc === 'string' ? desc : ''
  if (text.includes(AUTO_SCHEDULE_DESC_MARK)) {
    return text
  }
  const suffix = text.trim().length ? ` ${text.trim()}` : t('meta.scan.autoCreated')
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

const delay = ms => new Promise(resolve => window.setTimeout(resolve, ms))

const scanRunIDOf = run => run?.execution_id || run?.executionId || run?.id || ''

const notifyScanHook = (hook, payload) => {
  if (typeof hook === 'function') {
    hook(payload)
  }
}

const waitForScanRun = async (run, hooks = {}) => {
  const runID = scanRunIDOf(run)
  if (!runID) {
    return run
  }

  let latest = run
  notifyScanHook(hooks.onProgress, latest)
  while (!disposed) {
    latest = await metaApi.getScanRun(runID)
    notifyScanHook(hooks.onProgress, latest)
    const status = String(latest?.status || '').toLowerCase()
    if (SUCCESS_SCAN_STATUSES.has(status)) {
      return latest
    }
    if (FAILED_SCAN_STATUSES.has(status)) {
      const message = latest?.error_message || latest?.error || latest?.progress_message || status
      throw new Error(message)
    }
    if (!ACTIVE_SCAN_STATUSES.has(status) && status) {
      return latest
    }
    await delay(SCAN_RUN_POLL_INTERVAL_MS)
  }
  return latest
}

const waitForScanRuns = async (runs = [], hooks = {}) => {
  const validRuns = runs.filter(scanRunIDOf)
  for (let index = 0; index < validRuns.length; index += 1) {
    const run = validRuns[index]
    await waitForScanRun(run, {
      onProgress: latest => {
        notifyScanHook(hooks.onProgress, {
          run: latest,
          index,
          total: validRuns.length
        })
      }
    })
    notifyScanHook(hooks.onRunCompleted, {
      run,
      index,
      total: validRuns.length
    })
  }
  return validRuns.length
}

const startScanStatus = (title, detail, percent = 5) => {
  cancelScanStatusTimer()
  activeScan.value = {
    visible: true,
    title,
    detail,
    percent: clampScanPercent(percent),
    status: ''
  }
}

const updateScanStatus = ({ title = '', detail = '', percent = 10, status = '' }) => {
  cancelScanStatusTimer()
  activeScan.value = {
    visible: true,
    title: title || activeScan.value.title || t('meta.scan.scanRunning'),
    detail: detail || activeScan.value.detail || t('meta.scan.scanWaiting'),
    percent: clampScanPercent(percent),
    status
  }
}

const updateScanStatusFromRun = (run, title = '', minPercent = 10) => {
  const progress = Number(run?.progress ?? run?.progress_percent)
  updateScanStatus({
    title: title || scanTitleFromRun(run),
    detail: scanDetailFromRun(run),
    percent: Number.isFinite(progress) ? Math.max(progress, minPercent) : minPercent
  })
}

const updateBatchScanStatus = ({ run, index = 0, total = 1 }, title = '') => {
  const progress = Number(run?.progress ?? run?.progress_percent)
  const runPercent = Number.isFinite(progress) ? clampScanPercent(progress) : 0
  const percent = total > 0
    ? ((index + (runPercent / 100)) / total) * 100
    : runPercent
  updateScanStatus({
    title: title || t('meta.scan.scanRunning'),
    detail: t('meta.scan.scanBatchProgress', {
      current: Math.min(index + 1, total),
      total,
      detail: scanDetailFromRun(run)
    }),
    percent: Math.max(percent, total > 0 ? 5 : 10)
  })
}

const completeScanStatus = (title, detail) => {
  cancelScanStatusTimer()
  activeScan.value = {
    visible: true,
    title,
    detail,
    percent: 100,
    status: 'success'
  }
  scanStatusTimer = window.setTimeout(() => {
    clearScanStatus()
  }, SCAN_STATUS_HIDE_DELAY_MS)
}

const failScanStatus = (error) => {
  cancelScanStatusTimer()
  activeScan.value = {
    visible: true,
    title: t('meta.scan.scanFailed'),
    detail: error?.response?.data?.error || error?.message || t('meta.scan.scanFailed'),
    percent: 100,
    status: 'exception'
  }
}

const clearScanStatus = () => {
  cancelScanStatusTimer()
  activeScan.value = {
    visible: false,
    title: '',
    detail: '',
    percent: 0,
    status: ''
  }
}

const cancelScanStatusTimer = () => {
  if (scanStatusTimer) {
    window.clearTimeout(scanStatusTimer)
    scanStatusTimer = 0
  }
}

const scanTitleFromRun = run => {
  const status = String(run?.status || '').toLowerCase()
  if (status === 'pending') {
    return t('meta.scan.scanSubmitted')
  }
  if (status === 'running') {
    return t('meta.scan.scanRunning')
  }
  return t('meta.scan.scanSubmitted')
}

const scanDetailFromRun = run => run?.current_step || run?.progress_message || run?.message || t('meta.scan.scanWaiting')

const clampScanPercent = value => Math.max(0, Math.min(100, Math.round(Number(value) || 0)))

// 一键补扫未扫描引擎
const handleCreateUnscannedScanRuns = async () => {
	try {
		await ElMessageBox.confirm(
			t('meta.scan.unscannedScanConfirmMsg'),
			t('meta.scan.unscannedScanConfirmTitle'),
			{ type: 'warning' }
		)

    unscannedScanning.value = true
    const res = await metaApi.createUnscannedScanRuns()
    const runs = Array.isArray(res?.runs) ? res.runs : []
	const submitted = Number(res?.submitted || runs.length || 0)
	if (submitted === 0) {
		ElMessage.success(t('meta.scan.unscannedScanNoRuns'))
		return
	}
	startScanStatus(
		t('meta.scan.unscannedScanSubmitted', { n: submitted }),
		t('meta.scan.scanWaiting'),
		5
	)
	await waitForScanRuns(runs, {
		onProgress: payload => updateBatchScanStatus(payload, t('meta.scan.unscannedScanSubmitted', { n: submitted }))
	})
	completeScanStatus(
		t('meta.scan.unscannedScanCompleted', { n: submitted }),
		t('meta.scan.unscannedScanCompleted', { n: submitted })
	)
	ElMessage.success(t('meta.scan.unscannedScanCompleted', { n: submitted }))
	await Promise.all([loadEngines(), loadScanTasks()])
} catch (error) {
	if (error !== 'cancel') {
		failScanStatus(error)
		ElMessage.error(t('meta.scan.unscannedScanFailed', { msg: error.response?.data?.error || error.message }))
	}
} finally {
    unscannedScanning.value = false
  }
}

// 批量扫描顶层资源
const handleBatchScan = async () => {
  if (!selectedCatalogEntries.value.length) return

  const terminology = getCatalogEntryTerminology(selectedResource.value)

  try {
    await ElMessageBox.confirm(
      t('meta.scan.batchScanConfirmMsg', { n: selectedCatalogEntries.value.length, term: terminology }),
      t('meta.scan.batchScanConfirmTitle'),
      { type: 'warning' }
    )

    scanning.value = true

    const catalogPaths = selectedCatalogEntries.value.map(item => catalogEntryTargetOf(item)).filter(Boolean)
    const run = await metaApi.createManualScanRun(selectedResource.value.id, catalogPaths, { scan_depth: 'deep', force: false })
    startScanStatus(t('meta.scan.batchScanSubmitted'), t('meta.scan.scanWaiting'), 5)
    await waitForScanRun(run, {
      onProgress: latest => updateScanStatusFromRun(latest, t('meta.scan.batchScanSubmitted'))
    })
    completeScanStatus(t('meta.scan.batchScanCompleted'), t('meta.scan.batchScanCompleted'))
    ElMessage.success(t('meta.scan.batchScanCompleted'))
    await loadCatalogEntries(selectedResource.value)
  } catch (error) {
    if (error !== 'cancel') {
      failScanStatus(error)
      ElMessage.error(t('meta.scan.batchScanFailed', { msg: error.response?.data?.error || error.message }))
    }
  } finally {
    scanning.value = false
  }
}

const submitScheduleForm = async () => {
  if (!selectedResource.value) {
    ElMessage.warning(t('meta.scan.selectEngineFirst'))
    return
  }

  // 允许 cron 为空（清除调度）

  savingSchedule.value = true
  try {
    const existing = autoScheduleTask.value

    // 统一使用 cron 类型，直接传递 Cron 表达式
    const existingCatalogPaths = existing?.parameters?.catalog_paths || []
    const payload = {
      name: existing?.name || getAutoScheduleTaskName(),
      description: ensureAutoScheduleDescription(existing?.description || ''),
      catalog_paths: existingCatalogPaths.length ? existingCatalogPaths : deriveAutoTaskEntryPaths(),
      scan_depth: existing?.parameters?.scan_depth || 'deep',
      force: existing?.parameters?.force === true,
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

    ElMessage.success(t('meta.scan.scheduleSaved'))
    scheduleDialogVisible.value = false
    await loadScanTasks()
  } catch (error) {
    ElMessage.error(t('meta.scan.saveFailed', { msg: error.response?.data?.error || error.message }))
  } finally {
    savingSchedule.value = false
  }
}

// 扫描单个顶层资源
const handleScanCatalogEntry = async (catalogEntry) => {
  const catalogEntryName = catalogEntryNameOf(catalogEntry)
  const terminology = getCatalogEntryTerminology(selectedResource.value)
  const key = catalogEntry.id ?? catalogEntryName
  scanningCatalogEntries[key] = true

  try {
    const target = catalogEntryTargetOf(catalogEntry) || catalogEntryName
    const run = await metaApi.createManualScanRun(selectedResource.value.id, [target], { scan_depth: 'deep', force: false })
    startScanStatus(t('meta.scan.catalogEntryScanSubmitted', { term: terminology, name: catalogEntryName }), t('meta.scan.scanWaiting'), 5)
    await waitForScanRun(run, {
      onProgress: latest => updateScanStatusFromRun(latest, t('meta.scan.catalogEntryScanSubmitted', { term: terminology, name: catalogEntryName }))
    })
    completeScanStatus(
      t('meta.scan.catalogEntryScanCompleted', { term: terminology, name: catalogEntryName }),
      t('meta.scan.catalogEntryScanCompleted', { term: terminology, name: catalogEntryName })
    )
    ElMessage.success(t('meta.scan.catalogEntryScanCompleted', { term: terminology, name: catalogEntryName }))
    await loadCatalogEntries(selectedResource.value)
  } catch (error) {
    failScanStatus(error)
    ElMessage.error(t('meta.scan.scanError', { msg: error.response?.data?.error || error.message }))
  } finally {
    scanningCatalogEntries[key] = false
  }
}

// 顶层资源调度相关方法
const handleCatalogEntrySchedule = async (catalogEntry) => {
  currentCatalogEntry.value = catalogEntry
  const catalogEntryName = catalogEntryNameOf(catalogEntry)
  // 查找该顶层资源的调度任务
  currentCatalogEntryTask.value = allScanTasks.value.find(task => {
    if (task.engine_id !== selectedResource.value.id) return false
    const params = task.parameters || {}
    const paths = params.catalog_paths || []
    const target = catalogEntryTargetOf(catalogEntry) || catalogEntryName
    return paths.length === 1 && paths[0] === target
  })

  // 预填表单
  if (currentCatalogEntryTask.value) {
    catalogEntryScheduleCron.value = currentCatalogEntryTask.value.schedule || ''
    catalogEntryScheduleDepth.value = currentCatalogEntryTask.value.parameters?.scan_depth || 'deep'
    catalogEntryScheduleEnabled.value = currentCatalogEntryTask.value.enabled
  } else {
    // 默认继承引擎设置或使用默认值
    catalogEntryScheduleCron.value = autoScheduleTask.value?.schedule || '0 2 * * *'
    catalogEntryScheduleDepth.value = 'deep'
    catalogEntryScheduleEnabled.value = true
  }

  catalogEntryScheduleDialogVisible.value = true
}

const submitCatalogEntrySchedule = async () => {
  if (!currentCatalogEntry.value) return

  const catalogEntryName = catalogEntryNameOf(currentCatalogEntry.value)
  const catalogPath = catalogEntryTargetOf(currentCatalogEntry.value) || catalogEntryName

  savingSchedule.value = true
  try {
    const terminology = getCatalogEntryTerminology(selectedResource.value)
    const payload = {
      name: `${selectedResource.value.name} - ${catalogEntryName}`,
      description: `${terminology} ${catalogEntryName} 的定时扫描`,
      catalog_paths: [catalogPath],
      scan_depth: catalogEntryScheduleDepth.value,
      force: false,
      schedule_type: 'cron',
      schedule: catalogEntryScheduleCron.value,
      enabled: catalogEntryScheduleEnabled.value
    }

    if (currentCatalogEntryTask.value) {
      // 更新现有任务
      await metaApi.updateScanTask(
        selectedResource.value.id,
        currentCatalogEntryTask.value.id,
        payload
      )
      ElMessage.success(t('meta.scan.scheduleUpdated'))
    } else {
      // 创建新任务
      await metaApi.createScanTask(selectedResource.value.id, payload)
      ElMessage.success(t('meta.scan.scheduleCreated'))
    }

    catalogEntryScheduleDialogVisible.value = false
    await loadScanTasks()
  } catch (error) {
    ElMessage.error(t('meta.scan.saveFailed', { msg: error.response?.data?.error || error.message }))
  } finally {
    savingSchedule.value = false
  }
}

function formatScheduleDescription(task) {
  if (!task.schedule) {
    return t('meta.scan.manualTrigger')
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

  if (diffDays === 0) return t('meta.scan.today')
  if (diffDays === 1) return t('meta.scan.yesterday')
  if (diffDays < 7) return t('meta.scan.daysAgo', { n: diffDays })

  return date.toLocaleDateString('zh-CN', {
    month: '2-digit',
    day: '2-digit'
  })
}

watch(selectedResource, () => {
  selectedCatalogEntries.value = []
  scheduleDialogVisible.value = false
  resetScheduleForm()
})

onMounted(() => {
  loadEngines()
  window.addEventListener('resize', enforceBounds)
})

onBeforeUnmount(() => {
  disposed = true
  cancelScanStatusTimer()
  stopResizing()
  window.removeEventListener('resize', enforceBounds)
})
</script>

<style scoped>
.metadata-scan {
  padding: 20px;
}

.scan-status {
  position: sticky;
  top: 0;
  z-index: 3;
  margin-bottom: 16px;
  padding: 10px 12px;
  border: 1px solid var(--el-border-color);
  border-radius: 6px;
  background: var(--el-bg-color);
}

.scan-status__header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 6px;
  font-size: 12px;
}

.scan-status__title {
  min-width: 0;
  overflow: hidden;
  color: var(--addp-text-primary);
  font-weight: 500;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.scan-status__percent {
  flex: 0 0 auto;
  color: var(--addp-text-secondary);
  font-variant-numeric: tabular-nums;
}

.scan-status__detail {
  margin-top: 6px;
  overflow: hidden;
  color: var(--addp-text-secondary);
  font-size: 12px;
  line-height: 1.4;
  text-overflow: ellipsis;
  white-space: nowrap;
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

/* ========== CatalogEntry信息列 ========== */
.catalogEntry-info {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 4px 0;
}

.catalogEntry-header {
  display: flex;
  align-items: center;
  gap: 8px;
}

.catalogEntry-name {
  font-weight: 500;
  color: var(--addp-text-primary);
  font-size: 14px;
}

.schedule-icon {
  cursor: help;
  flex-shrink: 0;
}

.catalogEntry-details {
  font-size: 12px;
  color: var(--addp-text-tertiary);
  display: flex;
  align-items: center;
  gap: 4px;
}

.detail-separator {
  color: var(--addp-border-color);
}

/* ========== CatalogEntry操作列 ========== */
.catalogEntry-actions {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}

/* ========== 批量操作栏 ========== */
.catalogEntry-actions-bar {
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
.catalogEntry-actions .el-button {
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

.unscanned-scan-button {
  white-space: nowrap;
}

.catalogEntry-actions {
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

.catalogEntry-table-wrapper {
  width: 100%;
  overflow-x: auto;
}
</style>
