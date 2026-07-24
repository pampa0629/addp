/**
 * TaskWizard 状态管理
 * 职责：集中管理向导的所有状态和业务逻辑
 */

import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { formatLocatorDisplayPath } from '@addp/common-frontend'
import { taskAPI } from '@/api/tasks'
import { parseTransferLocator } from '@/utils/resourceLocator'
import {
  buildContinuousSourceEndpoint,
  buildDatabaseCDCSourceEndpoint,
	cdcMappingsCoverSourceFields,
  continuousMappedTargetKeys,
	continuousMappingsValid,
	databaseCDCMappingsValid,
	databaseCDCUnavailableReasonCodes,
  isKafkaTopicSource,
	normalizeContinuousKeyFields
} from './continuousTask.mjs'

export function useTaskWizardState() {
  const { t } = useI18n()
  // ===== 状态定义 =====
  const currentStep = ref(0)
  const taskName = ref('')
  const taskDescription = ref('')
  const schedule = ref('')
  const enabled = ref(false) // 定时任务启用状态
  const batchSize = ref(1000) // 批大小，默认 1000
  const runtimeBoundary = ref('bounded')
  const loadMode = ref('snapshot')
  const watermarkField = ref('')
  const watermarkTieBreakers = ref([])
  const targetKeys = ref([])
  const continuousKeyFields = ref([])
  const continuousInitialPosition = ref('earliest')
  const continuousPollBatchSize = ref(1000)

  const sourceConfig = ref({})
  const sourceEngineID = ref(null)
  const sourceEngineType = ref('')
  const sourceSchema = ref('')
  const sourceTable = ref('')
  const sourceType = ref('postgresql')
  const sourceDataType = ref('table')
  const sourceRepresentation = ref('native')
  const sourceFormat = ref('')
  const sourceLocator = ref('')
  const readableEncodedFormats = ref(new Set())
  const rawCopyFormats = ref(new Map())
  const formatCapabilitiesLoaded = ref(false)
	const databaseCDCCapability = ref(null)

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
  const isRawCopyTask = computed(() => {
    return isRawCopyShape({
      dataType: sourceDataType.value,
      representation: sourceRepresentation.value,
      format: sourceFormat.value,
      locator: sourceLocator.value
    })
  })

  const supportsWatermarkIncremental = computed(() => {
    return isPostgresqlEngineType(sourceEngineType.value) &&
      sourceRepresentation.value === 'native' &&
      sourceDataType.value === 'table' &&
      isPostgresqlEngineType(targetEngineType.value) &&
      targetRepresentation.value === 'native' &&
      !isRawCopyTask.value
  })

  const isContinuousTask = computed(() => runtimeBoundary.value === 'continuous')
	const isKafkaContinuousTask = computed(() => {
		return isContinuousTask.value && isKafkaTopicSource(sourceEngineType.value, sourceLocator.value)
	})

	const databaseCDCUnavailableReasons = computed(() => databaseCDCUnavailableReasonCodes({
		sourceEngineType: sourceEngineType.value,
		sourceLocator: sourceLocator.value,
		sourceRepresentation: sourceRepresentation.value,
		sourceDataType: sourceDataType.value,
		targetEngineType: targetEngineType.value,
		targetRepresentation: targetRepresentation.value,
		sourceFields: sourceFields.value,
		databaseCDCCapability: databaseCDCCapability.value
	}))

	const supportsDatabaseCDC = computed(() => databaseCDCUnavailableReasons.value.length === 0)

	const isDatabaseCDCTask = computed(() => {
		return isContinuousTask.value && loadMode.value === 'cdc'
	})

  const supportsContinuousTarget = computed(() => {
    return isContinuousTask.value &&
      isPostgresqlEngineType(targetEngineType.value) &&
      targetRepresentation.value === 'native'
  })

  const isWatermarkIncremental = computed(() => runtimeBoundary.value === 'bounded' && loadMode.value === 'incremental')

  const continuousTargetKeys = computed(() => {
    return continuousMappedTargetKeys(fieldMappings.value, continuousKeyFields.value)
  })

  const continuousConfigValid = computed(() => {
		const sourceValid = isDatabaseCDCTask.value
			? supportsDatabaseCDC.value
			: isKafkaTopicSource(sourceEngineType.value, sourceLocator.value)
		const mappingsValid = isDatabaseCDCTask.value
			? databaseCDCMappingsValid(fieldMappings.value, continuousKeyFields.value, sourceEngineType.value)
			: continuousMappingsValid(fieldMappings.value, continuousKeyFields.value)
		return sourceValid &&
      supportsContinuousTarget.value &&
			mappingsValid &&
			(!isDatabaseCDCTask.value || cdcMappingsCoverSourceFields(fieldMappings.value, sourceFields.value)) &&
      transforms.value.length === 0 &&
      continuousPollBatchSize.value > 0 &&
			(isDatabaseCDCTask.value || ['earliest', 'latest'].includes(continuousInitialPosition.value))
  })

  const watermarkIncrementalValid = computed(() => {
    const field = String(watermarkField.value || '').trim()
    const tieBreakers = normalizedFieldNames(watermarkTieBreakers.value)
    const keys = normalizedFieldNames(targetKeys.value)
    const mappedTargets = normalizedFieldNames(fieldMappings.value.map(mapping => mapping?.target_field))
    return supportsWatermarkIncremental.value &&
      !!field &&
      tieBreakers.length > 0 &&
      !tieBreakers.some(name => sameFieldName(name, field)) &&
      keys.length > 0 &&
      keys.every(key => mappedTargets.some(target => sameFieldName(target, key)))
  })

  const canGoNext = computed(() => {
    switch (currentStep.value) {
      case 0: // 选择Source
        return !!(sourceEngineID.value && isSupportedSourceShape() && sourceLocator.value)
      case 1: // 选择Target
        if (targetRepresentation.value === 'native') {
          return !!(targetEngineID.value && targetConfig.value?.parentLocator && targetTable.value)
        }
        const hasTargetFormat = isRawCopyTask.value || !!targetBackendFormat(targetConfig.value || {})
        return !!(
          targetEngineID.value &&
          hasTargetFormat &&
          targetConfig.value?.parentLocator &&
          targetConfig.value?.resourceFile &&
          !targetConfig.value?.extensionError
        )
      case 2: // 字段映射
        if (isRawCopyTask.value) return true
        return fieldMappings.value.length > 0 || sourceFields.value.length === 0
      case 3: // 配置
        return taskName.value.trim() !== '' &&
          (!isContinuousTask.value || continuousConfigValid.value) &&
          (!isWatermarkIncremental.value || watermarkIncrementalValid.value)
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
      task_type: 'sync',
      config: {
        runtime: isContinuousTask.value
          ? { boundary: runtimeBoundary.value, record_failure: { mode: 'block' } }
          : { boundary: runtimeBoundary.value },
        load: buildLoadConfig(),
        source: sourceEndpoint,
        target: targetEndpoint,
        transforms: buildTransformsConfig()
      },
      schedule: isContinuousTask.value ? '' : schedule.value,
      enabled: isContinuousTask.value ? false : (schedule.value ? enabled.value : false),
      batch_size: isContinuousTask.value ? continuousPollBatchSize.value : batchSize.value,
      auto_scan_metadata: !isContinuousTask.value
    }

    if (!isContinuousTask.value) {
      config.config.batch_size = batchSize.value
    }

    return config
  })

  function buildLoadConfig() {
		if (isDatabaseCDCTask.value) {
			return {
				mode: 'incremental',
				change_detection: { type: 'cdc', bootstrap: 'initial_snapshot' }
			}
		}
    if (isContinuousTask.value) {
      return {
        mode: 'incremental',
        change_detection: { type: 'kafka' }
      }
    }
    if (!isWatermarkIncremental.value) {
      return { mode: 'snapshot' }
    }
    return {
      mode: 'incremental',
      change_detection: {
        type: 'watermark',
        field: String(watermarkField.value || '').trim(),
        tie_breaker: normalizedFieldNames(watermarkTieBreakers.value),
        start: 'committed',
        end: 'execution_upper_bound'
      }
    }
  }

  function buildTransformsConfig() {
    if (isRawCopyTask.value) return []
    const result = []
    const fieldMapping = buildFieldMappingTransform()
    if (fieldMapping) {
      result.push(fieldMapping)
    }
    result.push(...transforms.value)
    return result
  }

  function buildFieldMappingTransform() {
    const fields = fieldMappings.value
      .filter(mapping => String(mapping.target_field || '').trim())
      .map(mapping => {
        const field = {
          target: String(mapping.target_field || '').trim(),
          nullable: mapping.nullable !== false
        }
        const source = String(mapping.source_field || '').trim()
        if (source) field.source = source
        if (mapping.target_type) field.target_type = mapping.target_type
        if (mapping.default_value !== undefined && mapping.default_value !== null && String(mapping.default_value).trim() !== '') {
          field.default = mapping.default_value
        }
        if (mapping.format) field.format = mapping.format
        return field
      })

    if (fields.length === 0) return null
    return {
      type: 'field_mapping',
      version: 'v1',
      mode: 'project',
      fields
    }
  }

  function buildSourceEndpoint() {
    const config = sourceConfig.value || {}
		if (isDatabaseCDCTask.value) {
			return buildDatabaseCDCSourceEndpoint(sourceLocator.value)
		}
    if (isContinuousTask.value) {
      return buildContinuousSourceEndpoint(
        sourceLocator.value,
        continuousKeyFields.value,
        continuousInitialPosition.value,
        continuousPollBatchSize.value
      )
    }
    const endpoint = {
      locator: sourceLocator.value,
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
        geometry_field: config.geometryField
      })
    }
    return compactOptions(config.options || {})
  }

  function isSupportedSourceShape() {
    if (isKafkaTopicSource(sourceEngineType.value, sourceLocator.value)) return true
    if (sourceDataType.value === 'table') {
      if (sourceRepresentation.value === 'native') return true
      if (sourceRepresentation.value === 'encoded') {
        return supportedEncodedSourceFormat(sourceFormat.value)
      }
    }
    return isRawCopyTask.value
  }

  function supportedEncodedSourceFormat(format) {
    return readableEncodedFormats.value.has(String(format || '').toLowerCase())
  }

  function rawCopyDataTypeForFormat(format) {
    return rawCopyFormats.value.get(String(format || '').toLowerCase()) || ''
  }

  function updateFormatCapabilities(capabilities = null) {
    const readable = Array.isArray(capabilities?.readableEncodedFormats)
      ? capabilities.readableEncodedFormats
      : []
    readableEncodedFormats.value = new Set(
      readable.map(format => String(format || '').toLowerCase()).filter(Boolean)
    )

    const nextRawCopyFormats = new Map()
    const rawCopyEntries = capabilities?.rawCopyFormats
    if (rawCopyEntries instanceof Map) {
      rawCopyEntries.forEach((dataType, format) => {
        const value = String(format || '').toLowerCase()
        const normalizedDataType = String(dataType || '').toLowerCase()
        if (value && normalizedDataType) {
          nextRawCopyFormats.set(value, normalizedDataType)
        }
      })
    } else if (Array.isArray(rawCopyEntries)) {
      rawCopyEntries.forEach(entry => {
        const value = String(entry?.value || entry?.format || '').toLowerCase()
        const dataType = String(entry?.data_type || entry?.dataType || '').toLowerCase()
        if (value && dataType) {
          nextRawCopyFormats.set(value, dataType)
        }
      })
    }
    rawCopyFormats.value = nextRawCopyFormats
		databaseCDCCapability.value = capabilities?.databaseCDC || null
    formatCapabilitiesLoaded.value = capabilities !== null
  }

  function buildTargetEndpoint() {
    const fileConfig = targetConfig.value || {}
    if (targetRepresentation.value === 'native') {
      const policy = isContinuousTask.value
        ? {
						apply_mode: isDatabaseCDCTask.value ? 'upsert_delete' : 'upsert',
            keys: continuousTargetKeys.value
          }
        : isWatermarkIncremental.value
        ? {
            apply_mode: 'upsert',
            keys: normalizedFieldNames(targetKeys.value)
          }
        : {
            apply_mode: normalizeTableApplyMode(fileConfig.writeMode)
          }
      return {
        parent_locator: fileConfig.parentLocator || '',
        name: targetTable.value,
        data_type: 'table',
        representation: 'native',
        policy
      }
    }

    const format = targetBackendFormat(fileConfig)
    const dataType = isRawCopyTask.value ? sourceDataType.value : 'table'
    const endpoint = {
      parent_locator: buildEncodedTargetParentLocator(fileConfig),
      name: trimSlashes(fileConfig.resourceFile || ''),
      data_type: dataType,
      representation: 'encoded',
      format,
      policy: {
        apply_mode: 'replace'
      },
      options: targetBackendOptions(fileConfig)
    }
    return endpoint
  }

  function targetBackendFormat(fileConfig) {
    if (isRawCopyTask.value) {
      return String(fileConfig.backendFormat || fileConfig.format || sourceFormat.value || '').toLowerCase()
    }
    if (fileConfig.backendFormat) {
      return String(fileConfig.backendFormat).toLowerCase()
    }
    const uiFormat = String(fileConfig.format || '').toLowerCase()
    if (!uiFormat) return ''
    if (uiFormat === 'jsonl') return 'json'
    return uiFormat
  }

  function targetBackendOptions(fileConfig) {
    if (isRawCopyTask.value) return {}
    const backendOptions = compactOptions(fileConfig.backendOptions || {})
    const uiFormat = String(fileConfig.format || '').toLowerCase()
    switch (uiFormat) {
      case 'csv':
        return {
          ...backendOptions,
          header: fileConfig.includeHeader !== false,
          delimiter: fileConfig.delimiter || ','
        }
      case 'tsv':
        return {
          ...backendOptions,
          header: fileConfig.includeHeader !== false,
          delimiter: '\t'
        }
      case 'jsonl':
        return {
          ...backendOptions,
          json_mode: 'jsonl'
        }
      case 'json':
        return {
          ...backendOptions,
          json_mode: 'array'
        }
      case 'geojson':
        return compactOptions({
          ...backendOptions,
          geometry_field: fileConfig.geometryField
        })
      case 'shapefile':
        return compactOptions({
          ...backendOptions,
          geometry_field: fileConfig.geometryField,
          geometry_type: fileConfig.geometryType
        })
      default:
        return backendOptions
    }
  }

  function compactOptions(options) {
    return Object.fromEntries(
      Object.entries(options).filter(([, value]) => value !== undefined && value !== null && String(value).trim() !== '')
    )
  }

  function buildEncodedTargetParentLocator(fileConfig) {
    return fileConfig.parentLocator || ''
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
    const nextLocator = config.locator || extra.locator || ''
    const oldSourceEngineID = sourceEngineID.value
    const oldSourceLocator = sourceLocator.value
    const oldSourceEmpty = !oldSourceEngineID && !oldSourceLocator
    const sourceChanged = !oldSourceEmpty && (
      Number(oldSourceEngineID || 0) !== Number(config.engineID || 0) ||
      !sameLocatorIdentity(oldSourceLocator, nextLocator)
    )

    sourceEngineID.value = config.engineID
    sourceEngineType.value = config.engineType || ''
    sourceSchema.value = config.schema || extra.schema || ''
    sourceTable.value = config.table || extra.table || ''
    sourceType.value = config.sourceType || 'postgresql'
    sourceDataType.value = config.dataType || extra.dataType || 'table'
    sourceRepresentation.value = config.representation || extra.representation || 'native'
    sourceFormat.value = config.format || extra.format || ''
    sourceLocator.value = nextLocator
    sourceConfig.value = extra

    const nextContinuous = isKafkaTopicSource(sourceEngineType.value, sourceLocator.value)
    runtimeBoundary.value = nextContinuous ? 'continuous' : 'bounded'
    if (nextContinuous) {
      loadMode.value = 'incremental'
      schedule.value = ''
      enabled.value = false
      targetRepresentation.value = 'native'
    } else if (oldSourceEmpty || sourceChanged) {
      loadMode.value = 'snapshot'
    }

    if (sourceChanged) {
      sourceFields.value = []
      targetFields.value = []
      fieldMappings.value = []
      targetEngineID.value = null
      targetEngineType.value = ''
      targetSchema.value = ''
      targetTable.value = ''
      targetConfig.value = {}
      transforms.value = []
      targetRepresentation.value = nextContinuous
        ? 'native'
        : (isRawCopyTask.value ? 'encoded' : (sourceRepresentation.value === 'encoded' ? 'native' : 'encoded'))
      resetIncrementalConfig()
      continuousKeyFields.value = []
      continuousInitialPosition.value = 'earliest'
      continuousPollBatchSize.value = 1000
    }
  }

	function setLoadMode(mode) {
		if (isKafkaTopicSource(sourceEngineType.value, sourceLocator.value)) {
			loadMode.value = 'incremental'
			runtimeBoundary.value = 'continuous'
			return
		}
		if (mode === 'cdc') {
			if (!supportsDatabaseCDC.value) return
			loadMode.value = 'cdc'
			runtimeBoundary.value = 'continuous'
			schedule.value = ''
			enabled.value = false
			targetRepresentation.value = 'native'
			continuousInitialPosition.value = 'earliest'
			const primaryKeys = sourceFields.value.filter(isPrimaryKeyField).map(field => field.name)
			updateContinuousKeyFields(primaryKeys)
			return
		}
		loadMode.value = mode === 'incremental' ? 'incremental' : 'snapshot'
		runtimeBoundary.value = 'bounded'
	}

  function loadSourceFields(fields, attributes = null) {
    sourceFields.value = enrichSourceFieldsWithSpatialInfo(
      Array.isArray(fields) ? fields : [],
      attributes || sourceConfig.value?.sourceItem?.attributes || sourceConfig.value?.attributes || {}
    )
    // 自动初始化字段映射
    if (sourceFields.value.length === 0) {
      fieldMappings.value = []
      return
    }
    if (fieldMappings.value.length === 0) {
      autoGenerateFieldMappings()
    }
  }

  function enrichSourceFieldsWithSpatialInfo(fields, attributes) {
    const geometryColumns = sourceGeometryColumns(attributes)
    if (geometryColumns.length === 0) {
      return fields
    }
    return fields.map(field => {
      const column = geometryColumns.find(item => sameFieldName(item.name, field?.name))
      if (!column) return field
      const next = { ...field }
      if (shouldUseSpatialGeometryType(next.geometry_type || next.geometryType, column.geometry_type || column.geometryType)) {
        next.geometry_type = column.geometry_type || column.geometryType
      }
      if ((next.srid === undefined || next.srid === null || next.srid === '') && column.srid) {
        next.srid = column.srid
      }
      if ((next.dimension === undefined || next.dimension === null || next.dimension === '') && column.dimension) {
        next.dimension = column.dimension
      }
      if (!next.crs_ref && column.crs_ref) {
        next.crs_ref = column.crs_ref
      }
      return next
    })
  }

  function sourceGeometryColumns(attributes) {
    const spatial = attributes?.capabilities?.spatial || {}
    return Array.isArray(spatial.geometry_columns) ? spatial.geometry_columns : []
  }

  function sameFieldName(left, right) {
    return String(left || '').trim().toLowerCase() === String(right || '').trim().toLowerCase()
  }

  function shouldUseSpatialGeometryType(current, spatial) {
    const nextType = String(spatial || '').trim()
    if (!nextType || isGenericGeometryType(nextType)) return false
    const currentType = String(current || '').trim()
    return !currentType || isGenericGeometryType(currentType)
  }

  function isGenericGeometryType(value) {
    return String(value || '').trim().toLowerCase().replace(/[_\s-]/g, '') === 'geometry'
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
    if (isWatermarkIncremental.value && !supportsWatermarkIncremental.value) {
      resetIncrementalConfig()
    }
  }

  function clearTarget() {
    targetEngineID.value = null
    targetEngineType.value = ''
    targetSchema.value = ''
    targetTable.value = ''
    targetType.value = 'nfs'
    targetRepresentation.value = 'encoded'
    targetConfig.value = {}
    targetFields.value = []
    resetIncrementalConfig()
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
      target_type: field.type || 'string',
      format: '',
      default_value: '',
		nullable: field.nullable !== false
    }))
  }

  function updateFieldMapping(index, mapping) {
    const previousTarget = String(fieldMappings.value[index]?.target_field || '').trim()
    fieldMappings.value[index] = mapping
    const nextTarget = String(mapping?.target_field || '').trim()
    if (previousTarget && targetKeys.value.some(key => sameFieldName(key, previousTarget))) {
      targetKeys.value = normalizedFieldNames(
        targetKeys.value.map(key => sameFieldName(key, previousTarget) ? nextTarget : key)
      )
    }
    normalizeContinuousKeys()
  }

  function addFieldMapping() {
    fieldMappings.value.push({
      source_field: '',
      target_field: '',
      target_type: 'string',
      format: '',
      default_value: '',
      nullable: true
    })
  }

  function removeFieldMapping(index) {
    const removedSource = String(fieldMappings.value[index]?.source_field || '').trim()
    const removedTarget = String(fieldMappings.value[index]?.target_field || '').trim()
    fieldMappings.value.splice(index, 1)
    if (removedTarget) {
      targetKeys.value = targetKeys.value.filter(key => !sameFieldName(key, removedTarget))
    }
    if (removedSource) {
      continuousKeyFields.value = continuousKeyFields.value.filter(key => !sameFieldName(key, removedSource))
    }
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
  function loadFromTask(task, engineTypes = {}) {
    if (!task) return

    // 基本信息
    taskName.value = task.name || ''
    taskDescription.value = task.description || ''
    schedule.value = task.schedule || ''
    enabled.value = task.enabled || false
    batchSize.value = task.batch_size || 1000

    runtimeBoundary.value = task.config?.runtime?.boundary === 'continuous' ? 'continuous' : 'bounded'
    const load = task.config?.load || {}
		const changeType = load.change_detection?.type
		loadMode.value = changeType === 'cdc' ? 'cdc' : (load.mode === 'incremental' ? 'incremental' : 'snapshot')
    watermarkField.value = load.change_detection?.field || ''
    watermarkTieBreakers.value = normalizedFieldNames(load.change_detection?.tie_breaker)
    targetKeys.value = normalizedFieldNames(task.config?.target?.policy?.keys)
		continuousKeyFields.value = changeType === 'cdc'
			? normalizedFieldNames(task.config?.target?.policy?.keys).map(targetKey => {
				const mapping = (task.config?.transforms?.[0]?.fields || []).find(field => sameFieldName(field?.target, targetKey))
				return String(mapping?.source || '').trim()
			}).filter(Boolean)
			: normalizedFieldNames(task.config?.source?.change_stream?.key?.fields)
    continuousInitialPosition.value = task.config?.source?.change_stream?.start?.initial === 'latest' ? 'latest' : 'earliest'
    continuousPollBatchSize.value = Number(task.config?.source?.change_stream?.poll_batch_size || task.batch_size || 1000)

    // Source 配置
    if (task.config?.source) {
      const source = task.config.source
      const sourceLoc = parseTransferLocator(source.locator)
      sourceEngineID.value = sourceLoc.engineID || null
			sourceEngineType.value = normalizeEngineType(engineTypes.source || '')
      sourceSchema.value = sourceLoc.path.length >= 2 ? sourceLoc.path[sourceLoc.path.length - 2] : ''
      sourceTable.value = sourceLoc.path.length >= 1 ? sourceLoc.path[sourceLoc.path.length - 1] : ''
			sourceType.value = normalizeEngineType(sourceEngineType.value)
			sourceDataType.value = isKafkaContinuousTask.value ? 'unknown' : (source.data_type || 'table')
      sourceRepresentation.value = source.representation || 'native'
      sourceFormat.value = targetUiFormat(source.format, source.options || {})
      sourceLocator.value = source.locator || ''
      sourceConfig.value = extractSourceConfig(source)
    }

    // Target 配置
    if (task.config?.target) {
      const target = task.config.target
      const targetParentLoc = parseTransferLocator(target.parent_locator)
      targetEngineID.value = targetParentLoc.engineID || null
			targetEngineType.value = normalizeEngineType(engineTypes.target || '')
      targetSchema.value = targetParentLoc.type === 'schema' && targetParentLoc.path.length >= 1 ? targetParentLoc.path[targetParentLoc.path.length - 1] : ''
      targetTable.value = target.representation === 'native' ? (target.name || '') : ''
      targetType.value = normalizeTargetType(target)
      targetRepresentation.value = target.representation || 'encoded'
      targetConfig.value = extractTargetConfig(target)
    }

    // 字段映射：从 config.transforms 回填。
    const fieldMappingTransform = (task.config?.transforms || []).find(transform => transform?.type === 'field_mapping')
    if (fieldMappingTransform) {
      fieldMappings.value = (fieldMappingTransform.fields || []).map(field => ({
        source_field: field.source || '',
        target_field: field.target || '',
        target_type: field.target_type || 'string',
        format: field.format || '',
        default_value: field.default ?? '',
        nullable: field.nullable !== false
      }))
      transforms.value = (task.config?.transforms || []).filter(transform => transform?.type !== 'field_mapping')
    } else {
      transforms.value = task.config?.transforms || []
    }
  }

  function normalizeEngineType(engineType) {
    const type = String(engineType || '').toLowerCase()
    if (type.includes('postgres')) return 'postgresql'
    if (type.includes('mysql')) return 'mysql'
    if (type.includes('kafka')) return 'kafka'
    if (type.includes('s3') || type.includes('minio')) return 's3'
		return type
  }

  function isPostgresqlEngineType(engineType) {
    const type = String(engineType || '').trim().toLowerCase()
    return !!type && normalizeEngineType(type) === 'postgresql'
  }

  function sameLocatorIdentity(left, right) {
    const leftText = String(left || '').trim()
    const rightText = String(right || '').trim()
    if (leftText === rightText) return true

    const leftLoc = parseTransferLocator(leftText)
    const rightLoc = parseTransferLocator(rightText)
    if (!leftLoc.engineID || !rightLoc.engineID) return false
    if (leftLoc.engineID !== rightLoc.engineID) return false
    if (leftLoc.type !== rightLoc.type) return false
    if (leftLoc.path.length !== rightLoc.path.length) return false
    if (leftLoc.path.some((part, index) => part !== rightLoc.path[index])) return false
    if (leftLoc.itemID && rightLoc.itemID && leftLoc.itemID !== rightLoc.itemID) return false
    return true
  }

  function normalizeTargetType(target) {
    const loc = parseTransferLocator(target?.parent_locator)
    if (['bucket', 'prefix', 'service'].includes(loc.type)) return 's3'
    return 'nfs'
  }

  function extractTargetConfig(target) {
    const loc = parseTransferLocator(target?.parent_locator)
    if (target?.representation === 'native') {
      return {
        parentLocator: target.parent_locator || '',
        schema: loc.path.length >= 1 ? loc.path[loc.path.length - 1] : '',
        table: target.name || '',
        writeMode: normalizeTableWriteMode(target.policy?.apply_mode)
      }
    }

    if (target?.representation !== 'encoded') {
      return {}
    }

    const parentPath = loc.path.join('/')
    const options = target.options || {}
    const format = targetUiFormat(target.format, options)

    return {
      format,
      parentLocator: target.parent_locator || '',
      resourcePath: parentPath,
      resourceFile: target.name || '',
      includeHeader: target.options?.header !== false,
      delimiter: target.options?.delimiter || ',',
      geometryField: options.geometry_field || '',
      geometryType: options.geometry_type || '',
      writeMode: normalizeTableWriteMode(target.policy?.apply_mode)
    }
  }

  function extractSourceConfig(source) {
    const loc = parseTransferLocator(source?.locator)
    const label = locatorDisplayPath(source?.locator, source?.representation)
    return {
      sourceLabel: label,
      catalogPath: label,
      dataType: source.data_type || 'table',
      representation: source.representation || 'native',
      format: targetUiFormat(source.format, source.options || {}),
      locator: source.locator || '',
      sourceItem: loc.itemID
        ? {
            item_id: loc.itemID,
            meta_id: loc.itemID,
            path: sourcePathFromLocator(source.locator),
            data_type: source.data_type || 'table',
            representation: source.representation || 'native',
            format: targetUiFormat(source.format, source.options || '')
          }
        : null
    }
  }

  function updateSourceItem(item) {
    if (!item) return
    const attrs = item.attributes || {}
    const itemAttrs = attrs.item || {}
    const sourceItem = {
      item_id: item.id || item.item_id,
      meta_id: item.id || item.item_id,
      full_name: item.full_name,
      name: item.name,
      kind: item.item_type || item.kind,
      term: item.item_type || item.term,
      path: sourcePathFromLocator(sourceLocator.value) || sourcePathFromLabel(item.full_name || item.name, sourceRepresentation.value),
      data_type: itemAttrs.data_type || sourceDataType.value,
      representation: itemAttrs.representation || sourceRepresentation.value,
      format: itemAttrs.format || sourceFormat.value,
      layout: itemAttrs.layout,
      physical_path: attrs.storage?.physical_path || item.physical_path,
      attributes: attrs,
      row_count: item.row_count,
      size_bytes: item.size_bytes,
      last_modified_at: item.data_updated_at || item.scanned_at
    }
    sourceConfig.value = {
      ...(sourceConfig.value || {}),
      sourceItem
    }
  }

  function sourcePathFromLabel(label, representation) {
    const separator = String(representation || '').toLowerCase() === 'encoded' ? '/' : '.'
    const names = String(label || '')
      .split(separator)
      .map(part => part.trim())
      .filter(Boolean)
    return {
      segments: names.map((name, index) => ({
        name,
        kind: index === names.length - 1 ? 'item' : 'container',
        term: index === names.length - 1 ? 'item' : 'container'
      }))
    }
  }

  function sourcePathFromLocator(locator) {
    const loc = parseTransferLocator(locator)
    const names = loc.path
    if (names.length === 0) return null
    return {
      segments: names.map((name, index) => ({
        name,
        kind: index === names.length - 1 ? loc.type : containerKindForLocator(loc.type, index),
        term: index === names.length - 1 ? loc.type : containerKindForLocator(loc.type, index)
      }))
    }
  }

  function containerKindForLocator(kind, index) {
    if (kind === 'table') return 'schema'
    if (kind === 'object' && index === 0) return 'bucket'
    return 'directory'
  }

  function targetUiFormat(format, options = {}) {
    const normalized = String(format || '').toLowerCase()
    if (!normalized) return ''
    if (normalized !== 'json') return normalized

    const jsonMode = String(options.json_mode || options.layout || '').toLowerCase()
    if (jsonMode === 'jsonl' || jsonMode === 'lines' || jsonMode === 'ndjson') return 'jsonl'

    return 'json'
  }

  function normalizeTableWriteMode(value) {
    const mode = String(value || '').toLowerCase()
    if (mode === 'append') return 'append'
    return 'overwrite'
  }

  function normalizeTableApplyMode(value) {
    return normalizeTableWriteMode(value) === 'append' ? 'append' : 'replace'
  }

  function normalizedFieldNames(values) {
    const items = Array.isArray(values) ? values : []
    const seen = new Set()
    return items
      .map(value => String(value || '').trim())
      .filter(value => {
        const key = value.toLowerCase()
        if (!value || seen.has(key)) return false
        seen.add(key)
        return true
      })
  }

  function isPrimaryKeyField(field) {
    return field?.primary_key === true ||
      field?.primaryKey === true ||
      field?.is_primary_key === true ||
			field?.isPrimaryKey === true ||
			String(field?.key || '').trim().toLowerCase() === 'pri'
  }

  function mappedTargetField(sourceField) {
    const mapping = fieldMappings.value.find(item => sameFieldName(item?.source_field, sourceField))
    return String(mapping?.target_field || '').trim()
  }

  function initializeIncrementalDefaults() {
    if (!watermarkField.value) {
      const updatedAt = sourceFields.value.find(field => sameFieldName(field?.name, 'updated_at'))
      if (updatedAt) watermarkField.value = updatedAt.name
    }

    const primaryKeys = sourceFields.value.filter(isPrimaryKeyField).map(field => field.name)
    if (watermarkTieBreakers.value.length === 0 && primaryKeys.length > 0) {
      watermarkTieBreakers.value = primaryKeys.filter(name => !sameFieldName(name, watermarkField.value))
    }
    if (targetKeys.value.length === 0 && primaryKeys.length > 0) {
      targetKeys.value = primaryKeys.map(mappedTargetField).filter(Boolean)
    }
  }

  function updateContinuousKeyFields(values) {
    const next = normalizedFieldNames(values)
    if (!sameFieldList(continuousKeyFields.value, next)) {
      continuousKeyFields.value = next
    }
    normalizeContinuousKeys()
  }

  function sameFieldList(left, right) {
    if (left.length !== right.length) return false
    return left.every((value, index) => sameFieldName(value, right[index]))
  }

  function normalizeContinuousKeys() {
    if (!isContinuousTask.value) return
		const normalizedKeys = normalizeContinuousKeyFields(continuousKeyFields.value, fieldMappings.value)
		if (!sameFieldList(continuousKeyFields.value, normalizedKeys)) {
			continuousKeyFields.value = normalizedKeys
		}
    for (const mapping of fieldMappings.value) {
      if (continuousKeyFields.value.some(key => sameFieldName(key, mapping?.source_field))) {
        mapping.nullable = false
      }
    }
  }

  function resetIncrementalConfig() {
    if (!isContinuousTask.value) {
      loadMode.value = 'snapshot'
    }
    watermarkField.value = ''
    watermarkTieBreakers.value = []
    targetKeys.value = []
  }

  function locatorDisplayPath(locator, representation = '') {
    return formatLocatorDisplayPath(locator, representation)
  }

  function isRawCopyShape(shape) {
    const dataType = String(shape?.dataType || '').toLowerCase()
    const representation = String(shape?.representation || '').toLowerCase()
    const format = String(shape?.format || '').toLowerCase()
    const locatorType = parseTransferLocator(shape?.locator).type
    return ['document', 'media', 'cad', 'unknown'].includes(dataType) &&
      representation === 'encoded' &&
      !!format &&
      ['file', 'object'].includes(locatorType) &&
      rawCopyDataTypeForFormat(format) === dataType
  }

  function isRawCopyEndpoint(endpoint) {
    return isRawCopyShape({
      dataType: endpoint?.data_type,
      representation: endpoint?.representation,
      format: endpoint?.format,
      locator: endpoint?.locator
    })
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
    runtimeBoundary.value = 'bounded'
    resetIncrementalConfig()
    continuousKeyFields.value = []
    continuousInitialPosition.value = 'earliest'
    continuousPollBatchSize.value = 1000
    sourceEngineID.value = null
    sourceEngineType.value = ''
    sourceSchema.value = ''
    sourceTable.value = ''
    sourceType.value = 'postgresql'
    sourceDataType.value = 'table'
    sourceRepresentation.value = 'native'
    sourceFormat.value = ''
    sourceLocator.value = ''
    readableEncodedFormats.value = new Set()
    rawCopyFormats.value = new Map()
    formatCapabilitiesLoaded.value = false
		databaseCDCCapability.value = null
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
    runtimeBoundary,
    loadMode,
    watermarkField,
    watermarkTieBreakers,
    targetKeys,
    continuousKeyFields,
    continuousInitialPosition,
    continuousPollBatchSize,
    sourceConfig,
    sourceEngineID,
    sourceEngineType,
    sourceSchema,
    sourceTable,
    sourceType,
    sourceDataType,
    sourceRepresentation,
    sourceFormat,
    sourceLocator,
    readableEncodedFormats,
    rawCopyFormats,
    formatCapabilitiesLoaded,
		databaseCDCCapability,
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
    isRawCopyTask,
    supportsWatermarkIncremental,
    isWatermarkIncremental,
    watermarkIncrementalValid,
    isContinuousTask,
		isKafkaContinuousTask,
		isDatabaseCDCTask,
		supportsDatabaseCDC,
		databaseCDCUnavailableReasons,
    supportsContinuousTarget,
    continuousTargetKeys,
    continuousConfigValid,

    // 计算属性
    canGoNext,
    taskConfig,

    // 方法
    nextStep,
    prevStep,
    goToStep,
    updateSource,
    updateFormatCapabilities,
    loadSourceFields,
    updateSourceItem,
    updateTarget,
    clearTarget,
    loadTargetFields,
    initializeIncrementalDefaults,
    updateContinuousKeyFields,
		setLoadMode,
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
