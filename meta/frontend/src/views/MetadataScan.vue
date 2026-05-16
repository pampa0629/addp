<template>
  <div class="metadata-scan">
    <el-card>
      <div class="scan-container" ref="containerRef">
        <!-- 左侧：存储引擎列表 -->
        <div class="left-panel" :style="{ width: leftPanelWidth + 'px' }">
          <div class="panel-header">
            <h3>{{ t('meta.scan.storageEngineList') }}</h3>
            <el-button
              type="primary"
              @click="handleAutoScan"
              :loading="autoScanning"
              class="auto-scan-button"
            >
              <el-icon><Search /></el-icon>
              {{ t('meta.scan.autoScanUnscanned') }}
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

                  <!-- 第三行：catalog 顶层节点统计（tooltip显示） -->
                  <el-tooltip placement="top">
                    <template #content>
                      {{ t('meta.scan.totalCount', { term: getCatalogNodeTerminology(row), n: row.total_catalog_nodes || 0 }) }}<br>
                      {{ t('meta.scan.scannedCount', { term: getCatalogNodeTerminology(row), n: row.scanned_catalog_nodes || 0 }) }}<br>
                      {{ t('meta.scan.unscannedCount', { term: getCatalogNodeTerminology(row), n: row.unscanned_catalog_nodes || 0 }) }}
                    </template>
                    <div class="engine-stats">
                      {{ row.total_catalog_nodes || 0 }}{{ getCatalogNodeTerminology(row) }}
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

        <!-- 右侧：Catalog 顶层节点列表 -->
        <div class="right-panel">
          <div class="panel-header">
            <h3>{{ rightPanelTitle }}</h3>
            <div v-if="selectedResource" class="catalogNode-actions-bar">
              <!-- 选中提示 -->
              <div v-if="selectedCatalogNodes.length" class="selection-info">
                {{ t('meta.scan.selectedCount', { n: selectedCatalogNodes.length, term: getCatalogNodeTerminology(selectedResource) }) }}
              </div>

              <!-- 批量操作按钮 -->
              <el-button
                type="primary"
                size="default"
                @click="handleBatchScan"
                :disabled="!selectedCatalogNodes.length"
                :loading="scanning"
              >
                <el-icon><Search /></el-icon>
                {{ t('meta.scan.batchScan') }}
              </el-button>

              <!-- 刷新按钮 -->
              <el-button
                @click="loadCatalogNodes"
                :loading="loadingCatalogNodes"
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

          <div v-else class="catalogNode-table-wrapper">
            <el-table
              class="catalogNode-table"
              :data="catalogNodes"
              v-loading="loadingCatalogNodes"
              height="600"
              @selection-change="handleCatalogNodeSelectionChange"
              style="min-width: 720px"
            >
              <el-table-column type="selection" width="55" />
              <el-table-column :label="catalogNodeColumnLabel" width="250">
                <template #default="{ row }">
                  <div class="catalogNode-info">
                    <!-- 第一行：名称 + 状态标签 + 调度图标 -->
                    <div class="catalogNode-header">
                      <span class="catalogNode-name">{{ row.name }}</span>
                      <el-tag
                        size="small"
                        :type="row.scan_status === 'completed' ? 'success' : row.scan_status === 'running' ? 'warning' : 'info'"
                      >
                        {{ t(`meta.status.${row.scan_status}`) || row.scan_status }}
                      </el-tag>

                      <!-- 调度状态图标 -->
                      <el-tooltip
                        v-if="getCatalogNodePlan(row)"
                        :content="t('meta.scan.independentScheduleTooltip', { desc: getCatalogNodePlan(row).description, next: getCatalogNodePlan(row).nextRun })"
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
                    <div class="catalogNode-details">
                      <span v-if="row.table_count !== undefined">
                        <el-icon :size="12"><Document /></el-icon>
                        {{ row.table_count }}{{ t('meta.scan.tables') }}
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
                  <div class="catalogNode-actions">
                    <el-button
                      type="primary"
                      size="default"
                      @click.stop="handleScanCatalogNode(row)"
                      :loading="scanningCatalogNodes[row.id ?? catalogNodeNameOf(row)]"
                    >
                      <el-icon><Search /></el-icon>
                      {{ row.scan_status === 'completed' ? t('meta.scan.rescan') : t('meta.scan.scan') }}
                    </el-button>
                    <el-button
                      type="success"
                      size="default"
                      plain
                      @click.stop="handleCatalogNodeSchedule(row)"
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

    <!-- 扫描进度对话框 -->
    <el-dialog v-model="showScanDialog" :title="t('meta.scan.scanProgressTitle')" width="500px" :close-on-click-modal="false">
      <div v-if="scanning">
        <el-progress :percentage="scanProgress" :status="scanProgress === 100 ? 'success' : undefined" />
        <p style="margin-top: 20px; text-align: center; color: #999">{{ scanMessage }}</p>
      </div>
      <div v-else-if="scanResult">
        <el-result
          :icon="scanResult.status === 'success' ? 'success' : 'error'"
          :title="scanResult.status === 'success' ? t('meta.scan.scanComplete') : t('meta.scan.scanFailed')"
        >
          <template #sub-title>
            <div>{{ t('meta.scan.scannedCatalogNodes', { n: scanResult.catalog_nodes_scanned }) }}</div>
            <div>{{ t('meta.scan.foundItems', { n: scanResult.items_scanned }) }}</div>
            <div>{{ t('meta.scan.scannedFields', { n: scanResult.fields_scanned }) }}</div>
            <div>{{ t('meta.scan.duration', { n: scanResult.duration_ms }) }}</div>
          </template>
        </el-result>
      </div>
      <template #footer>
        <el-button @click="closeScanDialog">{{ t('meta.scan.close') }}</el-button>
      </template>
    </el-dialog>

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
        {{ t('meta.scan.engineSchemaCount', { n: inheritanceInfo.total, term: getCatalogNodeTerminology(selectedResource) }) }}
        <ul style="margin: 8px 0 0 20px">
          <li>{{ inheritanceInfo.independent }}{{ t('meta.scan.withIndependentSchedule') }}</li>
          <li>{{ inheritanceInfo.inherited }}{{ t('meta.scan.willInheritSchedule') }}</li>
        </ul>
        <div style="margin-top: 8px; color: var(--addp-text-tertiary)">
          {{ t('meta.scan.engineScheduleNote', { term: getCatalogNodeTerminology(selectedResource) }) }}
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

    <!-- 命名空间调度设置对话框 -->
    <el-dialog
      v-model="catalogNodeScheduleDialogVisible"
      :title="`${currentCatalogNode?.name || ''}${t('meta.scan.schemaScheduleTitle')}`"
      width="600px"
    >
      <!-- 继承说明 -->
      <el-alert
        v-if="hasEngineSchedule && !currentCatalogNodeTask"
        type="info"
        :closable="false"
        style="margin-bottom: 16px"
      >
        {{ t('meta.scan.inheritEngineSchedule', { desc: engineScheduleDesc }) }}
        <br />{{ t('meta.scan.independentScheduleNote') }}
      </el-alert>

      <!-- 调度配置 -->
      <ScheduleConfig v-model="catalogNodeScheduleCron" />

      <el-form label-width="100px" style="margin-top: 20px">
        <el-form-item :label="t('meta.scan.scanDepth')">
          <el-radio-group v-model="catalogNodeScheduleDepth">
            <el-radio value="basic">{{ t('meta.scan.basicScan') }}</el-radio>
            <el-radio value="deep">{{ t('meta.scan.deepScan') }}</el-radio>
          </el-radio-group>
        </el-form-item>

        <el-form-item :label="t('meta.scan.enableSchedule')">
          <el-switch v-model="catalogNodeScheduleEnabled" />
        </el-form-item>
      </el-form>

      <template #footer>
        <el-button @click="catalogNodeScheduleDialogVisible = false">{{ t('meta.scan.cancel') }}</el-button>
        <el-button
          type="primary"
          @click="submitCatalogNodeSchedule"
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

// 引擎列表
const engines = ref([])
const resourceTableRef = ref(null)
const loadingResources = ref(false)
const selectedResource = ref(null)
const containerRef = ref(null)

// 命名空间 / Catalog 顶层节点列表
const catalogNodes = ref([])
const loadingCatalogNodes = ref(false)
const selectedCatalogNodes = ref([])

// 扫描状态
const autoScanning = ref(false)
const scanning = ref(false)
const scanningCatalogNodes = reactive({})
const showScanDialog = ref(false)
const scanProgress = ref(0)
const scanMessage = ref('')
const scanResult = ref(null)

const allScanTasks = ref([])
const scheduleDialogVisible = ref(false)
const savingSchedule = ref(false)
const scheduleCron = ref('') // Cron 表达式
const scheduleEnabled = ref(true) // 是否启用

// 命名空间调度相关
const catalogNodeScheduleDialogVisible = ref(false)
const currentCatalogNode = ref(null)
const currentCatalogNodeTask = ref(null)
const catalogNodeScheduleCron = ref('')
const catalogNodeScheduleDepth = ref('deep')
const catalogNodeScheduleEnabled = ref(true)

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

// 命名空间调度相关computed
const catalogNodeNameOf = (catalogNode) => catalogNode?.name || ''

const catalogNodeTargetOf = (catalogNode) => {
  if (!catalogNode) return ''
  if (usesCatalogPathTargets(selectedResource.value)) {
    return catalogNode.path || catalogNode.name || ''
  }
  return catalogNodeNameOf(catalogNode)
}

const getCatalogNodePlan = (catalogNode) => {
  const catalogNodeName = catalogNodeNameOf(catalogNode)
  const task = allScanTasks.value.find(task => {
    if (task.engine_id !== selectedResource.value.id) return false
    const params = task.parameters || {}
    const paths = params.catalog_paths || []
    // 精确匹配：该任务只扫描这一个命名空间或 bucket/path
    const target = catalogNodeTargetOf(catalogNode) || catalogNodeName
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
  if (!selectedResource.value || !catalogNodes.value.length) return null

  const allSchemas = catalogNodes.value.length
  const withOwnSchedule = catalogNodes.value.filter(s => getCatalogNodePlan(s)).length
  const inheritedCount = allSchemas - withOwnSchedule

  return {
    total: allSchemas,
    independent: withOwnSchedule,
    inherited: inheritedCount,
    hasEngineSchedule: hasEngineSchedule.value
  }
})

// 计算右侧面板标题（根据引擎类型显示命名空间、Collection、Bucket 或目录）
const rightPanelTitle = computed(() => {
  if (!selectedResource.value) return t('meta.scan.catalogNodeList')
  const terminology = getCatalogNodeTerminology(selectedResource.value)
  return `${terminology}${t('meta.scan.catalogListSuffix')} - ${selectedResource.value.name}`
})

// 计算表格列标题（根据引擎类型显示命名空间、Collection、Bucket 或目录信息）
const catalogNodeColumnLabel = computed(() => {
  if (!selectedResource.value) return t('meta.scan.catalogNodeInfo')
  const terminology = getCatalogNodeTerminology(selectedResource.value)
  return `${terminology}${t('meta.scan.namespaceInfoSuffix')}`
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
      await Promise.all([loadCatalogNodes(), loadScanTasks()])
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
  await loadCatalogNodes()
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

// 判断扫描目标是否使用 catalog_paths。依据 catalog item 术语，不列举具体引擎 type。
const usesCatalogPathTargets = (resource) => {
  const itemTerm = String(resource?.catalog_item_term || '').toLowerCase()
  return itemTerm === 'object' || itemTerm === 'file'
}

// 判断是否为 NoSQL 数据库类型
const isNoSQLType = (resourceType) => {
  if (!resourceType) return false
  const type = resourceType.toLowerCase()
  return ['mongodb'].includes(type)
}

// 获取引擎原生的顶层 catalog 术语，避免把内部 catalog node 抽象暴露给用户。
const getCatalogNodeTerminology = (resource, plural = false) => {
  if (!resource) return t('meta.scan.defaultNamespaceTerm')
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
  const itemTerm = String(resource.catalog_item_term || '').toLowerCase()
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
  return t('meta.scan.defaultNamespaceTerm')
}

// 加载命名空间 / Catalog 顶层节点列表
const loadCatalogNodes = async () => {
  if (!selectedResource.value) return

  loadingCatalogNodes.value = true
  let availableSchemas = []
  let connectionError = null

  try {
    // 检查引擎连接状态，如果已知离线，直接跳过实际连接
    if (selectedResource.value.connection_status === 'offline') {
      connectionError = new Error(`引擎离线: ${selectedResource.value.check_message || '连接失败'}`)
      console.warn('资源已标记为离线，跳过实际连接:', selectedResource.value.name)
    } else {
      // 引擎在线或状态未知，统一通过 System 实时 catalog API 获取顶层节点
      try {
        const availableRes = await metaApi.listCatalogTopNodes(selectedResource.value.id)
        availableSchemas = Array.isArray(availableRes) ? availableRes : []
      } catch (error) {
        // 捕获连接错误，但不阻止后续加载
        connectionError = error
        console.warn('获取可用命名空间/Bucket失败（可能存储引擎离线）:', error.response?.data?.error || error.message)
      }
    }
  } catch (error) {
    // 不应该到这里，但保险起见
    connectionError = error
  }

  try {
    // 再获取已扫描的 catalog 顶层节点状态信息
    const scannedRes = await metaApi.getScannedCatalogTopNodes(selectedResource.value.id)
    const scannedSchemas = Array.isArray(scannedRes) ? scannedRes : []

    if (connectionError && scannedSchemas.length === 0) {
      // 如果连接失败且没有已扫描的节点，显示空列表
      // 用户已经能从左侧图标看到引擎离线状态，无需重复提示
      catalogNodes.value = []
    } else if (connectionError) {
      // 连接失败但有历史扫描数据，使用历史数据并标记状态
      catalogNodes.value = scannedSchemas.map(scanned => ({
        id: scanned.id,
        name: catalogNodeNameOf(scanned),
        scan_status: t('meta.scan.connectionFailed', { status: scanned.scan_status }),
        table_count: scanned.table_count || 0,
        scanned_at: scanned.scanned_at || '',
        total_size_bytes: scanned.total_size_bytes || 0
      }))
      // 已通过左侧连接状态图标显示，无需额外提示
    } else {
      // 正常情况：合并两个列表
      catalogNodes.value = availableSchemas.map(available => {
        const availableName = catalogNodeNameOf(available)
        const scanned = scannedSchemas.find(s => catalogNodeNameOf(s) === availableName)
        return {
          ...available,
          name: availableName,
          id: scanned?.id,
          scan_status: scanned?.scan_status || 'pending',
          table_count: scanned?.table_count || 0,
          scanned_at: scanned?.scanned_at || '',
          total_size_bytes: scanned?.total_size_bytes || 0
        }
      })
    }
  } catch (error) {
    ElMessage.error(t('meta.scan.loadCatalogNodesFailed', { msg: error.response?.data?.error || error.message }))
    catalogNodes.value = []
  } finally {
    loadingCatalogNodes.value = false
  }
}

// 命名空间选择变化
const handleCatalogNodeSelectionChange = (selection) => {
  selectedCatalogNodes.value = selection
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

const deriveAutoTaskCatalogPaths = () => {
  if (!Array.isArray(catalogNodes.value) || !catalogNodes.value.length) return []
  return catalogNodes.value
    .map(item => catalogNodeTargetOf(item))
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

// 一键自动扫描
const handleAutoScan = async () => {
  try {
    await ElMessageBox.confirm(
      t('meta.scan.autoScanConfirmMsg'),
      t('meta.scan.autoScanConfirmTitle'),
      { type: 'warning' }
    )

    autoScanning.value = true
    showScanDialog.value = true
    scanProgress.value = 0
    scanMessage.value = t('meta.scan.scanningMsg')
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
    ElMessage.success(t('meta.scan.autoScanComplete'))

    // 刷新引擎列表
    await loadEngines()
    if (selectedResource.value) {
      await loadCatalogNodes()
    }
  } catch (error) {
    if (error !== 'cancel') {
      ElMessage.error(t('meta.scan.autoScanFailed', { msg: error.response?.data?.error || error.message }))
    }
  } finally {
    autoScanning.value = false
  }
}

// 批量扫描命名空间或 catalog 路径
const handleBatchScan = async () => {
  if (!selectedCatalogNodes.value.length) return

  const terminology = getCatalogNodeTerminology(selectedResource.value)

  try {
    await ElMessageBox.confirm(
      t('meta.scan.batchScanConfirmMsg', { n: selectedCatalogNodes.value.length, term: terminology }),
      t('meta.scan.batchScanConfirmTitle'),
      { type: 'warning' }
    )

    scanning.value = true
    showScanDialog.value = true
    scanProgress.value = 0
    scanMessage.value = t('meta.scan.scanningMsg')
    scanResult.value = null

    const catalogPaths = selectedCatalogNodes.value.map(item => catalogNodeTargetOf(item)).filter(Boolean)

    // 模拟进度
    const progressInterval = setInterval(() => {
      if (scanProgress.value < 90) {
        scanProgress.value += 10
      }
    }, 500)

    const res = await metaApi.scanEngine(selectedResource.value.id, catalogPaths, { scan_depth: 'deep', force: false })
    clearInterval(progressInterval)
    scanProgress.value = 100

    scanResult.value = res
    ElMessage.success(t('meta.scan.batchScanComplete'))

    // 刷新命名空间列表
    await loadCatalogNodes()
    await loadEngines()
  } catch (error) {
    if (error !== 'cancel') {
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
      catalog_paths: existingCatalogPaths.length ? existingCatalogPaths : deriveAutoTaskCatalogPaths(),
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

// 扫描单个命名空间或 catalog 路径
const handleScanCatalogNode = async (catalogNode) => {
  const catalogNodeName = catalogNodeNameOf(catalogNode)
  const key = catalogNode.id ?? catalogNodeName
  scanningCatalogNodes[key] = true

  try {
    const target = catalogNodeTargetOf(catalogNode) || catalogNodeName
    await metaApi.scanEngine(selectedResource.value.id, [target], { scan_depth: 'deep', force: false })
    ElMessage.success(t('meta.scan.schemaScanComplete', { name: catalogNodeName }))

    // 刷新列表
    await loadCatalogNodes()
    await loadEngines()
  } catch (error) {
    ElMessage.error(t('meta.scan.scanError', { msg: error.response?.data?.error || error.message }))
  } finally {
    scanningCatalogNodes[key] = false
  }
}

// 命名空间调度相关方法
const handleCatalogNodeSchedule = async (catalogNode) => {
  currentCatalogNode.value = catalogNode
  const catalogNodeName = catalogNodeNameOf(catalogNode)
  // 查找该命名空间或 catalog 路径的调度任务
  currentCatalogNodeTask.value = allScanTasks.value.find(task => {
    if (task.engine_id !== selectedResource.value.id) return false
    const params = task.parameters || {}
    const paths = params.catalog_paths || []
    const target = catalogNodeTargetOf(catalogNode) || catalogNodeName
    return paths.length === 1 && paths[0] === target
  })

  // 预填表单
  if (currentCatalogNodeTask.value) {
    catalogNodeScheduleCron.value = currentCatalogNodeTask.value.schedule || ''
    catalogNodeScheduleDepth.value = currentCatalogNodeTask.value.parameters?.scan_depth || 'deep'
    catalogNodeScheduleEnabled.value = currentCatalogNodeTask.value.enabled
  } else {
    // 默认继承引擎设置或使用默认值
    catalogNodeScheduleCron.value = autoScheduleTask.value?.schedule || '0 2 * * *'
    catalogNodeScheduleDepth.value = 'deep'
    catalogNodeScheduleEnabled.value = true
  }

  catalogNodeScheduleDialogVisible.value = true
}

const submitCatalogNodeSchedule = async () => {
  if (!currentCatalogNode.value) return

  const catalogNodeName = catalogNodeNameOf(currentCatalogNode.value)
  const catalogPath = catalogNodeTargetOf(currentCatalogNode.value) || catalogNodeName

  savingSchedule.value = true
  try {
    const terminology = getCatalogNodeTerminology(selectedResource.value)
    const payload = {
      name: `${selectedResource.value.name} - ${catalogNodeName}`,
      description: `${terminology} ${catalogNodeName} 的定时扫描`,
      catalog_paths: [catalogPath],
      scan_depth: catalogNodeScheduleDepth.value,
      force: false,
      schedule_type: 'cron',
      schedule: catalogNodeScheduleCron.value,
      enabled: catalogNodeScheduleEnabled.value
    }

    if (currentCatalogNodeTask.value) {
      // 更新现有任务
      await metaApi.updateScanTask(
        selectedResource.value.id,
        currentCatalogNodeTask.value.id,
        payload
      )
      ElMessage.success(t('meta.scan.scheduleUpdated'))
    } else {
      // 创建新任务
      await metaApi.createScanTask(selectedResource.value.id, payload)
      ElMessage.success(t('meta.scan.scheduleCreated'))
    }

    catalogNodeScheduleDialogVisible.value = false
    await loadScanTasks()
  } catch (error) {
    ElMessage.error(t('meta.scan.saveFailed', { msg: error.response?.data?.error || error.message }))
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
  selectedCatalogNodes.value = []
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

/* ========== CatalogNode信息列 ========== */
.catalogNode-info {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 4px 0;
}

.catalogNode-header {
  display: flex;
  align-items: center;
  gap: 8px;
}

.catalogNode-name {
  font-weight: 500;
  color: var(--addp-text-primary);
  font-size: 14px;
}

.schedule-icon {
  cursor: help;
  flex-shrink: 0;
}

.catalogNode-details {
  font-size: 12px;
  color: var(--addp-text-tertiary);
  display: flex;
  align-items: center;
  gap: 4px;
}

.detail-separator {
  color: var(--addp-border-color);
}

/* ========== CatalogNode操作列 ========== */
.catalogNode-actions {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}

/* ========== 批量操作栏 ========== */
.catalogNode-actions-bar {
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
.catalogNode-actions .el-button {
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

.catalogNode-actions {
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

.catalogNode-table-wrapper {
  width: 100%;
  overflow-x: auto;
}
</style>
