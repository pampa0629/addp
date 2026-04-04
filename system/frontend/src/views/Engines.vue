<template>

    <el-card>
      <template #header>
        <div class="card-header">
          <span>引擎管理</span>
          <div class="header-buttons">
            <el-button type="primary" :icon="Plus" @click="showAddStorageDialog">注册标准引擎</el-button>
            <el-button type="success" :icon="Plus" @click="showAddComputeDialog">注册扩展引擎</el-button>
          </div>
        </div>
      </template>

      <!-- 能力过滤栏 -->
      <div class="filter-bar">
        <span class="filter-label">过滤:</span>
        <el-checkbox-group v-model="selectedCategories" @change="handleFilterChange">
          <el-checkbox value="storage">存储</el-checkbox>
          <el-checkbox value="compute">计算</el-checkbox>
          <el-checkbox value="standard">标准引擎</el-checkbox>
          <el-checkbox value="extension">扩展引擎</el-checkbox>
          <el-checkbox value="builtin">内置</el-checkbox>
        </el-checkbox-group>
      </div>

      <el-table :data="filteredEngines" v-loading="loading" stripe :row-class-name="tableRowClassName">
        <!-- ID -->
        <el-table-column prop="id" label="ID" width="80" />

        <!-- 名称 -->
        <el-table-column prop="name" label="名称" min-width="150" />

        <!-- 类型 -->
        <el-table-column prop="resource_type" label="类型" width="150">
          <template #default="{ row }">
            <el-tag :type="getEngineTypeColor(row.engine_type)">
              {{ getEngineTypeLabel(row.engine_type) }}
            </el-tag>
          </template>
        </el-table-column>

        <!-- 连接状态（图标方式） -->
        <el-table-column label="连接" width="100" align="center">
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
        <el-table-column label="状态" width="100">
          <template #default="{ row }">
            <el-tag :type="row.is_active ? 'success' : 'danger'">
              {{ row.is_active ? '激活' : '禁用' }}
            </el-tag>
          </template>
        </el-table-column>

        <!-- 能力标签 -->
        <el-table-column label="能力" min-width="220">
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
        <el-table-column label="分类" width="100">
          <template #default="{ row }">
            <el-tag :type="row.engine_category === 'standard' ? 'success' : 'warning'" size="small">
              {{ row.engine_category === 'standard' ? '标准' : '扩展' }}
            </el-tag>
          </template>
        </el-table-column>

        <!-- 注册/内置标识 -->
        <el-table-column label="注册/内置" width="100">
          <template #default="{ row }">
            <el-tag v-if="row.is_builtin" type="info" size="small" effect="plain">
              内置
            </el-tag>
            <el-tag v-else type="success" size="small" effect="light">
              注册
            </el-tag>
          </template>
        </el-table-column>

        <!-- 创建时间 -->
        <el-table-column label="创建时间" width="160">
          <template #default="{ row }">
            {{ formatDate(row.created_at) }}
          </template>
        </el-table-column>

        <!-- 操作列 -->
        <el-table-column label="操作" width="340" fixed="right">
          <template #default="{ row }">
            <el-button size="small" type="primary" @click="testConnection(row)">测试</el-button>
            <el-button size="small" @click="viewEngineDetails(row)">详情</el-button>
            <el-button
              size="small"
              type="warning"
              :icon="Edit"
              :disabled="row.is_builtin"
              @click="editEngine(row)"
            >
              编辑
            </el-button>
            <el-button
              size="small"
              type="danger"
              :icon="Delete"
              :disabled="row.is_builtin"
              @click="deleteEngine(row)"
            >
              删除
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
      title="选择引擎类型"
      width="500px"
    >
      <div class="engine-type-selection">
        <el-card class="type-card" shadow="hover" @click="confirmEngineType('storage')">
          <div class="type-icon">📦</div>
          <h3>存储引擎</h3>
          <p>数据库、对象存储等数据存储服务</p>
          <ul>
            <li>PostgreSQL</li>
            <li>MySQL</li>
            <li>MinIO</li>
            <li>S3</li>
          </ul>
        </el-card>

        <el-card class="type-card" shadow="hover" @click="confirmEngineType('compute')">
          <div class="type-icon">🔧</div>
          <h3>计算引擎</h3>
          <p>提供计算能力的服务</p>
          <ul>
            <li>空间计算引擎</li>
            <li>工作流引擎</li>
            <li>数据处理服务</li>
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
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button
          v-if="!isComputeEngineForm"
          type="warning"
          :loading="testing"
          @click="testBeforeCreate"
        >
          测试连接
        </el-button>
        <el-button type="primary" :loading="submitting" @click="submitForm">保存</el-button>
      </template>
    </el-dialog>

    <!-- 引擎详情弹窗 -->
    <el-dialog
      v-model="detailsVisible"
      :title="`引擎详情 - ${selectedEngine?.name || ''}`"
      width="800px"
      destroy-on-close
    >
      <div v-loading="detailsLoading" style="min-height: 300px">
        <el-tabs v-if="selectedEngine" type="border-card">
          <!-- 基本信息标签页 -->
          <el-tab-pane label="基本信息">
            <el-descriptions :column="2" border>
              <el-descriptions-item label="ID">{{ selectedEngine.id }}</el-descriptions-item>
              <el-descriptions-item label="名称">{{ selectedEngine.name }}</el-descriptions-item>
              <el-descriptions-item label="引擎类型">
                <el-tag :type="getEngineTypeColor(selectedEngine.engine_type)">
                  {{ getEngineTypeLabel(selectedEngine.engine_type) }}
                </el-tag>
              </el-descriptions-item>
              <el-descriptions-item label="引擎分类">
                <el-tag :type="selectedEngine.engine_category === 'standard' ? 'success' : 'warning'">
                  {{ selectedEngine.engine_category === 'standard' ? '标准引擎' : '扩展引擎' }}
                </el-tag>
              </el-descriptions-item>
              <el-descriptions-item label="注册方式">
                <el-tag v-if="selectedEngine.is_builtin" type="info">内置引擎</el-tag>
                <el-tag v-else type="success">用户注册</el-tag>
              </el-descriptions-item>
              <el-descriptions-item label="激活状态">
                <el-tag :type="selectedEngine.is_active ? 'success' : 'danger'">
                  {{ selectedEngine.is_active ? '激活' : '禁用' }}
                </el-tag>
              </el-descriptions-item>
              <el-descriptions-item label="创建时间" :span="2">
                {{ formatDate(selectedEngine.created_at) }}
              </el-descriptions-item>
              <el-descriptions-item label="更新时间" :span="2">
                {{ formatDate(selectedEngine.updated_at) }}
              </el-descriptions-item>
              <el-descriptions-item label="描述" :span="2">
                {{ selectedEngine.description || '无' }}
              </el-descriptions-item>
            </el-descriptions>
          </el-tab-pane>

          <!-- 连接配置标签页 -->
          <el-tab-pane label="连接配置" v-if="selectedEngine.connection_info && Object.keys(selectedEngine.connection_info).length > 0">
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
          <el-tab-pane label="能力声明" v-if="selectedEngine.capabilities">
            <div v-if="parseCapabilitiesJSON(selectedEngine.capabilities).storage?.length > 0" style="margin-bottom: 20px">
              <div style="font-weight: 500; margin-bottom: 12px; color: var(--addp-text-primary); font-size: 14px">存储能力</div>
              <el-table :data="parseCapabilitiesJSON(selectedEngine.capabilities).storage" border size="small">
                <el-table-column prop="type" label="类型" width="150">
                  <template #default="{ row }">
                    {{ getStorageTypeLabel(row.type) }}
                  </template>
                </el-table-column>
                <el-table-column prop="engine" label="引擎" />
              </el-table>
            </div>
            <div v-if="parseCapabilitiesJSON(selectedEngine.capabilities).compute?.length > 0">
              <div style="font-weight: 500; margin-bottom: 12px; color: var(--addp-text-primary); font-size: 14px">计算能力</div>
              <el-table :data="parseCapabilitiesJSON(selectedEngine.capabilities).compute" border size="small">
                <el-table-column prop="type" label="类型" width="150">
                  <template #default="{ row }">
                    {{ getComputeTypeLabel(row.type) }}
                  </template>
                </el-table-column>
                <el-table-column prop="dev_modes" label="开发模式" width="150">
                  <template #default="{ row }">
                    <el-tag v-for="mode in row.dev_modes" :key="mode" size="small" style="margin: 2px">
                      {{ mode }}
                    </el-tag>
                  </template>
                </el-table-column>
                <el-table-column prop="description" label="描述" />
              </el-table>
            </div>
          </el-tab-pane>

          <!-- 扫描配置标签页 -->
          <el-tab-pane label="扫描配置" v-if="selectedEngine.scan_config">
            <el-descriptions :column="2" border>
              <el-descriptions-item label="立即扫描" :span="2">
                <el-tag :type="selectedEngine.scan_config.immediate_scan ? 'success' : 'info'">
                  {{ selectedEngine.scan_config.immediate_scan ? '是' : '否' }}
                </el-tag>
                <span v-if="selectedEngine.scan_config.immediate_scan" style="margin-left: 8px">
                  (深度: {{ selectedEngine.scan_config.immediate_depth || 'basic' }})
                </span>
              </el-descriptions-item>
              <el-descriptions-item label="定时扫描" :span="2">
                <el-tag :type="selectedEngine.scan_config.scheduled_scan ? 'success' : 'info'">
                  {{ selectedEngine.scan_config.scheduled_scan ? '是' : '否' }}
                </el-tag>
              </el-descriptions-item>
              <el-descriptions-item v-if="selectedEngine.scan_config.scheduled_scan" label="调度类型">
                {{ selectedEngine.scan_config.schedule_type }}
              </el-descriptions-item>
              <el-descriptions-item v-if="selectedEngine.scan_config.scheduled_scan && selectedEngine.scan_config.cron_expression" label="Cron 表达式">
                {{ selectedEngine.scan_config.cron_expression }}
              </el-descriptions-item>
              <el-descriptions-item v-if="selectedEngine.scan_config.scheduled_scan && selectedEngine.scan_config.schedule_time" label="调度时间">
                {{ selectedEngine.scan_config.schedule_time }}
              </el-descriptions-item>
            </el-descriptions>
          </el-tab-pane>
        </el-tabs>
      </div>
      <template #footer>
        <el-button @click="detailsVisible = false">关闭</el-button>
      </template>
    </el-dialog>
</template>

<script setup>
import { ref, onMounted, computed } from 'vue'
import { enginesAPI } from '../api/engines'
import { Plus, Edit, Delete } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { StorageEngineForm, EngineForm } from '@common-ui'

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
  if (isEdit.value) return '编辑引擎'
  if (selectedEngineCategory.value === 'storage') return '新建存储引擎'
  if (selectedEngineCategory.value === 'compute') return '新建计算引擎'
  return '新建引擎'
})

// 是否使用计算引擎表单
const isComputeEngineForm = computed(() => {
  return selectedEngineCategory.value === 'compute' ||
         (isEdit.value && form.value.engine_type === 'compute_engine')
})

// 过滤后的引擎列表
const filteredEngines = computed(() => {
  if (selectedCategories.value.length === 0) {
    return [] // 全不选则隐藏所有
  }

  return engines.value.filter(engine => {
    const caps = parseCapabilitiesJSON(engine.capabilities)
    const hasStorage = caps.storage?.length > 0
    const hasCompute = caps.compute?.length > 0
    const isBuiltin = engine.is_builtin
    const engineCategory = engine.engine_category

    // 能力维度过滤（storage / compute）
    const matchesCapability =
      (selectedCategories.value.includes('storage') && hasStorage) ||
      (selectedCategories.value.includes('compute') && hasCompute)

    // 引擎分类维度过滤（standard / extension）
    const matchesEngineCategory =
      (selectedCategories.value.includes('standard') && engineCategory === 'standard') ||
      (selectedCategories.value.includes('extension') && engineCategory === 'extension')

    // 内置维度过滤
    // - 如果勾选了"内置",则内置引擎和用户引擎都显示
    // - 如果未勾选"内置",则只显示用户引擎(is_builtin=false)
    const matchesBuiltin = selectedCategories.value.includes('builtin') || !isBuiltin

    // 组合过滤逻辑
    // - 如果选择了能力过滤（storage/compute），必须匹配能力
    // - 如果选择了分类过滤（standard/extension），必须匹配分类
    // - 如果两者都没选，显示空
    // - 如果两者都选了，需要同时满足
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
      matches = false // 如果只选了"内置"但没选其他，不显示任何引擎
    }

    return matches && matchesBuiltin
  })
})

// 对连接配置字段进行排序显示
const sortedConnectionInfo = computed(() => {
  if (!selectedEngine.value?.connection_info) {
    return []
  }

  // 定义字段显示的优先顺序
  const fieldOrder = ['host', 'port', 'database', 'user', 'password', 'sslmode']
  const connectionInfo = selectedEngine.value.connection_info
  const entries = Object.entries(connectionInfo)

  // 排序：优先显示 fieldOrder 中的字段，然后是其他字段（按字母顺序）
  const sorted = entries.sort((a, b) => {
    const [keyA] = a
    const [keyB] = b
    const indexA = fieldOrder.indexOf(keyA)
    const indexB = fieldOrder.indexOf(keyB)

    // 如果 A 在优先顺序中，B 不在，A 排前面
    if (indexA !== -1 && indexB === -1) return -1
    // 如果 B 在优先顺序中，A 不在，B 排前面
    if (indexA === -1 && indexB !== -1) return 1
    // 如果都在优先顺序中，按照顺序索引排序
    if (indexA !== -1 && indexB !== -1) return indexA - indexB
    // 如果都不在优先顺序中，按字母顺序排序
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

  // 存储能力
  if (caps.storage) {
    caps.storage.forEach(s => {
      if (s.type) {
        const typeMap = {
          'relational_db': '关系数据库',
          'object_storage': '对象存储',
          'graph_db': '图数据库'
        }
        tags.push(typeMap[s.type] || s.type)
      }
      if (s.engine) tags.push(s.engine)
    })
  }

  // 计算能力
  if (caps.compute) {
    caps.compute.forEach(c => {
      if (c.type) {
        const typeMap = {
          'sql_query': 'SQL查询',
          'spatial': '空间计算',
          'tile_cache': '瓦片缓存',
          'scan': '元数据扫描',
          'workflow': '工作流'
        }
        tags.push(typeMap[c.type] || c.type)
      }
      if (c.description) tags.push(c.description)
      if (c.category) tags.push(c.category)
    })
  }

  return tags.length > 0 ? tags : ['无']
}

const handleFilterChange = () => {
  // 过滤变化时自动重新渲染（computed 会自动处理）
}

const engineTypeMap = {
  'postgresql': 'PostgreSQL',
  'mysql': 'MySQL',
  'doris': 'Apache Doris',
  'clickhouse': 'ClickHouse',
  'mongodb': 'MongoDB',
  'minio': 'MinIO',
  'neo4j': 'Neo4j',
  'spark': 'Apache Spark',
  'spatialite': 'SpatiaLite/SQLite',
  'database': '数据库',
  'compute_engine': '计算引擎'
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
    'spark': 'danger',
    'database': 'success',
    'compute_engine': 'info'
  }
  return colorMap[type] || 'info'
}

const formatDate = (dateString) => {
  return new Date(dateString).toLocaleString('zh-CN')
}

// 获取连接状态标签
const getConnectionStatusLabel = (status) => {
  const labelMap = {
    'online': '在线',
    'offline': '离线',
    'unknown': '未知',
    'checking': '检测中'
  }
  return labelMap[status] || '未检测'
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

// 获取连接状态标签类型（颜色）- 保留用于其他地方
const getConnectionStatusType = (status) => {
  const typeMap = {
    'online': 'success',
    'offline': 'danger',
    'unknown': 'info',
    'checking': 'primary'
  }
  return typeMap[status] || 'info'
}

// 获取连接状态提示信息
const getConnectionTooltip = (row) => {
  if (!row.connection_status) return '未检测'

  let tooltip = `状态: ${getConnectionStatusLabel(row.connection_status)}`

  if (row.last_check_at) {
    tooltip += `\n最后检测: ${formatDate(row.last_check_at)}`
  }

  if (row.check_message) {
    tooltip += `\n详情: ${row.check_message}`
  }

  return tooltip
}

const loadEngines = async () => {
  loading.value = true
  try {
    const response = await enginesAPI.list(currentPage.value, pageSize.value)
    // 后端返回分页数据格式: { data: [], total: 123, page: 1, page_size: 10 }
    engines.value = response?.data || []
    total.value = response?.total || 0
  } catch (error) {
    ElMessage.error('加载引擎列表失败')
    console.error(error)
  } finally {
    loading.value = false
  }
}

const showAddDialog = () => {
  isEdit.value = false
  editId.value = null
  selectedEngineCategory.value = ''
  resetForm()
  typeSelectionVisible.value = true
}

// 直接打开存储引擎表单
const showAddStorageDialog = () => {
  isEdit.value = false
  editId.value = null
  selectedEngineCategory.value = 'storage'
  resetForm()
  dialogVisible.value = true
}

// 直接打开计算引擎表单
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

  // 根据引擎类型设置分类（用于表单选择）
  if (row.engine_type === 'compute_engine') {
    selectedEngineCategory.value = 'compute'

    // 计算引擎使用完整字段
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

    // 存储引擎使用原有字段
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
  // 根据表单类型选择校验
  const formRef = isComputeEngineForm.value ? resourceFormRef.value : storageFormRef.value
  const valid = await formRef?.validate()
  if (!valid) {
    ElMessage.warning('请完整填写必填信息')
    return
  }

  // 计算引擎暂不支持测试连接（需要健康检查端点）
  if (isComputeEngineForm.value) {
    ElMessage.info('计算引擎需要在保存后通过健康检查端点测试')
    return
  }

  testing.value = true
  try {
    // 始终使用当前表单数据测试（编辑模式初始化时已从 API 获取解密后的连接信息）
    const response = await enginesAPI.testConnection(form.value)
    if (response.success) {
      ElMessage.success('连接测试成功！')
    } else {
      ElMessage.error(`连接测试失败: ${response.error || response.message}`)
    }
  } catch (error) {
    ElMessage.error(`连接测试失败: ${error.response?.data?.error || error.message}`)
  } finally {
    testing.value = false
  }
}

const testConnection = async (row) => {
  try {
    const response = await enginesAPI.testExistingConnection(row.id)
    if (response.success) {
      ElMessage.success('连接测试成功！')
      // 刷新列表以更新连接状态图标
      await loadEngines()
    } else {
      ElMessage.error(`连接测试失败: ${response.error || response.message}`)
      // 即使失败也刷新，因为后端可能更新了状态
      await loadEngines()
    }
  } catch (error) {
    ElMessage.error(`连接测试失败: ${error.response?.data?.error || error.message}`)
    // 刷新列表
    await loadEngines()
  }
}

const submitForm = async () => {
  // 根据表单类型选择校验
  const formRef = isComputeEngineForm.value ? resourceFormRef.value : storageFormRef.value
  const valid = await formRef?.validate()
  if (!valid) return

  submitting.value = true
  try {
    // 准备提交数据
    let submitData = { ...form.value }

    // 计算引擎需要解析JSON字段
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
        ElMessage.error('JSON 格式错误，请检查')
        return
      }
    }

    if (isEdit.value) {
      await enginesAPI.update(editId.value, submitData)
      ElMessage.success('更新成功')
    } else {
      await enginesAPI.create(submitData)
      ElMessage.success('创建成功')
    }
    dialogVisible.value = false
    loadEngines()
  } catch (error) {
    ElMessage.error(error.response?.data?.error || '操作失败')
  } finally {
    submitting.value = false
  }
}

const deleteEngine = (row) => {
  // 前端二次检查（尽管按钮已禁用）
  if (row.is_builtin) {
    ElMessage.warning('内置引擎不可删除')
    return
  }

  ElMessageBox.confirm(
    `确定要删除引擎 "${row.name}" 吗？此操作不可恢复。`,
    '确认删除',
    {
      confirmButtonText: '确定',
      cancelButtonText: '取消',
      type: 'warning'
    }
  ).then(async () => {
    try {
      await enginesAPI.delete(row.id)
      ElMessage.success('删除成功')
      loadEngines()
    } catch (error) {
      const errorMsg = error.response?.data?.error || '删除失败'
      ElMessage.error(errorMsg)
    }
  }).catch(() => {})
}

// 查看引擎详情
const viewEngineDetails = async (row) => {
  detailsLoading.value = true
  detailsVisible.value = true
  selectedEngine.value = null

  try {
    const response = await enginesAPI.getById(row.id)
    selectedEngine.value = response
  } catch (error) {
    ElMessage.error('获取引擎详情失败')
    console.error(error)
    detailsVisible.value = false
  } finally {
    detailsLoading.value = false
  }
}

// 格式化JSON字符串
const formatJSON = (jsonStr) => {
  if (!jsonStr) return 'N/A'
  try {
    return JSON.stringify(JSON.parse(jsonStr), null, 2)
  } catch {
    return jsonStr
  }
}

// 获取存储类型标签
const getStorageTypeLabel = (type) => {
  const typeMap = {
    'relational_db': '关系数据库',
    'object_storage': '对象存储',
    'graph_db': '图数据库'
  }
  return typeMap[type] || type
}

// 获取计算类型标签
const getComputeTypeLabel = (type) => {
  const typeMap = {
    'sql_query': 'SQL查询',
    'spatial': '空间计算',
    'tile_cache': '瓦片缓存',
    'scan': '元数据扫描',
    'workflow': '工作流'
  }
  return typeMap[type] || type
}

// 表格行样式
const tableRowClassName = ({ row }) => {
  return row.is_builtin ? 'builtin-engine-row' : ''
}

const resetForm = () => {
  if (isComputeEngineForm.value) {
    // 计算引擎表单重置
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
    // 存储引擎表单重置
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
