<template>

    <el-card>
      <template #header>
        <div class="card-header">
          <span>{{ t('system.engine.title') }}</span>
          <div class="header-buttons">
            <el-button type="primary" :icon="Plus" @click="showAddStorageDialog">{{ t('system.engine.addStorage') }}</el-button>
            <el-button type="success" :icon="Plus" @click="showAddComputeDialog">{{ t('system.engine.addCompute') }}</el-button>
          </div>
        </div>
      </template>

      <!-- 能力过滤栏 -->
      <div class="filter-bar">
        <span class="filter-label">{{ t('system.engine.filter') }}</span>
        <el-checkbox-group v-model="selectedCategories" @change="handleFilterChange">
          <el-checkbox value="storage">{{ t('system.engine.filterStorage') }}</el-checkbox>
          <el-checkbox value="compute">{{ t('system.engine.filterCompute') }}</el-checkbox>
          <el-checkbox value="general">{{ t('system.engine.filterGeneral') }}</el-checkbox>
          <el-checkbox value="extension">{{ t('system.engine.filterExtension') }}</el-checkbox>
          <el-checkbox value="builtin">{{ t('system.engine.filterBuiltin') }}</el-checkbox>
        </el-checkbox-group>
      </div>

      <el-table :data="filteredEngines" v-loading="loading" stripe :row-class-name="tableRowClassName">
        <!-- ID -->
        <el-table-column prop="id" :label="t('system.engine.columns.id')" width="80" />

        <!-- 名称 -->
        <el-table-column prop="name" :label="t('system.engine.columns.name')" min-width="150" />

        <!-- 类型 -->
        <el-table-column prop="resource_type" :label="t('system.engine.columns.type')" width="150">
          <template #default="{ row }">
            <el-tag :type="getEngineTypeColor(row.engine_type)">
              {{ getEngineTypeLabel(row.engine_type) }}
            </el-tag>
          </template>
        </el-table-column>

        <!-- 连接状态（图标方式） -->
        <el-table-column :label="t('system.engine.columns.connection')" width="100" align="center">
          <template #default="{ row }">
            <el-tooltip
              :content="getConnectionTooltip(row)"
              placement="top"
            >
              <span
                class="connection-status-icon"
                :class="getConnectionStatusClass(row.connection_status)"
              ></span>
            </el-tooltip>
          </template>
        </el-table-column>

        <!-- 激活状态 -->
        <el-table-column :label="t('system.engine.columns.status')" width="100">
          <template #default="{ row }">
            <el-tag :type="row.is_active ? 'success' : 'danger'">
              {{ row.is_active ? t('system.engine.status.active') : t('system.engine.status.disabled') }}
            </el-tag>
          </template>
        </el-table-column>

        <!-- 能力标签 -->
        <el-table-column :label="t('system.engine.columns.capabilities')" min-width="220">
          <template #default="{ row }">
            <el-tag
              v-for="tag in parseCapabilities(row.capabilities)"
              :key="tag"
              size="small"
              effect="plain"
              style="margin: 2px"
            >
              {{ tag }}
            </el-tag>
          </template>
        </el-table-column>

        <!-- 引擎来源 -->
        <el-table-column :label="t('system.engine.columns.category')" width="100">
          <template #default="{ row }">
            <el-tag :type="row.engine_origin === 'general' ? 'success' : 'warning'" size="small">
              {{ row.engine_origin === 'general' ? t('system.engine.category.general') : t('system.engine.category.extension') }}
            </el-tag>
          </template>
        </el-table-column>

        <!-- 注册/内置标识 -->
        <el-table-column :label="t('system.engine.columns.builtin')" width="100">
          <template #default="{ row }">
            <el-tag v-if="row.is_builtin" type="info" size="small" effect="plain">
              {{ t('system.engine.builtin.builtin') }}
            </el-tag>
            <el-tag v-else type="success" size="small" effect="light">
              {{ t('system.engine.builtin.registered') }}
            </el-tag>
          </template>
        </el-table-column>

        <!-- 创建时间 -->
        <el-table-column :label="t('system.engine.columns.createdAt')" width="160">
          <template #default="{ row }">
            {{ formatDate(row.created_at) }}
          </template>
        </el-table-column>

        <!-- 操作列 -->
        <el-table-column :label="t('system.engine.columns.actions')" width="340" fixed="right">
          <template #default="{ row }">
            <el-button size="small" type="primary" @click="testConnection(row)">{{ t('system.engine.actions.test') }}</el-button>
            <el-button size="small" @click="viewEngineDetails(row)">{{ t('system.engine.actions.detail') }}</el-button>
            <el-button
              size="small"
              type="warning"
              :icon="Edit"
              :disabled="row.is_builtin"
              @click="editEngine(row)"
            >
              {{ t('system.engine.actions.edit') }}
            </el-button>
            <el-button
              size="small"
              type="danger"
              :icon="Delete"
              :disabled="row.is_builtin"
              @click="deleteEngine(row)"
            >
              {{ t('system.engine.actions.delete') }}
            </el-button>
          </template>
        </el-table-column>
      </el-table>

      <el-pagination
        v-model:current-page="currentPage"
        :page-size="pageSize"
        :total="total"
        layout="total, prev, pager, next"
        style="margin-top: 20px; justify-content: flex-end"
        @current-change="loadEngines"
      />
    </el-card>

    <!-- 引擎类型选择对话框 -->
    <el-dialog
      v-model="typeSelectionVisible"
      :title="t('system.engine.typeSelection.title')"
      width="500px"
    >
      <div class="engine-type-selection">
        <el-card class="type-card" shadow="hover" @click="confirmEngineType('storage')">
          <div class="type-icon">📦</div>
          <h3>{{ t('system.engine.typeSelection.storage') }}</h3>
          <p>{{ t('system.engine.typeSelection.storageDesc') }}</p>
          <ul>
            <li>PostgreSQL</li>
            <li>MySQL</li>
            <li>MinIO</li>
            <li>S3</li>
          </ul>
        </el-card>

        <el-card class="type-card" shadow="hover" @click="confirmEngineType('compute')">
          <div class="type-icon">🔧</div>
          <h3>{{ t('system.engine.typeSelection.compute') }}</h3>
          <p>{{ t('system.engine.typeSelection.computeDesc') }}</p>
          <ul>
            <li>Spatial Engine</li>
            <li>Workflow Engine</li>
            <li>Data Processing</li>
          </ul>
        </el-card>
      </div>
    </el-dialog>

    <!-- 新增/编辑对话框 -->
    <el-dialog
      v-model="dialogVisible"
      :title="dialogTitle"
      :width="isStorageLayout ? '980px' : '600px'"
      @close="resetForm"
    >
      <!-- 通用存储引擎表单（左侧类型选择 + 右侧连接信息） -->
      <template v-if="isStorageLayout">
        <div class="storage-layout">
          <aside class="engine-type-sidebar">
            <div class="sidebar-title">{{ t('system.engine.registerPanel.title') }}</div>
            <div class="sidebar-subtitle">{{ t('system.engine.registerPanel.subtitle') }}</div>
            <div class="engine-type-list">
              <el-tooltip
                v-for="item in visibleStorageEngineTypeOptions"
                :key="item.value"
                :content="item.desc"
                placement="right"
              >
                <button
                  type="button"
                  class="engine-type-item"
                  :class="{
                    'is-active': form.engine_type === item.value,
                    'is-disabled': isEdit
                  }"
                  :disabled="isEdit"
                  @click="selectStorageEngineType(item.value)"
                >
                  <span class="engine-type-icon">{{ item.icon }}</span>
                  <span class="engine-type-name">{{ item.label }}</span>
                </button>
              </el-tooltip>
            </div>
            <div v-if="isEdit" class="sidebar-hint">
              {{ t('system.engine.registerPanel.editLockedHint') }}
            </div>
          </aside>

          <section class="storage-form-panel">
            <StorageEngineForm
              ref="storageFormRef"
              v-model="form"
              :is-edit="isEdit"
              :show-type-selector="false"
            />
          </section>
        </div>
      </template>

      <!-- 通用存储引擎表单（非双栏场景兜底） -->
      <StorageEngineForm
        v-else-if="!isComputeEngineForm"
        ref="storageFormRef"
        v-model="form"
        :is-edit="isEdit"
      />

      <!-- 计算引擎表单 -->
      <EngineForm
        v-else
        ref="resourceFormRef"
        v-model="form"
        :is-edit="isEdit"
      />

      <template #footer>
        <el-button @click="dialogVisible = false">{{ t('system.engine.actions.cancel') }}</el-button>
        <el-button
          v-if="!isComputeEngineForm"
          type="warning"
          :loading="testing"
          @click="testBeforeCreate"
        >
          {{ t('system.engine.actions.testConnection') }}
        </el-button>
        <el-button type="primary" :loading="submitting" @click="submitForm">{{ t('system.engine.actions.save') }}</el-button>
      </template>
    </el-dialog>

    <!-- 引擎详情弹窗 -->
    <el-dialog
      v-model="detailsVisible"
      :title="t('system.engine.dialog.details', { name: selectedEngine?.name || '' })"
      width="800px"
      destroy-on-close
    >
      <div v-loading="detailsLoading" style="min-height: 300px">
        <el-tabs v-if="selectedEngine" type="border-card">
          <!-- 基本信息标签页 -->
          <el-tab-pane :label="t('system.engine.dialog.detailTabs.basic')">
            <el-descriptions :column="2" border>
              <el-descriptions-item :label="t('system.engine.dialog.basicInfo.id')">{{ selectedEngine.id }}</el-descriptions-item>
              <el-descriptions-item :label="t('system.engine.dialog.basicInfo.name')">{{ selectedEngine.name }}</el-descriptions-item>
              <el-descriptions-item :label="t('system.engine.dialog.basicInfo.engineType')">
                <el-tag :type="getEngineTypeColor(selectedEngine.engine_type)">
                  {{ getEngineTypeLabel(selectedEngine.engine_type) }}
                </el-tag>
              </el-descriptions-item>
              <el-descriptions-item :label="t('system.engine.dialog.basicInfo.category')">
                <el-tag :type="selectedEngine.engine_origin === 'general' ? 'success' : 'warning'">
                  {{ selectedEngine.engine_origin === 'general' ? t('system.engine.dialog.basicInfo.generalEngine') : t('system.engine.dialog.basicInfo.extensionEngine') }}
                </el-tag>
              </el-descriptions-item>
              <el-descriptions-item :label="t('system.engine.dialog.basicInfo.registration')">
                <el-tag v-if="selectedEngine.is_builtin" type="info">{{ t('system.engine.dialog.basicInfo.builtinEngine') }}</el-tag>
                <el-tag v-else type="success">{{ t('system.engine.dialog.basicInfo.userRegistered') }}</el-tag>
              </el-descriptions-item>
              <el-descriptions-item :label="t('system.engine.dialog.basicInfo.status')">
                <el-tag :type="selectedEngine.is_active ? 'success' : 'danger'">
                  {{ selectedEngine.is_active ? t('system.engine.status.active') : t('system.engine.status.disabled') }}
                </el-tag>
              </el-descriptions-item>
              <el-descriptions-item :label="t('system.engine.dialog.basicInfo.createdAt')" :span="2">
                {{ formatDate(selectedEngine.created_at) }}
              </el-descriptions-item>
              <el-descriptions-item :label="t('system.engine.dialog.basicInfo.updatedAt')" :span="2">
                {{ formatDate(selectedEngine.updated_at) }}
              </el-descriptions-item>
              <el-descriptions-item :label="t('system.engine.dialog.basicInfo.description')" :span="2">
                {{ selectedEngine.description || t('system.engine.dialog.basicInfo.none') }}
              </el-descriptions-item>
            </el-descriptions>
          </el-tab-pane>

          <!-- 连接配置标签页 -->
          <el-tab-pane :label="t('system.engine.dialog.detailTabs.connection')" v-if="selectedEngine.connection_info && Object.keys(selectedEngine.connection_info).length > 0">
            <el-descriptions :column="1" border>
              <el-descriptions-item
                v-for="[key, value] in sortedConnectionInfo"
                :key="key"
                :label="key"
              >
                {{ value }}
              </el-descriptions-item>
            </el-descriptions>
          </el-tab-pane>

          <!-- 能力声明标签页 -->
          <el-tab-pane :label="t('system.engine.dialog.detailTabs.capabilities')" v-if="selectedEngine.capabilities">
            <div class="capability-detail">
              <el-descriptions :column="2" border size="small" style="margin-bottom: 20px">
                <el-descriptions-item :label="t('system.engine.dialog.capabilities.schemaVersion')">
                  {{ parsedSelectedCapabilities.schema_version || '-' }}
                </el-descriptions-item>
                <el-descriptions-item :label="t('system.engine.dialog.capabilities.engineFamily')">
                  {{ getCapabilityFamilyLabel(parsedSelectedCapabilities.engine_family) }}
                </el-descriptions-item>
                <el-descriptions-item :label="t('system.engine.dialog.capabilities.engineType')">
                  {{ parsedSelectedCapabilities.engine_type || selectedEngine.engine_type }}
                </el-descriptions-item>
                <el-descriptions-item :label="t('system.engine.dialog.capabilities.sections')">
                  <el-tag
                    v-for="section in getCapabilitySections(parsedSelectedCapabilities)"
                    :key="section"
                    size="small"
                    effect="plain"
                    style="margin: 2px"
                  >
                    {{ section }}
                  </el-tag>
                </el-descriptions-item>
              </el-descriptions>

              <div v-if="hasStorageCapability(parsedSelectedCapabilities)" class="capability-section">
                <div class="capability-section-title">{{ t('system.engine.dialog.capabilities.storageCapabilities') }}</div>
                <el-descriptions :column="2" border size="small">
                  <el-descriptions-item :label="t('system.engine.dialog.capabilities.families')" :span="2">
                    <el-tag
                      v-for="family in parsedSelectedCapabilities.storage.families"
                      :key="family"
                      size="small"
                      style="margin: 2px"
                    >
                      {{ getStorageTypeLabel(family) }}
                    </el-tag>
                  </el-descriptions-item>
                  <el-descriptions-item :label="t('system.engine.dialog.capabilities.catalogModel')" :span="2">
                    {{ getCatalogModelSummary(parsedSelectedCapabilities.storage.catalog_model) }}
                  </el-descriptions-item>
                  <el-descriptions-item :label="t('system.engine.dialog.capabilities.catalog')">
                    {{ formatCapabilityFlags(parsedSelectedCapabilities.storage.catalog) }}
                  </el-descriptions-item>
                  <el-descriptions-item :label="t('system.engine.dialog.capabilities.metadata')">
                    {{ formatCapabilityFlags(parsedSelectedCapabilities.storage.metadata) }}
                  </el-descriptions-item>
                  <el-descriptions-item :label="t('system.engine.dialog.capabilities.store')" :span="2">
                    {{ formatCapabilityFlags(parsedSelectedCapabilities.storage.store) }}
                  </el-descriptions-item>
                  <el-descriptions-item v-if="parsedSelectedCapabilities.storage.semantics?.length" :label="t('system.engine.dialog.capabilities.semantics')" :span="2">
                    {{ joinCapabilityValues(parsedSelectedCapabilities.storage.semantics) }}
                  </el-descriptions-item>
                  <el-descriptions-item v-if="parsedSelectedCapabilities.storage.not_supported?.length" :label="t('system.engine.dialog.capabilities.notSupported')" :span="2">
                    {{ joinCapabilityValues(parsedSelectedCapabilities.storage.not_supported) }}
                  </el-descriptions-item>
                </el-descriptions>
              </div>

              <div v-if="hasComputeCapability(parsedSelectedCapabilities)" class="capability-section">
                <div class="capability-section-title">{{ t('system.engine.dialog.capabilities.computeCapabilities') }}</div>
                <el-table :data="getComputeCapabilityRows(parsedSelectedCapabilities)" border size="small">
                  <el-table-column prop="type" :label="t('system.engine.dialog.capabilities.type')" width="130">
                    <template #default="{ row }">
                      {{ getComputeTypeLabel(row.type) }}
                    </template>
                  </el-table-column>
                  <el-table-column prop="languages" :label="t('system.engine.dialog.capabilities.languages')" min-width="160">
                    <template #default="{ row }">
                      {{ joinCapabilityValues(row.languages) }}
                    </template>
                  </el-table-column>
                  <el-table-column prop="modes" :label="t('system.engine.dialog.capabilities.modes')" min-width="160">
                    <template #default="{ row }">
                      {{ joinCapabilityValues(row.modes) }}
                    </template>
                  </el-table-column>
                  <el-table-column prop="description" :label="t('system.engine.dialog.capabilities.description')" min-width="220" />
                </el-table>
              </div>

              <div v-if="parsedSelectedCapabilities.transfer" class="capability-section">
                <div class="capability-section-title">{{ t('system.engine.dialog.capabilities.transferCapabilities') }}</div>
                <el-descriptions :column="2" border size="small">
                  <el-descriptions-item :label="t('system.engine.dialog.capabilities.transfer')">
                    {{ formatCapabilityFlags(parsedSelectedCapabilities.transfer) }}
                  </el-descriptions-item>
                  <el-descriptions-item :label="t('system.engine.dialog.capabilities.formats')">
                    {{ joinCapabilityValues(parsedSelectedCapabilities.transfer.supported_formats) }}
                  </el-descriptions-item>
                  <el-descriptions-item :label="t('system.engine.dialog.capabilities.connectorTypes')" :span="2">
                    {{ formatKeyValueMap(parsedSelectedCapabilities.transfer.connector_types) }}
                  </el-descriptions-item>
                </el-descriptions>
              </div>

              <div v-if="parsedSelectedCapabilities.preview" class="capability-section">
                <div class="capability-section-title">{{ t('system.engine.dialog.capabilities.previewCapabilities') }}</div>
                <el-descriptions :column="2" border size="small">
                  <el-descriptions-item :label="t('system.engine.dialog.capabilities.preview')">
                    {{ formatCapabilityFlags(parsedSelectedCapabilities.preview) }}
                  </el-descriptions-item>
                  <el-descriptions-item :label="t('system.engine.dialog.capabilities.modes')">
                    {{ joinCapabilityValues(parsedSelectedCapabilities.preview.modes) }}
                  </el-descriptions-item>
                </el-descriptions>
              </div>
            </div>
          </el-tab-pane>

          <!-- 扫描配置标签页 -->
          <el-tab-pane :label="t('system.engine.dialog.detailTabs.scan')" v-if="selectedEngine.scan_config">
            <el-descriptions :column="2" border>
              <el-descriptions-item :label="t('system.engine.dialog.scan.immediateScan')" :span="2">
                <el-tag :type="selectedEngine.scan_config.immediate_scan ? 'success' : 'info'">
                  {{ selectedEngine.scan_config.immediate_scan ? t('system.engine.dialog.scan.yes') : t('system.engine.dialog.scan.no') }}
                </el-tag>
                <span v-if="selectedEngine.scan_config.immediate_scan" style="margin-left: 8px">
                  {{ t('system.engine.dialog.scan.depth', { depth: selectedEngine.scan_config.immediate_depth || 'basic' }) }}
                </span>
              </el-descriptions-item>
              <el-descriptions-item :label="t('system.engine.dialog.scan.scheduledScan')" :span="2">
                <el-tag :type="selectedEngine.scan_config.scheduled_scan ? 'success' : 'info'">
                  {{ selectedEngine.scan_config.scheduled_scan ? t('system.engine.dialog.scan.yes') : t('system.engine.dialog.scan.no') }}
                </el-tag>
              </el-descriptions-item>
              <el-descriptions-item v-if="selectedEngine.scan_config.scheduled_scan" :label="t('system.engine.dialog.scan.scheduleType')">
                {{ selectedEngine.scan_config.schedule_type }}
              </el-descriptions-item>
              <el-descriptions-item v-if="selectedEngine.scan_config.scheduled_scan && selectedEngine.scan_config.cron_expression" :label="t('system.engine.dialog.scan.cronExpression')">
                {{ selectedEngine.scan_config.cron_expression }}
              </el-descriptions-item>
              <el-descriptions-item v-if="selectedEngine.scan_config.scheduled_scan && selectedEngine.scan_config.schedule_time" :label="t('system.engine.dialog.scan.scheduleTime')">
                {{ selectedEngine.scan_config.schedule_time }}
              </el-descriptions-item>
            </el-descriptions>
          </el-tab-pane>
        </el-tabs>
      </div>
      <template #footer>
        <el-button @click="detailsVisible = false">{{ t('system.engine.dialog.close') }}</el-button>
      </template>
    </el-dialog>
</template>

<script setup>
import { ref, onMounted, computed } from 'vue'
import { enginesAPI } from '../api/engines'
import { Plus, Edit, Delete } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { StorageEngineForm, EngineForm } from '@common-ui'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

const engines = ref([])
const loading = ref(false)
const currentPage = ref(1)
const pageSize = ref(10)
const total = ref(0)

// 能力过滤
const selectedCategories = ref(['storage', 'compute', 'general', 'extension', 'builtin']) // 默认显示全部引擎

// 引擎类型选择对话框
const typeSelectionVisible = ref(false)
const selectedEngineCapabilityGroup = ref('')

// 资源表单对话框
const dialogVisible = ref(false)
const storageFormRef = ref(null)
const resourceFormRef = ref(null)
const testing = ref(false)
const submitting = ref(false)
const isEdit = ref(false)
const editId = ref(null)

// 引擎详情弹窗相关
const detailsVisible = ref(false)
const selectedEngine = ref(null)
const detailsLoading = ref(false)

const form = ref({
  engine_type: '',
  name: '',
  description: '',
  is_active: true,
  connection_info: {}
})

const dialogTitle = computed(() => {
  if (isEdit.value) return t('system.engine.dialog.edit')
  if (selectedEngineCapabilityGroup.value === 'storage') return t('system.engine.dialog.addStorage')
  if (selectedEngineCapabilityGroup.value === 'compute') return t('system.engine.dialog.addCompute')
  return t('system.engine.dialog.add')
})

// 是否使用计算引擎表单
const isComputeEngineForm = computed(() => {
  return selectedEngineCapabilityGroup.value === 'compute' ||
         (isEdit.value && form.value.engine_type === 'compute_engine')
})

const isStorageLayout = computed(() => {
  return !isComputeEngineForm.value
})

const storageEngineTypeOptions = computed(() => ([
  {
    value: 'postgresql',
    icon: '🐘',
    label: 'PostgreSQL',
    desc: t('system.engine.registerPanel.types.postgresql')
  },
  {
    value: 'mysql',
    icon: '🐬',
    label: 'MySQL',
    desc: t('system.engine.registerPanel.types.mysql')
  },
  {
    value: 'doris',
    icon: '🟠',
    label: 'Apache Doris',
    desc: t('system.engine.registerPanel.types.doris')
  },
  {
    value: 'clickhouse',
    icon: '⚡',
    label: 'ClickHouse',
    desc: t('system.engine.registerPanel.types.clickhouse')
  },
  {
    value: 'mongodb',
    icon: '🍃',
    label: 'MongoDB',
    desc: t('system.engine.registerPanel.types.mongodb')
  },
  {
    value: 'minio',
    icon: '🪣',
    label: 'MinIO',
    desc: t('system.engine.registerPanel.types.minio')
  },
  {
    value: 'neo4j',
    icon: '🕸️',
    label: 'Neo4j',
    desc: t('system.engine.registerPanel.types.neo4j')
  },
  {
    value: 'nfs',
    icon: '📁',
    label: t('system.engine.typeNfs'),
    desc: t('system.engine.registerPanel.types.nfs')
  },
  {
    value: 'spark',
    icon: '✨',
    label: 'Apache Spark',
    desc: t('system.engine.registerPanel.types.spark')
  }
]))

const visibleStorageEngineTypeOptions = computed(() => {
  if (!isEdit.value) {
    return storageEngineTypeOptions.value
  }

  return storageEngineTypeOptions.value.filter(item => item.value === form.value.engine_type)
})

const parsedSelectedCapabilities = computed(() => {
  return parseCapabilitiesJSON(selectedEngine.value?.capabilities)
})

// 过滤后的引擎列表
const filteredEngines = computed(() => {
  if (selectedCategories.value.length === 0) {
    return []
  }

  return engines.value.filter(engine => {
    const caps = parseCapabilitiesJSON(engine.capabilities)
    const hasStorage = hasStorageCapability(caps)
    const hasCompute = hasComputeCapability(caps)
    const isBuiltin = engine.is_builtin
    const engineOrigin = engine.engine_origin

    const matchesCapability =
      (selectedCategories.value.includes('storage') && hasStorage) ||
      (selectedCategories.value.includes('compute') && hasCompute)

    const matchesEngineOrigin =
      (selectedCategories.value.includes('general') && engineOrigin === 'general') ||
      (selectedCategories.value.includes('extension') && engineOrigin === 'extension')

    const matchesBuiltin = selectedCategories.value.includes('builtin') || !isBuiltin

    const hasCapabilityFilter = selectedCategories.value.includes('storage') || selectedCategories.value.includes('compute')
    const hasOriginFilter = selectedCategories.value.includes('general') || selectedCategories.value.includes('extension')

    let matches = true
    if (hasCapabilityFilter && hasOriginFilter) {
      matches = matchesCapability && matchesEngineOrigin
    } else if (hasCapabilityFilter) {
      matches = matchesCapability
    } else if (hasOriginFilter) {
      matches = matchesEngineOrigin
    } else {
      matches = false
    }

    return matches && matchesBuiltin
  })
})

// 对连接配置字段进行排序显示
const sortedConnectionInfo = computed(() => {
  if (!selectedEngine.value?.connection_info) {
    return []
  }

  const fieldOrder = ['host', 'port', 'database', 'user', 'password', 'sslmode']
  const connectionInfo = selectedEngine.value.connection_info
  const entries = Object.entries(connectionInfo)

  const sorted = entries.sort((a, b) => {
    const [keyA] = a
    const [keyB] = b
    const indexA = fieldOrder.indexOf(keyA)
    const indexB = fieldOrder.indexOf(keyB)

    if (indexA !== -1 && indexB === -1) return -1
    if (indexA === -1 && indexB !== -1) return 1
    if (indexA !== -1 && indexB !== -1) return indexA - indexB
    return keyA.localeCompare(keyB)
  })

  return sorted
})

// 解析 capabilities JSON 为对象
const parseCapabilitiesJSON = (capabilitiesJSON) => {
  try {
    if (typeof capabilitiesJSON === 'object' && capabilitiesJSON !== null) {
      return capabilitiesJSON
    }
    return JSON.parse(capabilitiesJSON || '{}')
  } catch {
    return {}
  }
}

const hasStorageCapability = (caps) => {
  return caps.schema_version === 'engine.capabilities/v1' && Boolean(caps.storage)
}

const hasComputeCapability = (caps) => {
  if (caps.schema_version !== 'engine.capabilities/v1' || !caps.compute) {
    return false
  }

  return Boolean(
    caps.compute.query?.supported ||
    caps.compute.workflow?.supported ||
    caps.compute.script?.supported
  )
}

const joinCapabilityValues = (values) => {
  return Array.isArray(values) && values.length > 0 ? values.join(', ') : '-'
}

const formatBoolean = (value) => {
  if (value === true) return t('system.engine.dialog.capabilities.yes')
  if (value === false) return t('system.engine.dialog.capabilities.no')
  return String(value)
}

const formatCapabilityFlags = (capability) => {
  if (!capability || typeof capability !== 'object') {
    return '-'
  }

  const entries = Object.entries(capability)
    .filter(([, value]) => value !== undefined && value !== null && value !== '' && !Array.isArray(value) && typeof value !== 'object')
    .map(([key, value]) => `${key}: ${formatBoolean(value)}`)

  return entries.length > 0 ? entries.join(', ') : '-'
}

const formatKeyValueMap = (value) => {
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    return '-'
  }

  const entries = Object.entries(value).map(([key, item]) => `${key}: ${item}`)
  return entries.length > 0 ? entries.join(', ') : '-'
}

const getCatalogModelSummary = (model) => {
  if (!model) {
    return '-'
  }

  const levels = Array.isArray(model.levels)
    ? model.levels.map(level => {
      const markers = [
        level.container ? t('system.engine.dialog.capabilities.container') : null,
        level.item ? t('system.engine.dialog.capabilities.item') : null,
        level.optional ? t('system.engine.dialog.capabilities.optional') : null
      ].filter(Boolean)
      return `${level.term}${markers.length ? `(${markers.join('/')})` : ''}`
    })
    : []

  return [
    `${t('system.engine.dialog.capabilities.rootTerm')}: ${model.root_term || '-'}`,
    `${t('system.engine.dialog.capabilities.pathVersion')}: ${model.path_version || '-'}`,
    `${t('system.engine.dialog.capabilities.levels')}: ${levels.length ? levels.join(' -> ') : '-'}`
  ].join('; ')
}

const getCapabilitySections = (caps) => {
  const sections = []
  if (hasStorageCapability(caps)) sections.push(t('system.engine.dialog.capabilities.storageCapabilities'))
  if (hasComputeCapability(caps)) sections.push(t('system.engine.dialog.capabilities.computeCapabilities'))
  if (caps.transfer) sections.push(t('system.engine.dialog.capabilities.transferCapabilities'))
  if (caps.preview) sections.push(t('system.engine.dialog.capabilities.previewCapabilities'))
  if (caps.limits) sections.push(t('system.engine.dialog.capabilities.limits'))
  if (caps.extensions) sections.push(t('system.engine.dialog.capabilities.extensions'))
  return sections.length > 0 ? sections : [t('system.engine.capabilities.none')]
}

const getCapabilityFamilyLabel = (family) => {
  if (!family) {
    return '-'
  }
  if (['tabular', 'object', 'file', 'document', 'graph'].includes(family)) {
    return getStorageTypeLabel(family)
  }
  return getComputeTypeLabel(family)
}

const getComputeCapabilityRows = (caps) => {
  if (!hasComputeCapability(caps)) {
    return []
  }

  const rows = []
  const query = caps.compute.query
  if (query?.supported) {
    rows.push({
      type: 'query',
      languages: query.languages || [],
      modes: [
        query.default_language ? `${t('system.engine.dialog.capabilities.defaultLanguage')}: ${query.default_language}` : null,
        query.read_only ? 'read_only' : null,
        query.supports_explain ? 'explain' : null,
        query.supports_cancel ? 'cancel' : null
      ].filter(Boolean),
      description: joinCapabilityValues(query.result_kinds)
    })
  }

  const workflow = caps.compute.workflow
  if (workflow?.supported) {
    rows.push({
      type: 'workflow',
      languages: [],
      modes: workflow.supported_operator_mode || [],
      description: [
        workflow.runtime_api ? `${t('system.engine.dialog.capabilities.runtimeApi')}: ${workflow.runtime_api}` : null,
        workflow.dynamic_operators ? 'dynamic_operators' : null
      ].filter(Boolean).join(', ') || '-'
    })
  }

  const script = caps.compute.script
  if (script?.supported) {
    rows.push({
      type: 'script',
      languages: script.languages || [],
      modes: script.modes || [],
      description: joinCapabilityValues(script.languages)
    })
  }

  return rows
}

// 解析 capabilities 为标签数组（用于显示）
const parseCapabilities = (capabilitiesJSON) => {
  const caps = parseCapabilitiesJSON(capabilitiesJSON)
  const tags = []

  if (hasStorageCapability(caps)) {
    tags.push(getStorageTypeLabel(caps.engine_family))
  }

  if (hasComputeCapability(caps)) {
    if (caps.compute.query?.supported) {
      tags.push(getComputeTypeLabel('query'))
      ;(caps.compute.query.languages || []).forEach(language => tags.push(language))
    }
    if (caps.compute.workflow?.supported) {
      tags.push(getComputeTypeLabel('workflow'))
    }
    if (caps.compute.script?.supported) {
      tags.push(getComputeTypeLabel('script'))
      ;(caps.compute.script.languages || []).forEach(language => tags.push(language))
    }
  }

  return tags.length > 0 ? [...new Set(tags)] : [t('system.engine.capabilities.none')]
}

const handleFilterChange = () => {}

const engineTypeMap = computed(() => ({
  'postgresql': 'PostgreSQL',
  'mysql': 'MySQL',
  'doris': 'Apache Doris',
  'clickhouse': 'ClickHouse',
  'mongodb': 'MongoDB',
  'minio': 'MinIO',
  'neo4j': 'Neo4j',
  'nfs': t('system.engine.typeNfs'),
  'spark': 'Apache Spark',
  'database': 'Database',
  'compute_engine': 'Compute Engine'
}))

const getEngineTypeLabel = (type) => {
  return engineTypeMap.value[type] || type
}

const getEngineTypeColor = (type) => {
  const colorMap = {
    'postgresql': 'primary',
    'mysql': 'success',
    'doris': 'warning',
    'minio': 'warning',
    'nfs': 'warning',
    'spark': 'danger',
    'database': 'success',
    'compute_engine': 'info'
  }
  return colorMap[type] || 'info'
}

const formatDate = (dateString) => {
  return new Date(dateString).toLocaleString()
}

// 获取连接状态标签
const getConnectionStatusLabel = (status) => {
  const labelMap = {
    'online': t('system.engine.connection.online'),
    'offline': t('system.engine.connection.offline'),
    'unknown': t('system.engine.connection.unknown'),
    'checking': t('system.engine.connection.checking')
  }
  return labelMap[status] || t('system.engine.connection.notChecked')
}

// 获取连接状态图标 CSS class
const getConnectionStatusClass = (status) => {
  const classMap = {
    'online': 'status-online',
    'offline': 'status-offline',
    'unknown': 'status-unknown',
    'checking': 'status-checking'
  }
  return classMap[status] || 'status-unknown'
}

// 获取连接状态提示信息
const getConnectionTooltip = (row) => {
  if (!row.connection_status) return t('system.engine.connection.notChecked')

  let tooltip = t('system.engine.connection.statusLine', { status: getConnectionStatusLabel(row.connection_status) })

  if (row.last_check_at) {
    tooltip += `\n${t('system.engine.connection.lastCheck', { time: formatDate(row.last_check_at) })}`
  }

  if (row.check_message) {
    tooltip += `\n${t('system.engine.connection.detail', { msg: row.check_message })}`
  }

  return tooltip
}

const loadEngines = async () => {
  loading.value = true
  try {
    const response = await enginesAPI.list(currentPage.value, pageSize.value)
    engines.value = response?.data || []
    total.value = response?.total || 0
  } catch (error) {
    ElMessage.error(t('system.engine.msg.loadFailed'))
    console.error(error)
  } finally {
    loading.value = false
  }
}

const selectStorageEngineType = (engineType) => {
  if (isEdit.value) return
  if (form.value.engine_type === engineType) return

  form.value = {
    ...form.value,
    engine_type: engineType
  }
}

const showAddStorageDialog = () => {
  isEdit.value = false
  editId.value = null
  selectedEngineCapabilityGroup.value = 'storage'
  resetForm()
  form.value = {
    ...form.value,
    engine_type: 'postgresql'
  }
  dialogVisible.value = true
}

const showAddComputeDialog = () => {
  isEdit.value = false
  editId.value = null
  selectedEngineCapabilityGroup.value = 'compute'
  resetForm()
  dialogVisible.value = true
}

const confirmEngineType = (category) => {
  selectedEngineCapabilityGroup.value = category
  typeSelectionVisible.value = false
  dialogVisible.value = true
}

const editEngine = (row) => {
  isEdit.value = true
  editId.value = row.id

  if (row.engine_type === 'compute_engine') {
    selectedEngineCapabilityGroup.value = 'compute'

    form.value = {
      unique_identifier: row.unique_identifier || '',
      name: row.name || '',
      display_name: row.display_name || '',
      description: row.description || '',
      engine_type: row.engine_type,
      capabilities: typeof row.capabilities === 'string'
        ? row.capabilities
        : JSON.stringify(row.capabilities || {}, null, 2),
      task_api_config: typeof row.task_api_config === 'string'
        ? row.task_api_config
        : JSON.stringify(row.task_api_config || {}, null, 2),
      health_check_config: typeof row.health_check_config === 'string'
        ? row.health_check_config
        : JSON.stringify(row.health_check_config || {}, null, 2),
      is_active: row.is_active
    }
  } else {
    selectedEngineCapabilityGroup.value = 'storage'

    form.value = {
      engine_type: row.engine_type,
      name: row.name,
      description: row.description,
      is_active: row.is_active,
      connection_info: { ...row.connection_info }
    }
  }

  dialogVisible.value = true
}

const testBeforeCreate = async () => {
  const formRef = isComputeEngineForm.value ? resourceFormRef.value : storageFormRef.value
  const valid = await formRef?.validate()
  if (!valid) {
    ElMessage.warning(t('system.engine.msg.fillRequired'))
    return
  }

  if (isComputeEngineForm.value) {
    ElMessage.info(t('system.engine.msg.computeTestHint'))
    return
  }

  testing.value = true
  try {
    const response = isEdit.value
      ? await enginesAPI.testExistingConnection(editId.value, form.value)
      : await enginesAPI.testConnection(form.value)

    if (response.success) {
      ElMessage.success(t('system.engine.msg.testSuccess'))
    } else {
      ElMessage.error(t('system.engine.msg.testFailed', { error: response.error || response.message }))
    }
  } catch (error) {
    ElMessage.error(t('system.engine.msg.testFailed', { error: error.response?.data?.error || error.message }))
  } finally {
    testing.value = false
    if (isEdit.value) {
      await loadEngines()
    }
  }
}

const testConnection = async (row) => {
  try {
    const response = await enginesAPI.testExistingConnection(row.id)
    if (response.success) {
      ElMessage.success(t('system.engine.msg.testSuccess'))
      await loadEngines()
    } else {
      ElMessage.error(t('system.engine.msg.testFailed', { error: response.error || response.message }))
      await loadEngines()
    }
  } catch (error) {
    ElMessage.error(t('system.engine.msg.testFailed', { error: error.response?.data?.error || error.message }))
    await loadEngines()
  }
}

const submitForm = async () => {
  const formRef = isComputeEngineForm.value ? resourceFormRef.value : storageFormRef.value
  const valid = await formRef?.validate()
  if (!valid) return

  submitting.value = true
  try {
    let submitData = { ...form.value }

    if (isComputeEngineForm.value) {
      try {
        submitData.capabilities = JSON.parse(submitData.capabilities || '{}')
        if (submitData.task_api_config) {
          submitData.task_api_config = JSON.parse(submitData.task_api_config)
        }
        if (submitData.health_check_config) {
          submitData.health_check_config = JSON.parse(submitData.health_check_config)
        }
      } catch (e) {
        ElMessage.error(t('system.engine.msg.jsonError'))
        return
      }
    }

    if (isEdit.value) {
      await enginesAPI.update(editId.value, submitData)
      ElMessage.success(t('system.engine.msg.updateSuccess'))
    } else {
      await enginesAPI.create(submitData)
      ElMessage.success(t('system.engine.msg.createSuccess'))
    }
    dialogVisible.value = false
    loadEngines()
  } catch (error) {
    ElMessage.error(error.response?.data?.error || t('system.engine.msg.opFailed'))
  } finally {
    submitting.value = false
  }
}

const deleteEngine = (row) => {
  if (row.is_builtin) {
    ElMessage.warning(t('system.engine.msg.builtinCannotDelete'))
    return
  }

  ElMessageBox.confirm(
    t('system.engine.msg.deleteConfirm', { name: row.name }),
    t('system.engine.msg.deleteTitle'),
    {
      confirmButtonText: 'OK',
      cancelButtonText: 'Cancel',
      type: 'warning'
    }
  ).then(async () => {
    try {
      await enginesAPI.delete(row.id)
      ElMessage.success(t('system.engine.msg.deleteSuccess'))
      loadEngines()
    } catch (error) {
      const errorMsg = error.response?.data?.error || t('system.engine.msg.opFailed')
      ElMessage.error(errorMsg)
    }
  }).catch(() => {})
}

const viewEngineDetails = async (row) => {
  detailsLoading.value = true
  detailsVisible.value = true
  selectedEngine.value = null

  try {
    const response = await enginesAPI.getById(row.id)
    selectedEngine.value = response
  } catch (error) {
    ElMessage.error(t('system.engine.msg.detailFailed'))
    console.error(error)
    detailsVisible.value = false
  } finally {
    detailsLoading.value = false
  }
}

// 获取存储类型标签
const getStorageTypeLabel = (type) => {
  const typeMap = {
    'tabular': t('system.engine.capabilities.tabular'),
    'object': t('system.engine.capabilities.objectStorage'),
    'file': t('system.engine.capabilities.file'),
    'document': t('system.engine.capabilities.document'),
    'graph': t('system.engine.capabilities.graphDb'),
    'formats': t('system.engine.capabilities.formats')
  }
  return typeMap[type] || type
}

// 获取计算类型标签
const getComputeTypeLabel = (type) => {
  const typeMap = {
    'query': t('system.engine.capabilities.query'),
    'workflow': t('system.engine.capabilities.workflow'),
    'script': t('system.engine.capabilities.script')
  }
  return typeMap[type] || type
}

// 表格行样式
const tableRowClassName = ({ row }) => {
  return row.is_builtin ? 'builtin-engine-row' : ''
}

const resetForm = () => {
  if (isComputeEngineForm.value) {
    form.value = {
      unique_identifier: '',
      name: '',
      display_name: '',
      description: '',
      engine_type: 'compute_engine',
      capabilities: '',
      task_api_config: '',
      health_check_config: '',
      is_active: true
    }
    resourceFormRef.value?.reset()
  } else {
    form.value = {
      engine_type: '',
      name: '',
      description: '',
      is_active: true,
      connection_info: {}
    }
    storageFormRef.value?.reset()
  }
}

onMounted(() => {
  loadEngines()
})
</script>

<style scoped>
.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-weight: 600;
}

.header-buttons {
  display: flex;
  gap: 10px;
}

/* 过滤栏样式 */
.filter-bar {
  display: flex;
  align-items: center;
  padding: 16px 0;
  margin-bottom: 16px;
  border-bottom: 1px solid var(--addp-border-color-light);
}

.filter-label {
  font-weight: 500;
  margin-right: 16px;
  color: var(--addp-text-secondary);
}

/* 通用存储引擎注册双栏布局 */
.storage-layout {
  display: flex;
  gap: 12px;
  align-items: flex-start;
}

.engine-type-sidebar {
  width: 220px;
  flex-shrink: 0;
  border: 1px solid var(--addp-border-color);
  border-radius: 10px;
  background: var(--addp-bg-secondary);
  padding: 12px;
  display: flex;
  flex-direction: column;
}

.sidebar-title {
  font-size: 14px;
  font-weight: 600;
  color: var(--addp-text-primary);
}

.sidebar-subtitle {
  margin-top: 4px;
  margin-bottom: 8px;
  font-size: 12px;
  color: var(--addp-text-secondary);
}

.engine-type-list {
  max-height: 70vh;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding-right: 2px;
}

.engine-type-item {
  width: 100%;
  border: 1px solid var(--addp-border-color);
  border-radius: 8px;
  background: var(--addp-bg-primary);
  color: inherit;
  text-align: center;
  padding: 10px 8px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 6px;
  cursor: pointer;
  transition: border-color 0.2s ease, background-color 0.2s ease, transform 0.2s ease;
}

.engine-type-item:hover {
  border-color: var(--el-color-primary);
  transform: translateY(-1px);
}

.engine-type-item.is-active {
  border-color: var(--el-color-primary);
  background: var(--el-color-primary-light-9);
}

.engine-type-item.is-disabled {
  cursor: not-allowed;
  opacity: 0.85;
}

.engine-type-item.is-disabled:hover {
  transform: none;
}

.engine-type-icon {
  font-size: 40px;
  line-height: 1;
  width: 56px;
  text-align: center;
  flex-shrink: 0;
}

.engine-type-name {
  font-size: 12px;
  font-weight: 600;
  color: var(--addp-text-primary);
  line-height: 1.2;
}

.sidebar-hint {
  margin-top: 10px;
  font-size: 12px;
  color: var(--el-color-warning);
}

.storage-form-panel {
  flex: 1;
  min-width: 0;
}

.capability-detail {
  display: flex;
  flex-direction: column;
  gap: 18px;
}

.capability-section {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.capability-section-title {
  font-size: 14px;
  font-weight: 600;
  color: var(--addp-text-primary);
}

/* 内置引擎行样式 */
:deep(.builtin-engine-row) {
  background-color: var(--addp-bg-secondary);
}

:deep(.builtin-engine-row:hover) {
  background-color: var(--addp-bg-primary) !important;
}

/* 连接状态图标样式 */
.connection-status-icon {
  display: inline-block;
  width: 12px;
  height: 12px;
  border-radius: 50%;
  cursor: help;
  transition: all 0.3s;
}

.connection-status-icon:hover {
  transform: scale(1.2);
}

.status-online {
  background-color: var(--el-color-success);
  box-shadow: 0 0 6px rgba(103, 194, 58, 0.6);
}

.status-offline {
  background-color: var(--el-color-danger);
  box-shadow: 0 0 6px rgba(245, 108, 108, 0.6);
}

.status-unknown {
  background-color: var(--addp-text-tertiary);
  box-shadow: 0 0 6px rgba(144, 147, 153, 0.6);
}

.status-checking {
  background-color: var(--el-color-warning);
  box-shadow: 0 0 6px rgba(230, 162, 60, 0.6);
  animation: pulse 1.5s ease-in-out infinite;
}

@keyframes pulse {
  0%, 100% {
    opacity: 1;
  }
  50% {
    opacity: 0.5;
  }
}

/* 引擎类型选择对话框样式 */
.engine-type-selection {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 20px;
  padding: 20px 0;
}

.type-card {
  cursor: pointer;
  transition: all 0.3s;
  text-align: center;
  padding: 20px;
}

.type-card:hover {
  transform: translateY(-5px);
  border-color: var(--el-color-primary);
}

.type-icon {
  font-size: 48px;
  margin-bottom: 16px;
}

.type-card h3 {
  margin: 16px 0 8px;
  font-size: 18px;
  font-weight: 600;
  color: var(--addp-text-primary);
}

.type-card p {
  margin: 0 0 16px;
  font-size: 14px;
  color: var(--addp-text-secondary);
  line-height: 1.5;
}

.type-card ul {
  list-style: none;
  padding: 0;
  margin: 0;
  font-size: 13px;
  color: var(--addp-text-tertiary);
}

.type-card ul li {
  padding: 4px 0;
}
</style>
