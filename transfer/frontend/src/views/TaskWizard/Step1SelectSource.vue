<template>
  <div class="step1-select-source">
    <h3>选择数据源</h3>
    <p class="step-description">请选择数据源引擎和表</p>

    <el-form :model="formData" label-width="120px">
      <!-- 数据源类型 -->
      <el-form-item label="数据源类型">
        <el-radio-group v-model="sourceType" @change="handleSourceTypeChange">
          <el-radio-button label="postgresql">PostgreSQL</el-radio-button>
          <el-radio-button label="mysql">MySQL</el-radio-button>
          <el-radio-button label="spatialite">SpatiaLite</el-radio-button>
          <el-radio-button label="s3">S3/MinIO</el-radio-button>
        </el-radio-group>
      </el-form-item>

      <!-- 数据源引擎 -->
      <el-form-item label="数据源引擎">
        <el-select
          v-model="formData.engineValue"
          placeholder="请选择数据源引擎"
          filterable
          :loading="loadingEngines"
          @change="handleEngineChange"
        >
          <el-option-group
            v-if="filteredSystemEngines.length > 0"
            label="系统管理（全局可用）"
          >
            <el-option
              v-for="engine in filteredSystemEngines"
              :key="`system:${engine.id}`"
              :label="`${engine.name} (${engine.engine_type})`"
              :value="`system:${engine.id}`"
            />
          </el-option-group>
          <el-option-group
            v-if="filteredLocalEngines.length > 0"
            label="本地引擎（仅数据传输可用）"
          >
            <el-option
              v-for="engine in filteredLocalEngines"
              :key="`local:${engine.id}`"
              :label="`${engine.name} (${engine.engine_type})`"
              :value="`local:${engine.id}`"
            />
          </el-option-group>
        </el-select>
        <div class="hint" style="margin-top: 8px; font-size: 13px; color: #909399;">
          <p>系统管理的引擎从元数据模块自动加载表，本地引擎实时扫描数据库</p>
        </div>
      </el-form-item>

      <!-- 查询模式（仅 postgresql/mysql/spatialite） -->
      <el-form-item v-if="supportsQueryMode && formData.engineValue" label="查询方式">
        <el-radio-group v-model="queryMode" @change="handleQueryModeChange">
          <el-radio-button label="table">选择表</el-radio-button>
          <el-radio-button label="sql">自定义 SQL</el-radio-button>
        </el-radio-group>
      </el-form-item>

      <!-- SQL 查询（仅 SQL 模式） -->
      <el-form-item v-if="queryMode === 'sql'" label="SQL 查询">
        <el-input
          v-model="sqlQuery"
          type="textarea"
          :rows="8"
          placeholder="请输入 SQL 查询语句，例如：SELECT * FROM table_name WHERE condition"
          @change="handleSQLQueryChange"
        />
        <div class="hint" style="margin-top: 8px; font-size: 13px; color: #909399;">
          <p>支持标准 SQL 语法，查询结果将作为数据源</p>
        </div>
      </el-form-item>

      <!-- Schema（仅 PostgreSQL/MySQL 的系统引擎需要，且在 table 模式） -->
      <el-form-item v-if="needsSchema && queryMode === 'table'" label="Schema">
        <el-select
          v-model="formData.schema"
          placeholder="请选择 Schema"
          filterable
          :loading="loadingSchemas"
          :disabled="!formData.engineValue"
          @change="handleSchemaChange"
        >
          <el-option
            v-for="schema in schemas"
            :key="schema.schema_name"
            :label="schema.schema_name"
            :value="schema.schema_name"
          />
        </el-select>
      </el-form-item>

      <!-- 表（仅 table 模式） -->
      <el-form-item v-if="queryMode === 'table'" label="数据表">
        <el-select
          v-model="formData.table"
          placeholder="请选择数据表"
          filterable
          :loading="loadingTables"
          :disabled="!formData.engineValue"
          @change="handleTableChange"
        >
          <el-option
            v-for="table in tables"
            :key="table.name"
            :label="table.row_count !== undefined ? `${table.name} (${table.row_count || 0} 行)` : table.name"
            :value="table.name"
          />
        </el-select>
      </el-form-item>

      <!-- 表信息预览（仅系统引擎有元数据） -->
      <el-form-item v-if="selectedTable && hasTableMetadata" label="表信息">
        <div class="table-info">
          <p v-if="selectedTable.row_count !== undefined"><strong>行数：</strong>{{ selectedTable.row_count || 0 }}</p>
          <p v-if="selectedTable.size_bytes !== undefined"><strong>大小：</strong>{{ formatBytes(selectedTable.size_bytes) }}</p>
          <p v-if="selectedTable.comment"><strong>备注：</strong>{{ selectedTable.comment }}</p>
        </div>
      </el-form-item>
    </el-form>
  </div>
</template>

<script setup>
import { ref, reactive, computed, watch, onMounted, nextTick } from 'vue'
import { ElMessage } from 'element-plus'
import { getTables, getTableFields, getSchemas } from '@/api/meta'
import { systemEnginesAPI } from '@/api/systemEngines'
import { localEnginesAPI } from '@/api/localEngines'

// Props
const props = defineProps({
  wizardState: {
    type: Object,
    required: true
  }
})

// Emits
const emit = defineEmits(['next', 'prev'])

// State
const sourceType = ref('postgresql') // 数据源类型: postgresql, mysql, spatialite, s3
const queryMode = ref('table') // 查询模式: table, sql
const sqlQuery = ref('') // SQL 查询语句

const formData = reactive({
  engineValue: '', // 格式: "system:1" 或 "local:1"
  schema: '',
  table: ''
})

const systemEngines = ref([])
const localEngines = ref([])
const schemas = ref([])
const tables = ref([])
const selectedTable = ref(null)

const loadingEngines = ref(false)
const loadingSchemas = ref(false)
const loadingTables = ref(false)

// 解析引擎 value
const parseEngineValue = (value) => {
  if (!value) return { origin: null, id: null }
  const [origin, id] = value.split(':')
  return { origin, id: parseInt(id) }
}

// 引擎类型匹配函数（从旧版本复制）
const matchesConnectorType = (resourceType, connectorType) => {
  const resource = (resourceType || '').toLowerCase()
  const type = (connectorType || '').toLowerCase()
  if (!type) return true
  if (type === 's3') {
    return ['s3', 'minio', 'oss'].includes(resource)
  }
  if (type === 'spatialite' || type === 'sqlite') {
    return resource.includes('spatialite') || resource.includes('sqlite')
  }
  return resource.includes(type)
}

// 当前选中的引擎信息
const selectedEngine = computed(() => {
  if (!formData.engineValue) return null
  const { origin, id } = parseEngineValue(formData.engineValue)
  if (origin === 'system') {
    return systemEngines.value.find(e => e.id === id)
  } else if (origin === 'local') {
    return localEngines.value.find(e => e.id === id)
  }
  return null
})

// 根据数据源类型过滤引擎
const filteredSystemEngines = computed(() => {
  return systemEngines.value.filter(e =>
    matchesConnectorType(e.engine_type, sourceType.value)
  )
})

const filteredLocalEngines = computed(() => {
  return localEngines.value.filter(e =>
    matchesConnectorType(e.engine_type, sourceType.value)
  )
})

// 判断是否需要 schema 选择器（仅系统引擎的 postgresql/mysql）
const needsSchema = computed(() => {
  if (!formData.engineValue) return false
  const { origin } = parseEngineValue(formData.engineValue)
  // 仅系统引擎需要 schema
  if (origin !== 'system') return false
  // 判断引擎类型是否需要 schema（PostgreSQL, MySQL）
  const engine = selectedEngine.value
  if (!engine) return false
  const engineType = (engine.engine_type || '').toLowerCase()
  return engineType.includes('postgresql') || engineType.includes('mysql')
})

// 判断是否支持查询模式切换（仅 postgresql/mysql/spatialite）
const supportsQueryMode = computed(() => {
  const type = sourceType.value.toLowerCase()
  return ['postgresql', 'mysql', 'spatialite'].includes(type)
})

// Computed
const canProceed = computed(() => {
  // SQL 模式：需要引擎和 SQL 查询
  if (queryMode.value === 'sql') {
    return formData.engineValue && sqlQuery.value.trim() !== ''
  }
  // Table 模式：需要引擎和表
  return formData.engineValue && formData.table
})

// 判断当前选中的表是否有元数据（行数、大小、备注）
const hasTableMetadata = computed(() => {
  if (!selectedTable.value) return false
  return selectedTable.value.row_count !== undefined ||
         selectedTable.value.size_bytes !== undefined ||
         !!selectedTable.value.comment
})

// 监听变化
watch(canProceed, (newVal) => {
  if (newVal) {
    const { origin, id } = parseEngineValue(formData.engineValue)
    // 更新向导状态
    props.wizardState.updateSource({
      engineID: id,
      scope: origin, // 'system' 或 'local'
      schema: formData.schema,
      table: formData.table,
      sourceType: sourceType.value, // 添加数据源类型
      queryMode: queryMode.value, // 添加查询模式
      sqlQuery: sqlQuery.value // 添加 SQL 查询
    })
  }
})

// Methods
function handleSourceTypeChange() {
  // 切换数据源类型时，清空已选择的引擎和表
  formData.engineValue = ''
  formData.table = ''
  tables.value = []
  selectedTable.value = null
}

function handleQueryModeChange() {
  // 切换查询模式时，清空相关字段
  if (queryMode.value === 'sql') {
    // 切换到 SQL 模式，清空表选择
    formData.table = ''
    selectedTable.value = null
  } else {
    // 切换到 Table 模式，清空 SQL 查询
    sqlQuery.value = ''
  }
}

function handleSQLQueryChange() {
  // SQL 查询变化时，触发 wizardState 更新
  // 通过 watch canProceed 自动触发
}

async function loadEngines() {
  loadingEngines.value = true
  try {
    // 并行加载系统引擎和本地引擎
    const [systemData, localData] = await Promise.all([
      systemEnginesAPI.list().catch(() => []),
      localEnginesAPI.list().catch(() => [])
    ])

    // 过滤掉 id 为 undefined 的引擎
    systemEngines.value = (systemData || []).filter(engine =>
      engine && engine.id !== undefined && engine.id !== null
    )
    localEngines.value = (localData || []).filter(engine =>
      engine && engine.id !== undefined && engine.id !== null
    )

    if (systemEngines.value.length === 0 && localEngines.value.length === 0) {
      ElMessage.warning('未找到可用的存储引擎，请先在系统管理中配置')
    }
  } catch (error) {
    ElMessage.error('加载存储引擎失败')
    console.error(error)
  } finally {
    loadingEngines.value = false
  }
}

async function handleEngineChange() {
  formData.schema = ''
  formData.table = ''
  schemas.value = []
  tables.value = []
  selectedTable.value = null

  if (!formData.engineValue) return

  const { origin, id } = parseEngineValue(formData.engineValue)

  if (origin === 'system') {
    // 系统引擎：判断是否需要先加载 schemas
    if (needsSchema.value) {
      // PostgreSQL/MySQL：先加载 schemas
      loadingSchemas.value = true
      try {
        console.log('[Step1] 加载系统引擎 schemas, engineID:', id)
        const response = await getSchemas(id)
        console.log('[Step1] Schemas API 响应:', response)
        // response 已经是 { data: [...] } 格式，直接访问 data 字段
        const schemaList = Array.isArray(response?.data) ? response.data : (response || [])
        // 过滤掉 schema_name 为 undefined 的数据
        schemas.value = schemaList.filter(schema =>
          schema && schema.schema_name !== undefined && schema.schema_name !== null
        )
        if (schemas.value.length > 0) {
          ElMessage.success(`已加载 ${schemas.value.length} 个 Schema`)
        } else {
          ElMessage.warning('未找到可用的 Schema，请确认元数据模块已扫描该数据源')
        }
      } catch (error) {
        ElMessage.error('加载 Schema 列表失败')
        console.error(error)
      } finally {
        loadingSchemas.value = false
      }
    } else {
      // 其他类型（如 S3）：不需要 schema，直接加载表
      await loadSystemEngineTables(id, null)
    }
  } else if (origin === 'local') {
    // 本地引擎：通过 Transfer 模块自己的 API 实时扫描表
    await loadLocalEngineTables(id)
  }
}

// Schema 改变时加载表（仅系统引擎）
async function handleSchemaChange() {
  formData.table = ''
  tables.value = []
  selectedTable.value = null

  if (!formData.schema) return

  const { origin, id } = parseEngineValue(formData.engineValue)
  if (origin === 'system') {
    await loadSystemEngineTables(id, formData.schema)
  }
}

// 加载系统引擎的表列表
async function loadSystemEngineTables(engineID, schema) {
  loadingTables.value = true
  try {
    console.log('[Step1] 加载系统引擎表列表, engineID:', engineID, 'schema:', schema)
    const response = await getTables(engineID, schema)
    console.log('[Step1] Tables API 响应:', response)
    // response 已经是 { data: [...] } 格式，直接访问 data 字段
    const tableList = Array.isArray(response?.data) ? response.data : (response || [])
    // 过滤掉 name 为 undefined/null 的数据
    tables.value = tableList
      .map(item => ({
        name: item.name || item,
        row_count: item.row_count || 0,
        size_bytes: item.size_bytes || 0,
        comment: item.comment || ''
      }))
      .filter(table => table.name !== undefined && table.name !== null)
    if (tables.value.length > 0) {
      ElMessage.success(`已加载 ${tables.value.length} 个表`)
    } else {
      ElMessage.warning('未找到可用的表，请确认元数据模块已扫描该数据源')
    }
  } catch (error) {
    ElMessage.error('加载表列表失败')
    console.error(error)
  } finally {
    loadingTables.value = false
  }
}

// 加载本地引擎的表列表
async function loadLocalEngineTables(engineID) {
  loadingTables.value = true
  try {
    const res = await localEnginesAPI.listTables(engineID)
    const tableList = Array.isArray(res) ? res : (Array.isArray(res?.data) ? res.data : [])
    tables.value = tableList
      .map(item => {
        if (typeof item === 'string') {
          // 本地引擎返回字符串数组，不包含元数据
          return { name: item }
        } else {
          // 如果是对象（未来可能支持），保留元数据
          return {
            name: item.name,
            row_count: item.row_count,
            size_bytes: item.size_bytes,
            comment: item.comment || ''
          }
        }
      })
      .filter(table => table.name !== undefined && table.name !== null)
    if (tables.value.length > 0) {
      ElMessage.success(`已从本地引擎扫描到 ${tables.value.length} 个表`)
    } else {
      ElMessage.warning('未找到可用的表')
    }
  } catch (error) {
    ElMessage.error('加载表列表失败')
    console.error(error)
  } finally {
    loadingTables.value = false
  }
}

async function handleTableChange() {
  selectedTable.value = tables.value.find(t => t.name === formData.table)

  if (!formData.table) return

  const { origin, id } = parseEngineValue(formData.engineValue)

  // 加载表字段
  try {
    if (origin === 'system') {
      // 系统引擎：从 Meta 模块获取字段
      const response = await getTableFields(id, formData.schema, formData.table)
      // response 已经是 { data: [...] } 格式，直接访问 data 字段
      const fieldList = Array.isArray(response?.data) ? response.data : (response || [])
      props.wizardState.loadSourceFields(fieldList)
    } else if (origin === 'local') {
      // 本地引擎：通过 Transfer 模块自己的 API 实时扫描字段
      const res = await localEnginesAPI.listFields(id, formData.table)
      const fieldList = Array.isArray(res) ? res : (Array.isArray(res?.data) ? res.data : [])
      props.wizardState.loadSourceFields(fieldList)
    }
  } catch (error) {
    ElMessage.error('加载表字段失败')
    console.error(error)
  }
}

function formatBytes(bytes) {
  if (!bytes) return '0 B'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return Math.round(bytes / Math.pow(k, i) * 100) / 100 + ' ' + sizes[i]
}

// 从 wizardState 恢复之前的状态
async function restoreState() {
  const state = props.wizardState

  // 恢复数据源类型
  if (state.sourceType.value) {
    sourceType.value = state.sourceType.value
  }

  // 恢复查询模式
  if (state.sourceQueryMode.value) {
    queryMode.value = state.sourceQueryMode.value
  }

  // 恢复 SQL 查询
  if (state.sourceSQLQuery.value) {
    sqlQuery.value = state.sourceSQLQuery.value
  }

  // 恢复引擎、schema、表选择
  if (state.sourceEngineID.value && state.sourceScope.value) {
    formData.engineValue = `${state.sourceScope.value}:${state.sourceEngineID.value}`

    // 延迟恢复 schema 和 table，等待引擎加载完成
    await nextTick()

    if (state.sourceSchema.value) {
      formData.schema = state.sourceSchema.value
      // 如果有 schema，需要先加载 schema 的表列表
      if (queryMode.value === 'table') {
        const { origin, id } = parseEngineValue(formData.engineValue)
        if (origin === 'system') {
          await loadSystemEngineTables(id, state.sourceSchema.value)
        }
      }
    }

    if (state.sourceTable.value) {
      formData.table = state.sourceTable.value
      selectedTable.value = tables.value.find(t => t.name === state.sourceTable.value)
    }
  }
}

// Lifecycle
onMounted(async () => {
  await loadEngines()
  await restoreState()
})
</script>

<style scoped>
.step1-select-source {
  max-width: 800px;
  margin: 0 auto;
}

.step-description {
  color: #606266;
  margin-bottom: 30px;
}

.table-info {
  padding: 12px;
  background: #f5f7fa;
  border-radius: 4px;
}

.table-info p {
  margin: 8px 0;
  color: #606266;
}
</style>
