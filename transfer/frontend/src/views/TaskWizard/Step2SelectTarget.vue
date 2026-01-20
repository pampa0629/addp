<template>
  <div class="step2-select-target">
    <h3>选择目标数据库</h3>
    <p class="step-description">请选择目标数据库和表（数据将写入此处）</p>

    <el-form :model="formData" label-width="120px">
      <!-- 目标类型 -->
      <el-form-item label="目标类型">
        <el-radio-group v-model="targetType" @change="handleTargetTypeChange">
          <el-radio-button label="postgresql">PostgreSQL</el-radio-button>
          <el-radio-button label="mysql">MySQL</el-radio-button>
          <el-radio-button label="s3">对象存储</el-radio-button>
        </el-radio-group>
      </el-form-item>

      <!-- 目标引擎 -->
      <el-form-item label="目标引擎">
        <el-select
          v-model="formData.engineID"
          placeholder="请选择目标引擎"
          filterable
          @change="handleEngineChange"
        >
          <el-option
            v-for="engine in filteredTargetEngines"
            :key="engine.id"
            :label="engine.name"
            :value="engine.id"
          />
        </el-select>
      </el-form-item>

      <!-- 对象存储配置 -->
      <template v-if="targetType === 's3'">
        <!-- 输出格式 -->
        <el-form-item label="输出格式">
          <el-select v-model="outputFormat" placeholder="请选择输出格式">
            <el-option label="CSV" value="csv" />
            <el-option label="JSON Lines" value="jsonl" />
            <el-option label="Parquet" value="parquet" />
            <el-option
              label="GeoJSON"
              value="geojson"
              :disabled="!hasSpatialFields"
            >
              <span>GeoJSON</span>
              <span v-if="!hasSpatialFields" style="color: #909399; font-size: 12px; margin-left: 8px;">（无空间字段）</span>
            </el-option>
            <el-option
              label="Shapefile"
              value="shapefile"
              :disabled="!hasSpatialFields"
            >
              <span>Shapefile</span>
              <span v-if="!hasSpatialFields" style="color: #909399; font-size: 12px; margin-left: 8px;">（无空间字段）</span>
            </el-option>
          </el-select>
        </el-form-item>

        <!-- 输出路径 -->
        <el-form-item label="输出路径">
          <el-input
            v-model="outputPath"
            placeholder="例如：exports/data.csv 或 exports/output.geojson"
          />
          <div class="hint" style="margin-top: 8px; font-size: 13px; color: #909399;">
            <p>文件将保存到对象存储的指定路径（相对于 bucket 根目录）</p>
          </div>
        </el-form-item>

        <!-- CSV 专用选项 -->
        <template v-if="outputFormat === 'csv'">
          <el-form-item label="CSV 选项">
            <div style="display: flex; align-items: center; gap: 20px;">
              <el-checkbox v-model="csvHeaders">包含表头</el-checkbox>
              <div style="display: flex; align-items: center; gap: 8px;">
                <span style="color: #606266;">分隔符：</span>
                <el-input
                  v-model="csvDelimiter"
                  placeholder=","
                  style="width: 80px;"
                />
              </div>
            </div>
          </el-form-item>
        </template>

        <!-- 几何字段选择器（针对空间格式） -->
        <el-form-item
          v-if="['geojson', 'shapefile'].includes(outputFormat) && hasSpatialFields"
          label="几何字段"
        >
          <el-select v-model="geometryField" placeholder="请选择几何字段">
            <el-option
              v-for="field in props.wizardState.sourceFields.filter(f => {
                const standardType = (f.standard_type || '').toLowerCase()
                const dataType = (f.data_type || '').toLowerCase()
                return standardType === 'geometry' ||
                       dataType.includes('geometry') ||
                       dataType.includes('point') ||
                       dataType.includes('linestring') ||
                       dataType.includes('polygon')
              })"
              :key="field.name"
              :label="`${field.name} (${field.data_type})`"
              :value="field.name"
            />
          </el-select>
          <div class="hint" style="margin-top: 8px; font-size: 13px; color: #909399;">
            <p>选择用于空间数据导出的几何字段</p>
          </div>
        </el-form-item>
      </template>

      <!-- 数据库配置（仅非对象存储） -->
      <template v-else>
      <!-- Schema（仅 PostgreSQL/MySQL 需要） -->
      <el-form-item v-if="needsSchema" label="Schema">
        <el-select
          v-model="formData.schema"
          placeholder="请选择 Schema"
          filterable
          :loading="loadingSchemas"
          :disabled="!formData.engineID"
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

      <!-- 目标表（智能输入框：可选择已有表或手动输入新表名） -->
      <el-form-item label="目标表">
        <el-select
          v-model="formData.table"
          placeholder="请选择目标表或输入新表名"
          filterable
          allow-create
          default-first-option
          :loading="loadingTables"
          :disabled="!formData.engineID || (needsSchema && !formData.schema)"
          @change="handleTableChange"
        >
          <el-option
            v-for="table in tables"
            :key="table.name"
            :label="table.name"
            :value="table.name"
          />
        </el-select>
        <el-alert type="info" :closable="false" style="margin-top: 12px;" show-icon>
          <template #title>
            数据写入采用覆盖模式：先清空目标表，再插入新数据。目标表不存在时将自动创建。
          </template>
        </el-alert>
      </el-form-item>
      </template>
    </el-form>
  </div>
</template>

<script setup>
import { ref, reactive, computed, watch, onMounted, nextTick } from 'vue'
import { ElMessage } from 'element-plus'
import { getTables, getTableFields, getSchemas } from '@/api/meta'
import { systemEnginesAPI } from '@/api/systemEngines'

const props = defineProps({
  wizardState: {
    type: Object,
    required: true
  }
})

const formData = reactive({
  engineID: null,
  schema: '',
  table: ''
})

const targetType = ref('postgresql') // 目标类型: postgresql, mysql, s3

// 对象存储导出配置
const outputFormat = ref('csv') // csv, jsonl, parquet, geojson, shapefile
const outputPath = ref('') // 输出路径
const csvHeaders = ref(true) // CSV 是否包含表头
const csvDelimiter = ref(',') // CSV 分隔符
const geometryField = ref('') // 几何字段（用于空间格式）

const targetEngines = ref([])
const schemas = ref([])
const tables = ref([])
const loadingSchemas = ref(false)
const loadingTables = ref(false)

// 引擎类型匹配函数
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

// 根据目标类型过滤引擎
const filteredTargetEngines = computed(() => {
  return targetEngines.value.filter(e =>
    matchesConnectorType(e.engine_type, targetType.value)
  )
})

// 判断当前目标类型是否为数据库（非对象存储）
const isDatabase = computed(() => {
  return targetType.value !== 's3'
})

// 判断是否有空间字段（用于判断是否可以选择 geojson/shapefile 格式）
const hasSpatialFields = computed(() => {
  return props.wizardState.sourceFields.some(field => {
    const standardType = (field.standard_type || '').toLowerCase()
    const dataType = (field.data_type || '').toLowerCase()
    return standardType === 'geometry' ||
           dataType.includes('geometry') ||
           dataType.includes('point') ||
           dataType.includes('linestring') ||
           dataType.includes('polygon')
  })
})

// 判断是否需要 Schema 选择器（PostgreSQL/MySQL 的系统引擎需要）
const needsSchema = computed(() => {
  if (!formData.engineID || targetType.value === 's3') return false
  const engine = targetEngines.value.find(e => e.id === formData.engineID)
  if (!engine) return false
  const engineType = (engine.engine_type || '').toLowerCase()
  return engineType.includes('postgres') || engineType.includes('mysql')
})

const canProceed = computed(() => {
  if (targetType.value === 's3') {
    // 对象存储：需要引擎和输出路径
    return formData.engineID && outputPath.value.trim() !== ''
  }
  // 数据库：需要引擎和表
  return formData.engineID && formData.table
})

watch(canProceed, (newVal) => {
  if (newVal) {
    if (targetType.value === 's3') {
      // 对象存储配置
      const extra = {
        output_format: outputFormat.value,
        output_path: outputPath.value
      }

      // CSV 专用选项
      if (outputFormat.value === 'csv') {
        extra.csv_headers = csvHeaders.value
        extra.csv_delimiter = csvDelimiter.value
      }

      // 空间格式需要几何字段
      if (['geojson', 'shapefile'].includes(outputFormat.value) && geometryField.value) {
        extra.geometry_field = geometryField.value
      }

      props.wizardState.updateTarget({
        engineID: formData.engineID,
        scope: 'system', // 目标引擎来源（当前仅支持 system）
        targetType: targetType.value,
        extra
      })
    } else {
      // 数据库配置
      props.wizardState.updateTarget({
        engineID: formData.engineID,
        scope: 'system', // 目标引擎来源（当前仅支持 system）
        schema: formData.schema,
        table: formData.table,
        targetType: targetType.value
      })
    }
  }
})

function handleTargetTypeChange() {
  // 切换目标类型时，清空已选择的引擎和表
  formData.engineID = null
  formData.table = ''
  tables.value = []
}

async function loadTargetEngines() {
  try {
    const data = await systemEnginesAPI.list()
    targetEngines.value = data || []
  } catch (error) {
    ElMessage.error('加载目标引擎失败')
  }
}

async function handleEngineChange() {
  formData.schema = ''
  formData.table = ''
  schemas.value = []
  tables.value = []

  if (!formData.engineID) return

  // 如果需要 schema,先加载 schema 列表
  if (needsSchema.value) {
    loadingSchemas.value = true
    try {
      const response = await getSchemas(formData.engineID)
      // response 已经是 { data: [...] } 格式，直接访问 data 字段
      const schemaList = Array.isArray(response?.data) ? response.data : (response || [])
      schemas.value = schemaList
    } catch (error) {
      ElMessage.error('加载 Schema 列表失败')
    } finally {
      loadingSchemas.value = false
    }
  } else {
    // 不需要 schema,直接加载表列表
    await loadTables()
  }
}

async function handleSchemaChange() {
  formData.table = ''
  tables.value = []

  if (!formData.schema) return

  await loadTables()
}

async function loadTables() {
  if (!formData.engineID) return

  loadingTables.value = true
  try {
    const schema = needsSchema.value ? formData.schema : null
    const response = await getTables(formData.engineID, schema)
    // response 已经是 { data: [...] } 格式，直接访问 data 字段
    const tableList = Array.isArray(response?.data) ? response.data : (response || [])
    tables.value = tableList.map(item => ({
      name: item.name || item
    }))
  } catch (error) {
    ElMessage.error('加载表列表失败')
  } finally {
    loadingTables.value = false
  }
}

async function handleTableChange() {
  if (!formData.table) return

  try {
    const response = await getTableFields(formData.engineID, formData.schema, formData.table)
    // response 已经是 { data: [...] } 格式，直接访问 data 字段
    const fieldList = Array.isArray(response?.data) ? response.data : (response || [])
    props.wizardState.loadTargetFields(fieldList)
  } catch (error) {
    ElMessage.error('加载表字段失败')
  }
}

// 从 wizardState 恢复之前的状态
async function restoreState() {
  const state = props.wizardState

  // 恢复目标类型
  if (state.targetType.value) {
    targetType.value = state.targetType.value
  }

  // 恢复对象存储配置
  if (state.targetConfig.value) {
    const config = state.targetConfig.value
    if (config.output_format) outputFormat.value = config.output_format
    if (config.output_path) outputPath.value = config.output_path
    if (config.csv_headers !== undefined) csvHeaders.value = config.csv_headers
    if (config.csv_delimiter) csvDelimiter.value = config.csv_delimiter
    if (config.geometry_field) geometryField.value = config.geometry_field
  }

  // 恢复引擎、schema、表选择
  if (state.targetEngineID.value) {
    formData.engineID = state.targetEngineID.value

    // 延迟恢复，等待引擎加载完成
    await nextTick()

    // 恢复 schema（如果需要）
    if (state.targetSchema.value && needsSchema.value) {
      // 先加载 schemas 列表
      loadingSchemas.value = true
      try {
        const response = await getSchemas(formData.engineID)
        // response 已经是 { data: [...] } 格式，直接访问 data 字段
        const schemaList = Array.isArray(response?.data) ? response.data : (response || [])
        schemas.value = schemaList
        formData.schema = state.targetSchema.value
      } catch (error) {
        ElMessage.error('恢复 Schema 失败')
      } finally {
        loadingSchemas.value = false
      }
    }

    // 恢复表选择
    if (state.targetTable.value) {
      // 先加载表列表
      await loadTables()
      formData.table = state.targetTable.value
    }
  }
}

// Lifecycle
onMounted(async () => {
  await loadTargetEngines()
  await restoreState()
})
</script>

<style scoped>
.step2-select-target {
  max-width: 800px;
  margin: 0 auto;
}

.step-description {
  color: #606266;
  margin-bottom: 30px;
}
</style>
