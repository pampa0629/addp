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

  const sourceConfig = ref({})
  const sourceEngineID = ref(null)
  const sourceEngineType = ref('')
  const sourceSchema = ref('')
  const sourceTable = ref('')
  const sourceType = ref('postgresql')
  const sourceDataType = ref('table')
  const sourceRepresentation = ref('native')
  const sourceFormat = ref('')
  const sourceResource = ref(null)

  // Target 配置
  const targetConfig = ref({})
  const targetEngineID = ref(null)
  const targetEngineType = ref('')
  const targetSchema = ref('')
  const targetTable = ref('')
  const targetType = ref('nfs')
  const targetRepresentation = ref('encoded')

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
        return !!(sourceEngineID.value && isSupportedSourceShape() && (sourceResource.value || sourceTable.value))
      case 1: // 选择Target
        if (targetRepresentation.value === 'native') {
          return !!(targetEngineID.value && targetSchema.value && targetTable.value)
        }
        return !!(targetEngineID.value && targetConfig.value?.resourceFile)
      case 2: // 字段映射
        return fieldMappings.value.length > 0 || sourceFields.value.length === 0
      case 3: // 配置
        return taskName.value.trim() !== ''
      default:
        return true
    }
  })

  const taskConfig = computed(() => {
    const sourceEndpoint = buildSourceEndpoint()
    const targetEndpoint = buildTargetEndpoint()

    const config = {
      name: taskName.value,
      description: taskDescription.value,
      task_type: taskTypeForShape(sourceEndpoint, targetEndpoint),
      config: {
        mode: 'batch',
        source: sourceEndpoint,
        target: targetEndpoint,
        batch_size: batchSize.value
      },
      schedule: schedule.value,
      enabled: schedule.value ? enabled.value : false, // 只有设置了定时任务才考虑 enabled
      batch_size: batchSize.value,
      mappings: fieldMappings.value,
      auto_scan_metadata: true
    }

    return config
  })

  function buildSourceEndpoint() {
    const config = sourceConfig.value || {}
    const endpoint = {
      engine: {
        scope: 'system',
        id: Number(sourceEngineID.value),
        type: sourceEngineType.value || sourceType.value
      },
      resource: sourceResource.value || {
        kind: 'native_table',
        path: {
          schema: sourceSchema.value,
          table: sourceTable.value
        }
      },
      data_type: sourceDataType.value || 'table',
      representation: sourceRepresentation.value || 'native'
    }

    const format = sourceBackendFormat(sourceFormat.value || config.format)
    if (endpoint.representation === 'encoded' && format) {
      endpoint.format = format
      endpoint.options = sourceBackendOptions(config)
    }
    return endpoint
  }

  function sourceBackendFormat(uiFormat) {
    if (!uiFormat) return ''
    return targetBackendFormat({ format: uiFormat })
  }

  function sourceBackendOptions(config) {
    const uiFormat = String(config.format || sourceFormat.value || '').toLowerCase()
    if (uiFormat === 'jsonl') {
      return { json_mode: 'jsonl' }
    }
    if (uiFormat === 'geojson') {
      return compactOptions({
        'spatial.target_encoding': 'geojson',
        geometry_field: config.geometryField
      })
    }
    return compactOptions(config.options || {})
  }

  function taskTypeForShape(sourceEndpoint, targetEndpoint) {
    if (sourceEndpoint?.representation === 'encoded' && targetEndpoint?.representation === 'native') return 'import'
    if (sourceEndpoint?.representation === 'native' && targetEndpoint?.representation === 'encoded') return 'export'
    return 'transfer'
  }

  function isSupportedSourceShape() {
    if (sourceDataType.value !== 'table') return false
    if (sourceRepresentation.value === 'native') return true
    if (sourceRepresentation.value === 'encoded') {
      return supportedEncodedSourceFormat(sourceFormat.value)
    }
    return false
  }

  function supportedEncodedSourceFormat(format) {
    return ['csv', 'tsv', 'json', 'jsonl', 'geojson', 'parquet'].includes(String(format || '').toLowerCase())
  }

  function buildTargetEndpoint() {
    const fileConfig = targetConfig.value || {}
    if (targetRepresentation.value === 'native') {
      return {
        engine: {
          scope: 'system',
          id: Number(targetEngineID.value),
          type: targetEngineType.value || targetType.value
        },
        resource: {
          kind: 'native_table',
          path: {
            schema: targetSchema.value,
            table: targetTable.value
          }
        },
        data_type: 'table',
        representation: 'native',
        policy: {
          write_mode: fileConfig.writeMode || 'create_if_not_exists'
        }
      }
    }

    const format = targetBackendFormat(fileConfig)
    const endpoint = {
      engine: {
        scope: 'system',
        id: Number(targetEngineID.value),
        type: targetEngineType.value || targetType.value
      },
      resource: buildEncodedTargetResource(fileConfig),
      data_type: 'table',
      representation: 'encoded',
      format,
      policy: {
        write_mode: fileConfig.writeMode || 'overwrite'
      },
      options: targetBackendOptions(fileConfig)
    }
    return endpoint
  }

  function targetBackendFormat(fileConfig) {
    const uiFormat = String(fileConfig.format || 'csv').toLowerCase()
    if (uiFormat === 'jsonl' || uiFormat === 'geojson') return 'json'
    return uiFormat
  }

  function targetBackendOptions(fileConfig) {
    const uiFormat = String(fileConfig.format || 'csv').toLowerCase()
    switch (uiFormat) {
      case 'csv':
        return {
          header: fileConfig.includeHeader !== false,
          delimiter: fileConfig.delimiter || ','
        }
      case 'tsv':
        return {
          header: fileConfig.includeHeader !== false,
          delimiter: '\t'
        }
      case 'jsonl':
        return {
          json_mode: 'jsonl'
        }
      case 'json':
        return {
          json_mode: 'array'
        }
      case 'geojson':
        return compactOptions({
          'spatial.target_encoding': 'geojson',
          geometry_field: fileConfig.geometryField
        })
      case 'shapefile':
        return compactOptions({
          geometry_field: fileConfig.geometryField,
          geometry_type: fileConfig.geometryType
        })
      default:
        return {}
    }
  }

  function compactOptions(options) {
    return Object.fromEntries(
      Object.entries(options).filter(([, value]) => value !== undefined && value !== null && String(value).trim() !== '')
    )
  }

  function buildEncodedTargetResource(fileConfig) {
    const outputPath = trimSlashes(fileConfig.resourcePath || '')
    const outputFileName = trimSlashes(fileConfig.resourceFile || '')
    const fullPath = [outputPath, outputFileName].filter(Boolean).join('/')

    if (targetType.value === 's3') {
      const parts = fullPath.split('/').filter(Boolean)
      return {
        kind: 'object',
        path: {
          bucket: parts.shift() || '',
          path: parts.join('/')
        }
      }
    }

    return {
      kind: 'file',
      path: {
        path: fullPath
      }
    }
  }

  function trimSlashes(value) {
    return String(value || '').trim().replace(/^\/+|\/+$/g, '')
  }

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
    const extra = config.extra || {}
    const nextResource = config.resource || extra.resource || null
    const sourceChanged = sourceEngineID.value !== config.engineID ||
      JSON.stringify(sourceResource.value || null) !== JSON.stringify(nextResource)

    sourceEngineID.value = config.engineID
    sourceEngineType.value = config.engineType || ''
    sourceSchema.value = config.schema || extra.schema || ''
    sourceTable.value = config.table || extra.table || ''
    sourceType.value = config.sourceType || 'postgresql'
    sourceDataType.value = config.dataType || extra.dataType || 'table'
    sourceRepresentation.value = config.representation || extra.representation || 'native'
    sourceFormat.value = config.format || extra.format || ''
    sourceResource.value = nextResource
    sourceConfig.value = extra

    if (sourceChanged) {
      sourceFields.value = []
      targetFields.value = []
      fieldMappings.value = []
      targetEngineID.value = null
      targetEngineType.value = ''
      targetSchema.value = ''
      targetTable.value = ''
      targetConfig.value = {}
      targetRepresentation.value = sourceRepresentation.value === 'encoded' ? 'native' : 'encoded'
    }
  }

  function loadSourceFields(fields) {
    sourceFields.value = Array.isArray(fields) ? fields : []
    // 自动初始化字段映射
    if (sourceFields.value.length === 0) {
      fieldMappings.value = []
      return
    }
    if (fieldMappings.value.length === 0) {
      autoGenerateFieldMappings()
    }
  }

  // Target 配置
  function updateTarget(config) {
    const extra = config.extra || {}
    targetEngineID.value = config.engineID
    targetEngineType.value = config.engineType || ''
    targetSchema.value = config.schema || extra.schema || ''
    targetTable.value = config.table || extra.table || ''
    targetType.value = config.targetType || 'nfs'
    targetRepresentation.value = config.representation || 'encoded'
    targetConfig.value = extra
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
      const created = await taskAPI.create(taskConfig.value)
      const task = created?.data || created
      if (!schedule.value && task?.id) {
        try {
          await taskAPI.start(task.id)
          ElMessage.success(t('transfer.taskWizard.taskCreateAndStartSuccess'))
        } catch (startError) {
          const startMessage = startError.response?.data?.error || startError.response?.data?.message || startError.message || t('transfer.taskWizard.taskCreateAndStartFailed')
          ElMessage.warning(startMessage)
        }
      } else {
        ElMessage.success(t('transfer.taskWizard.taskCreateSuccess'))
      }
      return true
    } catch (error) {
      const message = error.response?.data?.error || error.response?.data?.message || error.message || t('transfer.taskWizard.taskCreateFailed')
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
      sourceEngineID.value = source.engine?.id || null
      sourceEngineType.value = source.engine?.type || ''
      sourceSchema.value = source.resource?.path?.schema || ''
      sourceTable.value = source.resource?.path?.table || source.resource?.path?.name || ''
      sourceType.value = normalizeEngineType(sourceEngineType.value || 'postgresql')
      sourceDataType.value = source.data_type || 'table'
      sourceRepresentation.value = source.representation || 'native'
      sourceFormat.value = targetUiFormat(source.format, source.options || {})
      sourceResource.value = source.resource || null
      sourceConfig.value = extractSourceConfig(source)
    }

    // Target 配置
    if (task.config?.target) {
      const target = task.config.target
      targetEngineID.value = target.engine?.id || null
      targetEngineType.value = target.engine?.type || ''
      targetSchema.value = target.resource?.path?.schema || ''
      targetTable.value = target.resource?.path?.table || target.resource?.path?.name || ''
      targetType.value = normalizeTargetType(target)
      targetRepresentation.value = target.representation || 'encoded'
      targetConfig.value = extractTargetFileConfig(target)
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

  function normalizeEngineType(engineType) {
    const type = String(engineType || '').toLowerCase()
    if (type.includes('postgres')) return 'postgresql'
    if (type.includes('mysql')) return 'mysql'
    if (type.includes('s3') || type.includes('minio')) return 's3'
    return type || 'postgresql'
  }

  function normalizeTargetType(target) {
    if (target.resource?.kind === 'file') return 'nfs'
    if (target.resource?.kind === 'object') return 's3'
    return 'nfs'
  }

  function extractTargetFileConfig(target) {
    if (target.resource?.kind !== 'file' && target.resource?.kind !== 'object') {
      return {}
    }

    const rawPath = target.resource?.path || {}
    const path = target.resource.kind === 'object'
      ? [rawPath.bucket, rawPath.path].filter(Boolean).join('/')
      : rawPath.path || ''
    const { dir, file } = splitPath(path)
    const options = target.options || {}
    const format = targetUiFormat(target.format, options)

    return {
      format,
      resourcePath: dir,
      resourceFile: file,
      includeHeader: target.options?.header !== false,
      delimiter: target.options?.delimiter || ',',
      geometryField: options.geometry_field || '',
      geometryType: options.geometry_type || '',
      writeMode: target.policy?.write_mode || 'overwrite'
    }
  }

  function extractSourceConfig(source) {
    const resource = source.resource || {}
    const path = resource.path || {}
    const label = resource.kind === 'native_table'
      ? [path.schema, path.table].filter(Boolean).join('.')
      : resource.kind === 'object'
        ? [path.bucket, path.path].filter(Boolean).join('/')
        : path.path || ''
    return {
      sourceLabel: label,
      catalogPath: label,
      dataType: source.data_type || 'table',
      representation: source.representation || 'native',
      format: targetUiFormat(source.format, source.options || {}),
      resource
    }
  }

  function targetUiFormat(format, options = {}) {
    const normalized = String(format || 'csv').toLowerCase()
    if (normalized !== 'json') return normalized

    const spatialEncoding = String(options['spatial.target_encoding'] || options.target_encoding || '').toLowerCase()
    if (spatialEncoding === 'geojson') return 'geojson'

    const jsonMode = String(options.json_mode || options.layout || '').toLowerCase()
    if (jsonMode === 'jsonl' || jsonMode === 'lines' || jsonMode === 'ndjson') return 'jsonl'

    return 'json'
  }

  function splitPath(path) {
    const cleaned = trimSlashes(path)
    if (!cleaned) return { dir: '', file: '' }
    const parts = cleaned.split('/')
    const file = parts.pop() || ''
    return { dir: parts.join('/'), file }
  }

  // 更新任务
  async function updateTask(taskId) {
    try {
      // 更新任务
      await taskAPI.update(taskId, taskConfig.value)
      ElMessage.success(t('transfer.taskWizard.taskUpdateSuccess'))
      return true
    } catch (error) {
      const message = error.response?.data?.error || error.response?.data?.message || error.message || t('transfer.taskWizard.taskUpdateFailed')
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
    sourceEngineType.value = ''
    sourceSchema.value = ''
    sourceTable.value = ''
    sourceType.value = 'postgresql'
    sourceDataType.value = 'table'
    sourceRepresentation.value = 'native'
    sourceFormat.value = ''
    sourceResource.value = null
    sourceConfig.value = {}
    targetEngineID.value = null
    targetEngineType.value = ''
    targetSchema.value = ''
    targetTable.value = ''
    targetType.value = 'nfs'
    targetRepresentation.value = 'encoded'
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
    sourceEngineType,
    sourceSchema,
    sourceTable,
    sourceType,
    sourceDataType,
    sourceRepresentation,
    sourceFormat,
    sourceResource,
    targetConfig,
    targetEngineID,
    targetEngineType,
    targetSchema,
    targetTable,
    targetType,
    targetRepresentation,
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
