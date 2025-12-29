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
          <el-checkbox label="storage">存储</el-checkbox>
          <el-checkbox label="compute">计算</el-checkbox>
          <el-checkbox label="standard">标准引擎</el-checkbox>
          <el-checkbox label="extension">扩展引擎</el-checkbox>
          <el-checkbox label="builtin">内置</el-checkbox>
        </el-checkbox-group>
      </div>

      <el-table :data="filteredEngines" v-loading="loading" stripe :row-class-name="tableRowClassName">
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column prop="name" label="名称" min-width="150" />

        <!-- 类型标签列 -->
        <el-table-column label="类型标签" width="180">
          <template #default="{ row }">
            <el-tag
              v-for="label in getTypeLabels(row)"
              :key="label"
              size="small"
              :type="label === '存储' ? 'success' : 'info'"
              style="margin-right: 5px"
            >
              {{ label }}
            </el-tag>
          </template>
        </el-table-column>

        <el-table-column label="引擎标识" width="120">
          <template #default="{ row }">
            <el-tag v-if="row.is_builtin" type="info" size="small" effect="plain">
              🔵 内置
            </el-tag>
            <el-tag v-else type="success" size="small" effect="light">
              用户引擎
            </el-tag>
          </template>
        </el-table-column>

        <!-- 引擎分类列 -->
        <el-table-column label="引擎分类" width="120">
          <template #default="{ row }">
            <el-tag :type="row.engine_category === 'standard' ? 'success' : 'warning'">
              {{ row.engine_category === 'standard' ? '标准引擎' : '扩展引擎' }}
            </el-tag>
          </template>
        </el-table-column>

        <el-table-column prop="resource_type" label="类型" width="150">
          <template #default="{ row }">
            <el-tag :type="getEngineTypeColor(row.engine_type)">
              {{ getEngineTypeLabel(row.engine_type) }}
            </el-tag>
          </template>
        </el-table-column>

        <!-- 能力标签列 -->
        <el-table-column label="能力" min-width="250">
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

        <el-table-column prop="description" label="描述" min-width="200" />

        <!-- 连接状态列 -->
        <el-table-column label="连接状态" width="120">
          <template #default="{ row }">
            <el-tooltip
              :content="getConnectionTooltip(row)"
              placement="top"
              :disabled="!row.connection_status"
            >
              <el-tag :type="getConnectionStatusType(row.connection_status)">
                {{ getConnectionStatusLabel(row.connection_status) }}
              </el-tag>
            </el-tooltip>
          </template>
        </el-table-column>

        <el-table-column label="状态" width="100">
          <template #default="{ row }">
            <el-tag :type="row.is_active ? 'success' : 'danger'">
              {{ row.is_active ? '激活' : '禁用' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="创建时间" width="180">
          <template #default="{ row }">
            {{ formatDate(row.created_at) }}
          </template>
        </el-table-column>
        <el-table-column label="操作" width="300" fixed="right">
          <template #default="{ row }">
            <el-button size="small" type="success" @click="testConnection(row)">测试连接</el-button>
            <el-button
              size="small"
              type="primary"
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
            <el-button
              v-if="row.is_builtin"
              size="small"
              type="info"
              @click="viewBuiltinDetails(row)"
            >
              详情
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

    <!-- 内置引擎详情弹窗 -->
    <el-dialog
      v-model="builtinDetailsVisible"
      title="内置引擎详情"
      width="600px"
    >
      <el-descriptions :column="1" border v-if="selectedBuiltinEngine">
        <el-descriptions-item label="唯一标识">
          {{ selectedBuiltinEngine.unique_identifier || 'N/A' }}
        </el-descriptions-item>
        <el-descriptions-item label="显示名称">
          {{ selectedBuiltinEngine.display_name || 'N/A' }}
        </el-descriptions-item>
        <el-descriptions-item label="引擎类型">
          {{ selectedBuiltinEngine.engine_type }}
        </el-descriptions-item>
        <el-descriptions-item label="能力声明">
          <pre style="margin: 0; white-space: pre-wrap; font-size: 12px;">{{ formatJSON(selectedBuiltinEngine.capabilities) }}</pre>
        </el-descriptions-item>
        <el-descriptions-item label="描述">
          {{ selectedBuiltinEngine.description || '无' }}
        </el-descriptions-item>
      </el-descriptions>
      <template #footer>
        <el-button @click="builtinDetailsVisible = false">关闭</el-button>
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

// 内置引擎详情弹窗相关
const builtinDetailsVisible = ref(false)
const selectedBuiltinEngine = ref(null)

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

// 获取类型标签（[存储] [计算]）
const getTypeLabels = (engine) => {
  const labels = []
  const caps = parseCapabilitiesJSON(engine.capabilities)

  if (caps.storage?.length > 0) labels.push('存储')
  if (caps.compute?.length > 0) labels.push('计算')

  return labels
}

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
  'minio': 'MinIO',
  'spark_sql': 'Spark SQL',
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
    'spark_sql': 'danger',
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

// 获取连接状态标签类型（颜色）
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

  let tooltip = `状态: ${getConnectionStatusLabel(row.connection_status)}\n`

  if (row.last_check_at) {
    tooltip += `检测时间: ${formatDate(row.last_check_at)}\n`
  }

  if (row.check_message) {
    tooltip += `详情: ${row.check_message}`
  }

  return tooltip
}

const loadEngines = async () => {
  loading.value = true
  try {
    const response = await enginesAPI.list(currentPage.value, pageSize.value)
    engines.value = response || []
    total.value = (response || []).length
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
    // 如果是编辑模式，使用已保存资源的ID进行测试（会使用数据库中的真实密钥）
    if (isEdit.value && editId.value) {
      const response = await enginesAPI.testExistingConnection(editId.value)
      if (response.success) {
        ElMessage.success('连接测试成功！')
      } else {
        ElMessage.error(`连接测试失败: ${response.error || response.message}`)
      }
    } else {
      // 新增模式，使用表单数据测试
      const response = await enginesAPI.testConnection(form.value)
      if (response.success) {
        ElMessage.success('连接测试成功！')
      } else {
        ElMessage.error(`连接测试失败: ${response.error || response.message}`)
      }
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
    } else {
      ElMessage.error(`连接测试失败: ${response.error || response.message}`)
    }
  } catch (error) {
    ElMessage.error(`连接测试失败: ${error.response?.data?.error || error.message}`)
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

// 查看内置引擎详情
const viewBuiltinDetails = (row) => {
  selectedBuiltinEngine.value = row
  builtinDetailsVisible.value = true
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
  color: #606266;
}

/* 内置引擎行样式 */
:deep(.builtin-engine-row) {
  background-color: #f5f7fa;
}

:deep(.builtin-engine-row:hover) {
  background-color: #ebeef5 !important;
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
  border-color: #409eff;
}

.type-icon {
  font-size: 48px;
  margin-bottom: 16px;
}

.type-card h3 {
  margin: 16px 0 8px;
  font-size: 18px;
  font-weight: 600;
  color: #303133;
}

.type-card p {
  margin: 0 0 16px;
  font-size: 14px;
  color: #606266;
  line-height: 1.5;
}

.type-card ul {
  list-style: none;
  padding: 0;
  margin: 0;
  font-size: 13px;
  color: #909399;
}

.type-card ul li {
  padding: 4px 0;
}
</style>
