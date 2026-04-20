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
          <el-checkbox value="standard">{{ t('system.engine.filterStandard') }}</el-checkbox>
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

        <!-- 引擎分类 -->
        <el-table-column :label="t('system.engine.columns.category')" width="100">
          <template #default="{ row }">
            <el-tag :type="row.engine_category === 'standard' ? 'success' : 'warning'" size="small">
              {{ row.engine_category === 'standard' ? t('system.engine.category.standard') : t('system.engine.category.extension') }}
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
      width="600px"
      @close="resetForm"
    >
      <!-- 存储引擎表单 -->
      <StorageEngineForm
        v-if="!isComputeEngineForm"
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
                <el-tag :type="selectedEngine.engine_category === 'standard' ? 'success' : 'warning'">
                  {{ selectedEngine.engine_category === 'standard' ? t('system.engine.dialog.basicInfo.standardEngine') : t('system.engine.dialog.basicInfo.extensionEngine') }}
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
            <div v-if="parseCapabilitiesJSON(selectedEngine.capabilities).storage?.length > 0" style="margin-bottom: 20px">
              <div style="font-weight: 500; margin-bottom: 12px; color: var(--addp-text-primary); font-size: 14px">{{ t('system.engine.dialog.capabilities.storageCapabilities') }}</div>
              <el-table :data="parseCapabilitiesJSON(selectedEngine.capabilities).storage" border size="small">
                <el-table-column prop="type" :label="t('system.engine.dialog.capabilities.type')" width="150">
                  <template #default="{ row }">
                    {{ getStorageTypeLabel(row.type) }}
                  </template>
                </el-table-column>
                <el-table-column prop="engine" :label="t('system.engine.dialog.capabilities.engine')" />
              </el-table>
            </div>
            <div v-if="parseCapabilitiesJSON(selectedEngine.capabilities).compute?.length > 0">
              <div style="font-weight: 500; margin-bottom: 12px; color: var(--addp-text-primary); font-size: 14px">{{ t('system.engine.dialog.capabilities.computeCapabilities') }}</div>
              <el-table :data="parseCapabilitiesJSON(selectedEngine.capabilities).compute" border size="small">
                <el-table-column prop="type" :label="t('system.engine.dialog.capabilities.type')" width="150">
                  <template #default="{ row }">
                    {{ getComputeTypeLabel(row.type) }}
                  </template>
                </el-table-column>
                <el-table-column prop="dev_modes" :label="t('system.engine.dialog.capabilities.devModes')" width="150">
                  <template #default="{ row }">
                    <el-tag v-for="mode in row.dev_modes" :key="mode" size="small" style="margin: 2px">
                      {{ mode }}
                    </el-tag>
                  </template>
                </el-table-column>
                <el-table-column prop="description" :label="t('system.engine.dialog.capabilities.description')" />
              </el-table>
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
const selectedCategories = ref(['storage', 'compute', 'standard', 'extension']) // 默认选中所有，不选中内置

// 引擎类型选择对话框
const typeSelectionVisible = ref(false)
const selectedEngineCategory = ref('')

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
  if (selectedEngineCategory.value === 'storage') return t('system.engine.dialog.addStorage')
  if (selectedEngineCategory.value === 'compute') return t('system.engine.dialog.addCompute')
  return t('system.engine.dialog.add')
})

// 是否使用计算引擎表单
const isComputeEngineForm = computed(() => {
  return selectedEngineCategory.value === 'compute' ||
         (isEdit.value && form.value.engine_type === 'compute_engine')
})

// 过滤后的引擎列表
const filteredEngines = computed(() => {
  if (selectedCategories.value.length === 0) {
    return []
  }

  return engines.value.filter(engine => {
    const caps = parseCapabilitiesJSON(engine.capabilities)
    const hasStorage = caps.storage?.length > 0
    const hasCompute = caps.compute?.length > 0
    const isBuiltin = engine.is_builtin
    const engineCategory = engine.engine_category

    const matchesCapability =
      (selectedCategories.value.includes('storage') && hasStorage) ||
      (selectedCategories.value.includes('compute') && hasCompute)

    const matchesEngineCategory =
      (selectedCategories.value.includes('standard') && engineCategory === 'standard') ||
      (selectedCategories.value.includes('extension') && engineCategory === 'extension')

    const matchesBuiltin = selectedCategories.value.includes('builtin') || !isBuiltin

    const hasCapabilityFilter = selectedCategories.value.includes('storage') || selectedCategories.value.includes('compute')
    const hasCategoryFilter = selectedCategories.value.includes('standard') || selectedCategories.value.includes('extension')

    let matches = true
    if (hasCapabilityFilter && hasCategoryFilter) {
      matches = matchesCapability && matchesEngineCategory
    } else if (hasCapabilityFilter) {
      matches = matchesCapability
    } else if (hasCategoryFilter) {
      matches = matchesEngineCategory
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
    return JSON.parse(capabilitiesJSON || '{}')
  } catch {
    return {}
  }
}

// 解析 capabilities 为标签数组（用于显示）
const parseCapabilities = (capabilitiesJSON) => {
  const caps = parseCapabilitiesJSON(capabilitiesJSON)
  const tags = []

  if (caps.storage) {
    caps.storage.forEach(s => {
      if (s.type) {
        tags.push(getStorageTypeLabel(s.type))
      }
      if (s.engine) tags.push(s.engine)
    })
  }

  if (caps.compute) {
    caps.compute.forEach(c => {
      if (c.type) {
        tags.push(getComputeTypeLabel(c.type))
      }
      if (c.description) tags.push(c.description)
      if (c.category) tags.push(c.category)
    })
  }

  return tags.length > 0 ? tags : [t('system.engine.capabilities.none')]
}

const handleFilterChange = () => {}

const engineTypeMap = {
  'postgresql': 'PostgreSQL',
  'mysql': 'MySQL',
  'doris': 'Apache Doris',
  'clickhouse': 'ClickHouse',
  'mongodb': 'MongoDB',
  'minio': 'MinIO',
  'neo4j': 'Neo4j',
  'nfs': 'NFS 文件系统',
  'spark': 'Apache Spark',
  'spatialite': 'SpatiaLite/SQLite',
  'database': 'Database',
  'compute_engine': 'Compute Engine'
}

const getEngineTypeLabel = (type) => {
  return engineTypeMap[type] || type
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

const showAddStorageDialog = () => {
  isEdit.value = false
  editId.value = null
  selectedEngineCategory.value = 'storage'
  resetForm()
  dialogVisible.value = true
}

const showAddComputeDialog = () => {
  isEdit.value = false
  editId.value = null
  selectedEngineCategory.value = 'compute'
  resetForm()
  dialogVisible.value = true
}

const confirmEngineType = (category) => {
  selectedEngineCategory.value = category
  typeSelectionVisible.value = false
  dialogVisible.value = true
}

const editEngine = (row) => {
  isEdit.value = true
  editId.value = row.id

  if (row.engine_type === 'compute_engine') {
    selectedEngineCategory.value = 'compute'

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
    selectedEngineCategory.value = 'storage'

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
    const response = await enginesAPI.testConnection(form.value)
    if (response.success) {
      ElMessage.success(t('system.engine.msg.testSuccess'))
    } else {
      ElMessage.error(t('system.engine.msg.testFailed', { error: response.error || response.message }))
    }
  } catch (error) {
    ElMessage.error(t('system.engine.msg.testFailed', { error: error.response?.data?.error || error.message }))
  } finally {
    testing.value = false
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
    'relational_db': t('system.engine.capabilities.relationalDb'),
    'object_storage': t('system.engine.capabilities.objectStorage'),
    'graph_db': t('system.engine.capabilities.graphDb')
  }
  return typeMap[type] || type
}

// 获取计算类型标签
const getComputeTypeLabel = (type) => {
  const typeMap = {
    'sql_query': t('system.engine.capabilities.sqlQuery'),
    'spatial': t('system.engine.capabilities.spatial'),
    'tile_cache': t('system.engine.capabilities.tileCache'),
    'scan': t('system.engine.capabilities.scan'),
    'workflow': t('system.engine.capabilities.workflow')
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
  border-bottom: 1px solid #ebeef5;
}

.filter-label {
  font-weight: 500;
  margin-right: 16px;
  color: var(--addp-text-secondary);
}

/* 内置引擎行样式 */
:deep(.builtin-engine-row) {
  background-color: var(--addp-bg-secondary);
}

:deep(.builtin-engine-row:hover) {
  background-color: #ebeef5 !important;
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
