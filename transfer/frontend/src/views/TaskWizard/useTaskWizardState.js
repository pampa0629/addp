/**
 * TaskWizard 状态管理
 * 职责：集中管理向导的所有状态和业务逻辑
 */

import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { taskAPI } from '@/api/tasks'

export function useTaskWizardState() {
  const { t } = useI18n()
  // ===== 状态定义 =====
  const currentStep = ref(0)
  const taskName = ref('')
  const taskDescription = ref('')
  const schedule = ref('')
  const enabled = ref(false) // 定时任务启用状态
  const batchSize = ref(1000) // 批大小，默认 1000

  // Source 配置
  const sourceObjectPath = ref('') // S3/Parquet 路径
  const sourceObjectFile = ref('') // S3/Parquet 文件（可选）
  const sourceConfig = ref({})
  const sourceEngineID = ref(null)
  const sourceScope = ref('system') // 'system' 或 'local'，默认为 'system'
  const sourceSchema = ref('')
  const sourceTable = ref('')
  const sourceType = ref('postgresql') // postgresql, mysql, spatialite, s3, parquet
  const sourceQueryMode = ref('table') // table, sql
  const sourceSQLQuery = ref('') // SQL 查询语句

  // Target 配置
  const targetConfig = ref({})
  const targetEngineID = ref(null)
  const targetScope = ref('system') // 'system' 或 'local'，默认为 'system'
  const targetSchema = ref('')
  const targetTable = ref('')
  const targetType = ref('postgresql') // postgresql, mysql, s3

  // 字段映射
  const fieldMappings = ref([])
  const sourceFields = ref([])
  const targetFields = ref([])

  // 转换配置
  const transforms = ref([])

  // ===== 计算属性 =====
  const canGoNext = computed(() => {
    switch (currentStep.value) {
      case 0: // 选择Source
        return sourceEngineID.value && sourceTable.value
      case 1: // 选择Target
        if (targetType.value === 's3') {
          return !!(targetEngineID.value && targetConfig.value?.output_path && targetConfig.value?.output_file_name)
        }
        return targetEngineID.value && targetTable.value
      case 2: // 字段映射
        return fieldMappings.value.length > 0
      case 3: // 配置
        return taskName.value.trim() !== ''
      default:
        return true
    }
  })

  const taskConfig = computed(() => {
    // 构建 source 配置
    const sourceConfigObj = {
      scope: sourceScope.value,
      engine_id: sourceEngineID.value,
      connector_type: sourceType.value,
      ...sourceConfig.value
    }

    // 构建 target 配置
    const targetConfigObj = {
      scope: targetScope.value,
      engine_id: targetEngineID.value,
      connector_type: targetType.value,
      ...targetConfig.value
    }

    // 根据后端 CreateTaskRequest 结构构建请求
    const config = {
      name: taskName.value,
      description: taskDescription.value,
      config: {
        source: sourceConfigObj,
        target: targetConfigObj
      },
      schedule: schedule.value,
      enabled: schedule.value ? enabled.value : false, // 只有设置了定时任务才考虑 enabled
      batch_size: batchSize.value,
      mappings: fieldMappings.value,
      auto_scan_metadata: true // 默认自动扫描元数据
    }

    // Source 配置：根据查询模式添加不同字段
    if (sourceType.value === 'parquet') {
      // Parquet 数据源：指定路径（目录或单文件）
      config.config.source.connector_type = 'parquet'
      // prefix 为目录路径，file_name 为具体文件（可选）
      const path = sourceObjectFile.value || sourceObjectPath.value
      config.config.source.prefix = sourceObjectPath.value
      if (sourceObjectFile.value) {
        config.config.source.file_name = sourceObjectFile.value
      }
      config.config.source.path = path
    } else if (sourceQueryMode.value === 'sql') {
      config.config.source.query_type = 'sql'
      config.config.source.query = sourceSQLQuery.value
    } else {
      config.config.source.query_type = 'table'
      config.config.source.schema = sourceSchema.value
      config.config.source.table = sourceTable.value
    }

    // Target 配置：根据目标类型添加不同字段
    if (targetType.value === 's3') {
      // 对象存储配置（targetConfig 中已是 snake_case，直接读取）
      const s3Config = targetConfig.value || {}
      config.config.target.output_format = s3Config.output_format || 'csv'
      config.config.target.output_path = s3Config.output_path || ''
      config.config.target.output_file_name = s3Config.output_file_name || ''

      // CSV 专用选项
      if (s3Config.output_format === 'csv') {
        config.config.target.csv_headers = s3Config.csv_headers !== false
        config.config.target.csv_delimiter = s3Config.csv_delimiter || ','
      }

      // 空间格式需要几何字段
      if (['geojson', 'shapefile'].includes(s3Config.output_format) && s3Config.geometry_field) {
        config.config.target.geometry_field = s3Config.geometry_field
      }
    } else {
      // 数据库配置
      config.config.target.schema = targetSchema.value
      config.config.target.table = targetTable.value
    }

    return config
  })

  // ===== 方法 =====

  // 步骤导航
  function nextStep() {
    if (canGoNext.value && currentStep.value < 4) {
      currentStep.value++
    }
  }

  function prevStep() {
    if (currentStep.value > 0) {
      currentStep.value--
    }
  }

  function goToStep(step) {
    if (step >= 0 && step <= 4) {
      currentStep.value = step
    }
  }

  // Source 配置
  function updateSource(config) {
    sourceEngineID.value = config.engineID
    sourceScope.value = config.scope || 'system'
    sourceSchema.value = config.schema || ''
    sourceTable.value = config.table || ''
    sourceType.value = config.sourceType || 'postgresql'
    sourceQueryMode.value = config.queryMode || 'table'
    sourceSQLQuery.value = config.sqlQuery || ''
    sourceObjectPath.value = config.objectPath || ''
    sourceObjectFile.value = config.objectFile || ''
    sourceConfig.value = config.extra || {}
  }

  function loadSourceFields(fields) {
    sourceFields.value = fields
    // 自动初始化字段映射
    if (fieldMappings.value.length === 0) {
      autoGenerateFieldMappings()
    }
  }

  // Target 配置
  function updateTarget(config) {
    targetEngineID.value = config.engineID
    targetScope.value = config.scope || 'system'
    targetSchema.value = config.schema || ''
    targetTable.value = config.table || ''
    targetType.value = config.targetType || 'postgresql'
    targetConfig.value = config.extra || {}
  }

  function loadTargetFields(fields) {
    targetFields.value = fields
  }

  // 字段映射
  function autoGenerateFieldMappings() {
    if (sourceFields.value.length === 0) return

    fieldMappings.value = sourceFields.value.map(field => ({
      source_field: field.name,
      target_field: field.name, // 默认同名映射
      field_type: field.standard_type || field.data_type || 'string',
      format: '',
      default_value: '',
      nullable: true
    }))
  }

  function updateFieldMapping(index, mapping) {
    fieldMappings.value[index] = mapping
  }

  function addFieldMapping() {
    fieldMappings.value.push({
      source_field: '',
      target_field: '',
      field_type: 'string',
      format: '',
      default_value: '',
      nullable: true
    })
  }

  function removeFieldMapping(index) {
    fieldMappings.value.splice(index, 1)
  }

  // 转换配置
  function addTransform(transform) {
    transforms.value.push(transform)
  }

  function removeTransform(index) {
    transforms.value.splice(index, 1)
  }

  // 提交任务
  async function submitTask() {
    try {
      // 创建任务
      await taskAPI.create(taskConfig.value)
      ElMessage.success(t('transfer.taskWizard.taskCreateSuccess'))
      return true
    } catch (error) {
      const message = error.response?.data?.message || error.message || t('transfer.taskWizard.taskCreateFailed')
      ElMessage.error(message)
      return false
    }
  }

  // 从任务详情加载数据（编辑模式）
  function loadFromTask(task) {
    if (!task) return

    // 基本信息
    taskName.value = task.name || ''
    taskDescription.value = task.description || ''
    schedule.value = task.schedule || ''
    enabled.value = task.enabled || false
    batchSize.value = task.batch_size || 1000

    // Source 配置
    if (task.config?.source) {
      const source = task.config.source
      sourceEngineID.value = source.engine_id || null
      sourceScope.value = source.scope || 'system'
      sourceSchema.value = source.schema || ''
      sourceTable.value = source.table || ''
      sourceType.value = source.connector_type || 'postgresql'
      sourceQueryMode.value = source.query_type || 'table'
      sourceSQLQuery.value = source.query || ''

      // 额外的 source 配置
      const extraSourceFields = { ...source }
      delete extraSourceFields.scope
      delete extraSourceFields.engine_id
      delete extraSourceFields.connector_type
      delete extraSourceFields.schema
      delete extraSourceFields.table
      delete extraSourceFields.query_type
      delete extraSourceFields.query
      sourceConfig.value = extraSourceFields
    }

    // Target 配置
    if (task.config?.target) {
      const target = task.config.target
      targetEngineID.value = target.engine_id || null
      targetScope.value = target.scope || 'system'
      targetSchema.value = target.schema || ''
      targetTable.value = target.table || ''
      targetType.value = target.connector_type || 'postgresql'

      // 额外的 target 配置
      const extraTargetFields = { ...target }
      delete extraTargetFields.scope
      delete extraTargetFields.engine_id
      delete extraTargetFields.connector_type
      delete extraTargetFields.schema
      delete extraTargetFields.table
      targetConfig.value = extraTargetFields
    }

    // 字段映射
    if (Array.isArray(task.mappings)) {
      fieldMappings.value = task.mappings.map(m => ({
        source_field: m.source_field || '',
        target_field: m.target_field || '',
        field_type: m.field_type || 'string',
        format: m.format || '',
        default_value: m.default_value || '',
        nullable: m.nullable !== false
      }))
    }
  }

  // 更新任务
  async function updateTask(taskId) {
    try {
      // 更新任务
      await taskAPI.update(taskId, taskConfig.value)
      ElMessage.success(t('transfer.taskWizard.taskUpdateSuccess'))
      return true
    } catch (error) {
      const message = error.response?.data?.message || error.message || t('transfer.taskWizard.taskUpdateFailed')
      ElMessage.error(message)
      return false
    }
  }

  // 重置状态
  function reset() {
    currentStep.value = 0
    taskName.value = ''
    taskDescription.value = ''
    schedule.value = ''
    enabled.value = false
    batchSize.value = 1000
    sourceEngineID.value = null
    sourceScope.value = 'system'
    sourceSchema.value = ''
    sourceTable.value = ''
    sourceType.value = 'postgresql'
    sourceQueryMode.value = 'table'
    sourceSQLQuery.value = ''
    sourceObjectPath.value = ''
    sourceObjectFile.value = ''
    sourceConfig.value = {}
    targetEngineID.value = null
    targetScope.value = 'system'
    targetSchema.value = ''
    targetTable.value = ''
    targetType.value = 'postgresql'
    targetConfig.value = {}
    fieldMappings.value = []
    sourceFields.value = []
    targetFields.value = []
    transforms.value = []
  }

  return {
    // 状态
    currentStep,
    taskName,
    taskDescription,
    schedule,
    enabled,
    batchSize,
    sourceConfig,
    sourceEngineID,
    sourceScope,
    sourceSchema,
    sourceTable,
    sourceType,
    sourceQueryMode,
    sourceSQLQuery,
    sourceObjectPath,
    sourceObjectFile,
    targetConfig,
    targetEngineID,
    targetScope,
    targetSchema,
    targetTable,
    targetType,
    fieldMappings,
    sourceFields,
    targetFields,
    transforms,

    // 计算属性
    canGoNext,
    taskConfig,

    // 方法
    nextStep,
    prevStep,
    goToStep,
    updateSource,
    loadSourceFields,
    updateTarget,
    loadTargetFields,
    autoGenerateFieldMappings,
    updateFieldMapping,
    addFieldMapping,
    removeFieldMapping,
    addTransform,
    removeTransform,
    loadFromTask,
    submitTask,
    updateTask,
    reset
  }
}
