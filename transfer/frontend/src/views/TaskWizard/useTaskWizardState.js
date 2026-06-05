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
  const sourceLocator = ref('')
  const readableEncodedFormats = ref(new Set())
  const rawCopyFormats = ref(new Map())
  const formatCapabilitiesLoaded = ref(false)

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

  const canGoNext = computed(() => {
    switch (currentStep.value) {
      case 0: // 选择Source
        return !!(sourceEngineID.value && isSupportedSourceShape() && (sourceLocator.value || sourceTable.value))
      case 1: // 选择Target
        if (targetRepresentation.value === 'native') {
          return !!(targetEngineID.value && targetSchema.value && targetTable.value)
        }
        const hasTargetFormat = isRawCopyTask.value || !!targetBackendFormat(targetConfig.value || {})
        return !!(
          targetEngineID.value &&
          hasTargetFormat &&
          targetConfig.value?.resourceFile &&
          !targetConfig.value?.extensionError &&
          (targetType.value !== 's3' || targetConfig.value?.resourcePath)
        )
      case 2: // 字段映射
        if (isRawCopyTask.value) return true
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
        transforms: buildTransformsConfig(),
        batch_size: batchSize.value
      },
      schedule: schedule.value,
      enabled: schedule.value ? enabled.value : false, // 只有设置了定时任务才考虑 enabled
      batch_size: batchSize.value,
      auto_scan_metadata: true
    }

    return config
  })

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
    const endpoint = {
      locator: sourceLocator.value || buildLocator(Number(sourceEngineID.value), 'table', [sourceSchema.value, sourceTable.value]),
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

  function taskTypeForShape(sourceEndpoint, targetEndpoint) {
    if (isRawCopyEndpoint(sourceEndpoint) && targetEndpoint?.representation === 'encoded') return 'transfer'
    if (sourceEndpoint?.representation === 'encoded' && targetEndpoint?.representation === 'native') return 'import'
    if (sourceEndpoint?.representation === 'native' && targetEndpoint?.representation === 'encoded') return 'export'
    return 'transfer'
  }

  function isSupportedSourceShape() {
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
    formatCapabilitiesLoaded.value = capabilities !== null
  }

  function buildTargetEndpoint() {
    const fileConfig = targetConfig.value || {}
    if (targetRepresentation.value === 'native') {
      return {
        locator: buildLocator(Number(targetEngineID.value), 'table', [targetSchema.value, targetTable.value]),
        data_type: 'table',
        representation: 'native',
        policy: {
          write_mode: normalizeTableWriteMode(fileConfig.writeMode)
        }
      }
    }

    const format = targetBackendFormat(fileConfig)
    const dataType = isRawCopyTask.value ? sourceDataType.value : 'table'
    const endpoint = {
      locator: buildEncodedTargetLocator(fileConfig),
      data_type: dataType,
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

  function buildEncodedTargetLocator(fileConfig) {
    const outputPath = trimSlashes(fileConfig.resourcePath || '')
    const outputFileName = trimSlashes(fileConfig.resourceFile || '')
    const fullPath = [outputPath, outputFileName].filter(Boolean).join('/')

    if (targetType.value === 's3') {
      return buildLocator(Number(targetEngineID.value), 'object', splitPathSegments(fullPath))
    }

    return buildLocator(Number(targetEngineID.value), 'file', splitPathSegments(fullPath))
  }

  function trimSlashes(value) {
    return String(value || '').trim().replace(/^\/+|\/+$/g, '')
  }

  function splitPathSegments(value) {
    return String(value || '').split('/').map(part => part.trim()).filter(Boolean)
  }

  function buildLocator(engineID, type, segments = [], itemID = 0) {
    const cleanEngineID = Number(engineID)
    const cleanType = String(type || '').trim()
    const path = (segments || []).map(segment => String(segment || '').trim()).filter(Boolean)
    if (!cleanEngineID || !cleanType || path.length === 0) return ''
    const encodedPath = path.map(segment => encodeURIComponent(segment)).join('/')
    const params = new URLSearchParams({ type: cleanType })
    const cleanItemID = Number(itemID || 0)
    if (cleanItemID > 0) params.set('item_id', String(cleanItemID))
    return `addp://engine/${cleanEngineID}/path/${encodedPath}?${params.toString()}`
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

    if (sourceChanged) {
      sourceFields.value = []
      targetFields.value = []
      fieldMappings.value = []
      targetEngineID.value = null
      targetEngineType.value = ''
      targetSchema.value = ''
      targetTable.value = ''
      targetConfig.value = {}
      targetRepresentation.value = isRawCopyTask.value ? 'encoded' : (sourceRepresentation.value === 'encoded' ? 'native' : 'encoded')
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

  function clearTarget() {
    targetEngineID.value = null
    targetEngineType.value = ''
    targetSchema.value = ''
    targetTable.value = ''
    targetType.value = 'nfs'
    targetRepresentation.value = 'encoded'
    targetConfig.value = {}
    targetFields.value = []
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
      target_type: 'string',
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
      const sourceLoc = parseLocator(source.locator)
      sourceEngineID.value = sourceLoc.engineID || null
      sourceEngineType.value = ''
      sourceSchema.value = sourceLoc.path.length >= 2 ? sourceLoc.path[sourceLoc.path.length - 2] : ''
      sourceTable.value = sourceLoc.path.length >= 1 ? sourceLoc.path[sourceLoc.path.length - 1] : ''
      sourceType.value = normalizeEngineType(sourceEngineType.value || 'postgresql')
      sourceDataType.value = source.data_type || 'table'
      sourceRepresentation.value = source.representation || 'native'
      sourceFormat.value = targetUiFormat(source.format, source.options || {})
      sourceLocator.value = source.locator || ''
      sourceConfig.value = extractSourceConfig(source)
    }

    // Target 配置
    if (task.config?.target) {
      const target = task.config.target
      const targetLoc = parseLocator(target.locator)
      targetEngineID.value = targetLoc.engineID || null
      targetEngineType.value = ''
      targetSchema.value = targetLoc.type === 'table' && targetLoc.path.length >= 2 ? targetLoc.path[targetLoc.path.length - 2] : ''
      targetTable.value = targetLoc.type === 'table' && targetLoc.path.length >= 1 ? targetLoc.path[targetLoc.path.length - 1] : ''
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
    if (type.includes('s3') || type.includes('minio')) return 's3'
    return type || 'postgresql'
  }

  function sameLocatorIdentity(left, right) {
    const leftText = String(left || '').trim()
    const rightText = String(right || '').trim()
    if (leftText === rightText) return true

    const leftLoc = parseLocator(leftText)
    const rightLoc = parseLocator(rightText)
    if (!leftLoc.engineID || !rightLoc.engineID) return false
    if (leftLoc.engineID !== rightLoc.engineID) return false
    if (leftLoc.type !== rightLoc.type) return false
    if (leftLoc.path.length !== rightLoc.path.length) return false
    if (leftLoc.path.some((part, index) => part !== rightLoc.path[index])) return false
    if (leftLoc.itemID && rightLoc.itemID && leftLoc.itemID !== rightLoc.itemID) return false
    return true
  }

  function normalizeTargetType(target) {
    const loc = parseLocator(target?.locator)
    if (loc.type === 'file') return 'nfs'
    if (loc.type === 'object') return 's3'
    return 'nfs'
  }

  function extractTargetConfig(target) {
    const loc = parseLocator(target?.locator)
    if (loc.type === 'table') {
      return {
        schema: loc.path.length >= 2 ? loc.path[loc.path.length - 2] : '',
        table: loc.path.length >= 1 ? loc.path[loc.path.length - 1] : '',
        writeMode: normalizeTableWriteMode(target.policy?.write_mode)
      }
    }

    if (loc.type !== 'file' && loc.type !== 'object') {
      return {}
    }

    const path = loc.path.join('/')
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
    const loc = parseLocator(source?.locator)
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
    const loc = parseLocator(locator)
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

  function splitPath(path) {
    const cleaned = trimSlashes(path)
    if (!cleaned) return { dir: '', file: '' }
    const parts = cleaned.split('/')
    const file = parts.pop() || ''
    return { dir: parts.join('/'), file }
  }

  function normalizeTableWriteMode(value) {
    const mode = String(value || '').toLowerCase()
    if (mode === 'append') return 'append'
    return 'overwrite'
  }

  function parseLocator(locator) {
    const result = { engineID: 0, path: [], type: '', itemID: 0 }
    const match = String(locator || '').match(/^addp:\/\/engine\/(\d+)\/path\/([^?]*)(?:\?(.*))?$/)
    if (!match) return result
    result.engineID = Number(match[1] || 0)
    result.path = String(match[2] || '')
      .split('/')
      .map(part => decodeURIComponent(part).trim())
      .filter(Boolean)
    const params = new URLSearchParams(match[3] || '')
    result.type = String(params.get('type') || '').toLowerCase()
    result.itemID = Number(params.get('item_id') || 0)
    return result
  }

  function locatorDisplayPath(locator, representation = '') {
    const loc = parseLocator(locator)
    if (loc.path.length === 0) return ''
    if (String(representation || '').toLowerCase() === 'native' && loc.type === 'table') {
      return loc.path.slice(-2).join('.')
    }
    return loc.path.join('/')
  }

  function isRawCopyShape(shape) {
    const dataType = String(shape?.dataType || '').toLowerCase()
    const representation = String(shape?.representation || '').toLowerCase()
    const format = String(shape?.format || '').toLowerCase()
    const locatorType = parseLocator(shape?.locator).type
    return ['document', 'media', 'unknown'].includes(dataType) &&
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
    sourceLocator,
    readableEncodedFormats,
    rawCopyFormats,
    formatCapabilitiesLoaded,
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
