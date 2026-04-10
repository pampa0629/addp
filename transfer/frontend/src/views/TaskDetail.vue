<template>
  <div class="task-detail">
    <el-button @click="$router.back()" style="margin-bottom: 20px;">
      <el-icon><ArrowLeft /></el-icon>
      {{ t('transfer.taskDetail.back') }}
    </el-button>

    <el-card v-loading="loading">
      <template #header>
        <div class="header">
          <span>{{ t('transfer.taskDetail.taskDetailTitle', { name: task.name }) }}</span>
          <div>
            <template v-if="isManualTask">
              <el-button type="primary" @click="handleExecute" :disabled="task.status === 'running'">
                {{ t('transfer.taskDetail.execute') }}
              </el-button>
              <el-button type="warning" @click="handleStop" :disabled="task.status !== 'running'">
                {{ t('transfer.taskDetail.stop') }}
              </el-button>
            </template>
            <template v-else>
              <el-button type="primary" @click="handleResume" :disabled="!canStartSchedule">
                {{ t('transfer.taskDetail.start') }}
              </el-button>
              <el-button type="warning" @click="handlePause" :disabled="!canPauseSchedule">
                {{ t('transfer.taskDetail.pause') }}
              </el-button>
              <el-button @click="handleExecute" :disabled="task.status === 'running'">
                {{ t('transfer.taskDetail.runOnce') }}
              </el-button>
            </template>
            <el-button @click="handleEdit" :disabled="!canEditTask">{{ t('transfer.taskDetail.edit') }}</el-button>
            <el-button @click="openJsonDialog">{{ t('transfer.taskDetail.viewJsonConfig') }}</el-button>
          </div>
        </div>
      </template>

      <el-descriptions :column="2" border>
        <el-descriptions-item :label="t('transfer.taskDetail.taskId')">{{ task.id }}</el-descriptions-item>
        <el-descriptions-item :label="t('transfer.taskDetail.taskName')">{{ task.name }}</el-descriptions-item>
        <el-descriptions-item :label="t('transfer.taskDetail.status')">
          <el-tag :type="getTaskStatusTagType(task)">{{ getTaskStatusLabel(task) }}</el-tag>
        </el-descriptions-item>
        <el-descriptions-item :label="t('transfer.taskDetail.batchSize')">{{ task.batch_size }}</el-descriptions-item>
        <el-descriptions-item :label="t('transfer.taskDetail.schedule')">{{ formatSchedule(task.schedule) }}</el-descriptions-item>
        <el-descriptions-item :label="t('transfer.taskDetail.createdAt')">
          {{ formatDate(task.created_at) }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('transfer.taskDetail.description')" :span="2">
          {{ task.description || '-' }}
        </el-descriptions-item>
      </el-descriptions>

      <el-divider content-position="left">{{ t('transfer.taskDetail.sourceDataSource') }}</el-divider>
      <el-descriptions :column="2" border v-if="sourceDetails.length">
        <el-descriptions-item
          v-for="item in sourceDetails"
          :key="`source-${item.label}`"
          :span="item.span || 1"
        >
          <template #label>{{ item.label }}</template>
          <span class="detail-text">{{ item.value }}</span>
        </el-descriptions-item>
      </el-descriptions>

      <el-divider content-position="left">{{ t('transfer.taskDetail.targetDataSource') }}</el-divider>
      <el-descriptions :column="2" border v-if="targetDetails.length">
        <el-descriptions-item
          v-for="item in targetDetails"
          :key="`target-${item.label}`"
          :span="item.span || 1"
        >
          <template #label>{{ item.label }}</template>
          <span class="detail-text">{{ item.value }}</span>
        </el-descriptions-item>
      </el-descriptions>

      <el-divider>{{ t('transfer.taskDetail.executionRecords') }}</el-divider>
      <el-table :data="executions" stripe>
        <el-table-column prop="id" :label="t('transfer.taskDetail.id')" width="80" />
        <el-table-column prop="status" :label="t('transfer.taskDetail.status')" width="100">
          <template #default="{ row }">
            <el-tag :type="getExecutionTagType(row.status)">
              {{ getExecutionLabel(row.status) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="records_written" :label="t('transfer.taskDetail.recordsWritten')" width="120" />
        <el-table-column prop="start_time" :label="t('transfer.taskDetail.startTime')" width="180">
          <template #default="{ row }">
            {{ formatDate(row.start_time) }}
          </template>
        </el-table-column>
        <el-table-column prop="end_time" :label="t('transfer.taskDetail.endTime')" width="180">
          <template #default="{ row }">
            {{ formatDate(row.end_time) }}
          </template>
        </el-table-column>
        <el-table-column :label="t('transfer.taskDetail.actions')" width="150">
          <template #default="{ row }">
            <el-button size="small" @click="viewExecution(row.id)">{{ t('transfer.taskDetail.detail') }}</el-button>
            <el-button size="small" type="primary" @click="retryExecution(row.id)"
              v-if="row.status === 'failed'">
              {{ t('transfer.taskDetail.retry') }}
            </el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <el-dialog v-model="jsonDialogVisible" width="700px">
      <template #header>
        <div class="json-dialog-header">
          <span>{{ t('transfer.taskDetail.jsonConfig') }}</span>
          <el-button size="small" type="primary" @click="handleCopyJson">{{ t('transfer.taskDetail.copy') }}</el-button>
        </div>
      </template>
      <pre class="json-pre">{{ formattedConfig }}</pre>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { taskAPI, executionAPI } from '@/api/tasks'
import { systemEnginesAPI } from '@/api/systemEngines'
import { formatDate } from '@common-ui'
import { formatSchedule, getTaskStatusLabel, getTaskStatusTagType, getLastExecutionLabel, getExecutionTagType, getExecutionLabel } from '@/utils/formatters'

const { t } = useI18n()

const route = useRoute()
const router = useRouter()
const loading = ref(false)
const task = ref({})
const executions = ref([])
const mappings = ref([])
const isManualTask = computed(() => !task.value?.schedule)
const canStartSchedule = computed(() => {
  return !task.value?.enabled
})
const canPauseSchedule = computed(() => {
  return task.value?.enabled
})
const canEditTask = computed(() => {
  const status = task.value?.status
  return !status || status !== 'running'
})

const systemResources = ref([])
const jsonDialogVisible = ref(false)

const systemResourceMap = computed(() => {
  const map = new Map()
  systemResources.value.forEach((item) => {
    if (item && item.id !== undefined && item.id !== null) {
      map.set(Number(item.id), item)
    }
  })
  return map
})

const connectorTypeLabels = computed(() => ({
  postgresql: 'PostgreSQL',
  mysql: 'MySQL',
  s3: t('transfer.taskDetail.objectStorageS3'),
  minio: t('transfer.taskDetail.objectStorageMinio'),
  oss: t('transfer.taskDetail.objectStorageOss'),
  csv: t('transfer.taskDetail.csvFile'),
  json: t('transfer.taskDetail.jsonFile')
}))

const incrementalTypeLabels = computed(() => ({
  timestamp: t('transfer.taskDetail.timestampType'),
  numeric: t('transfer.taskDetail.numericType'),
  string: t('transfer.taskDetail.stringType')
}))

const loadTask = async () => {
  if (!route.params.id) return
  loading.value = true
  try {
    const taskData = await taskAPI.get(route.params.id)
    const [executionsRes, mappingsRes] = await Promise.all([
      taskAPI.executions(route.params.id, { page: 1, page_size: 10 }).catch(() => ({ data: [] })),
      taskAPI.getMappings(route.params.id).catch(() => [])
    ])

    task.value = taskData || {}
    executions.value = executionsRes?.data || []
    mappings.value = Array.isArray(mappingsRes) ? mappingsRes : []
  } finally {
    loading.value = false
  }
}

const loadSystemResources = async () => {
  try {
    const data = await systemEnginesAPI.list()
    systemResources.value = Array.isArray(data) ? data : []
  } catch (error) {
    console.error(t('transfer.taskDetail.loadSystemResourcesFailed'), error)
  }
}

const handleExecute = async () => {
  await taskAPI.start(route.params.id)
  const message = isManualTask.value ? t('transfer.taskDetail.executeSubmitted') : t('transfer.taskDetail.runOnceSubmitted')
  ElMessage.success(message)
  await loadTask()
}

const handleStop = async () => {
  try {
    await ElMessageBox.confirm(t('transfer.taskDetail.stopConfirm'), t('transfer.taskDetail.hint'), {
      confirmButtonText: t('transfer.taskDetail.confirm'),
      cancelButtonText: t('transfer.taskDetail.cancel'),
      type: 'warning'
    })
    await taskAPI.stop(route.params.id)
    ElMessage.success(t('transfer.taskDetail.taskStopped'))
    await loadTask()
  } catch (error) {
    if (error !== 'cancel') {
      console.error('停止任务失败:', error)
    }
  }
}

const handlePause = async () => {
  try {
    await ElMessageBox.confirm(t('transfer.taskDetail.pauseConfirm'), t('transfer.taskDetail.hint'), {
      confirmButtonText: t('transfer.taskDetail.confirm'),
      cancelButtonText: t('transfer.taskDetail.cancel'),
      type: 'warning'
    })
    await taskAPI.pause(route.params.id)
    ElMessage.success(t('transfer.taskDetail.taskPaused'))
    await loadTask()
  } catch (error) {
    if (error !== 'cancel') {
      console.error('暂停任务失败:', error)
    }
  }
}

const handleResume = async () => {
  await taskAPI.resume(route.params.id)
  ElMessage.success(t('transfer.taskDetail.taskResumed'))
  await loadTask()
}

const handleEdit = () => {
  if (!canEditTask.value) {
    ElMessage.warning(t('transfer.taskDetail.taskRunning'))
    return
  }
  router.push(`/tasks/${route.params.id}/edit`)
}

const viewExecution = (id) => {
  router.push(`/executions/${id}`)
}

const retryExecution = async (id) => {
  await executionAPI.retry(id)
  ElMessage.success(t('transfer.taskDetail.retrySubmitted'))
  loadTask()
}

const toLower = (value) => (value || '').toString().toLowerCase()

const cloneDeep = (value) => {
  if (value === null || typeof value !== 'object') {
    return value
  }
  try {
    return JSON.parse(JSON.stringify(value))
  } catch {
    return value
  }
}

const toNumber = (value) => {
  if (value === undefined || value === null || value === '') {
    return null
  }
  const num = Number(value)
  return Number.isFinite(num) ? num : null
}

const isS3Type = (resourceType) => {
  const type = toLower(resourceType)
  return type === 's3' || type === 'minio' || type === 'oss'
}

const resourceToConnectorForWorker = (resource) => {
  if (!resource) return {}
  const type = toLower(resource.resource_type)
  const conn = resource.connection_info || {}
  const connector = {}

  if (type === 'postgresql' || type === 'mysql') {
    connector.type = 'jdbc'
    connector.driver = type
    if (conn.host) connector.host = conn.host
    const port = toNumber(conn.port)
    if (port !== null) connector.port = port
    const username = conn.username || conn.user
    if (username) connector.username = username
    if (conn.password) connector.password = conn.password
    if (conn.database) connector.database = conn.database
    const sslMode = conn.ssl_mode || conn.sslmode
    if (sslMode) connector.ssl_mode = sslMode
  } else if (isS3Type(type)) {
    connector.type = 's3'
    if (conn.endpoint) connector.endpoint = conn.endpoint
    if (conn.access_key) connector.access_key = conn.access_key
    if (conn.secret_key) connector.secret_key = conn.secret_key
    if (conn.bucket) connector.bucket = conn.bucket
    if (conn.region) connector.region = conn.region
    connector.use_ssl = conn.use_ssl !== undefined ? !!conn.use_ssl : false
  }

  return connector
}

const mergeTaskConnectorConfig = (base, extra, resourceType) => {
  const skipKeys = new Set([
    'host',
    'port',
    'user',
    'username',
    'password',
    'database',
    'driver',
    'endpoint',
    'access_key',
    'secret_key',
    'bucket',
    'connection_info',
    'scope',
    'engine_id',
    'local_resource_name',
    'resource_type'
  ])

  const isS3 = isS3Type(resourceType)

  Object.entries(extra || {}).forEach(([key, value]) => {
    if (value === undefined || value === null || value === '') {
      return
    }
    if (skipKeys.has(key)) {
      return
    }
    if (isS3 && key === 'path') {
      base.file_name = value
      return
    }
    if (isS3 && key === 'format') {
      base.file_type = value
      return
    }
    if (typeof value === 'string' && ['parameters', 'include_patterns', 'exclude_patterns', 'conflict_keys', 'geometry_fields'].includes(key)) {
      try {
        base[key] = JSON.parse(value)
        return
      } catch {
        // fall back to string
      }
    }
    base[key] = value
  })

  return base
}

const sanitizeConnectorForWorker = (connector) => {
  const sanitized = {}
  Object.entries(connector || {}).forEach(([key, value]) => {
    if (value === undefined) {
      return
    }
    if (['scope', 'engine_id', 'local_resource_name', 'resource_type'].includes(key)) {
      return
    }
    sanitized[key] = value
  })

  const type = toLower(sanitized.type || sanitized.driver)
  if (type === 'jdbc' || type === 'postgresql' || type === 'mysql') {
    if (!sanitized.type) sanitized.type = 'jdbc'
    if (!sanitized.driver && type !== 'jdbc') sanitized.driver = type
    if (!sanitized.username && sanitized.user) {
      sanitized.username = sanitized.user
    }
    delete sanitized.user
    if (sanitized.port !== undefined) {
      const port = toNumber(sanitized.port)
      if (port !== null) {
        sanitized.port = port
      } else {
        delete sanitized.port
      }
    }
    if (sanitized.sslmode && !sanitized.ssl_mode) {
      sanitized.ssl_mode = sanitized.sslmode
    }
    delete sanitized.sslmode
    if (sanitized.parameters && typeof sanitized.parameters === 'string') {
      try {
        sanitized.parameters = JSON.parse(sanitized.parameters)
      } catch {}
    }
  }

  if (type === 's3' || isS3Type(type)) {
    sanitized.type = 's3'
    sanitized.use_ssl = sanitized.use_ssl !== undefined ? !!sanitized.use_ssl : false
    if (sanitized.path && !sanitized.file_name) {
      sanitized.file_name = sanitized.path
    }
  }

  return sanitized
}

const resolveConnectorConfigForWorker = (key) => {
  const taskConfig = cloneDeep(task.value?.config || {})
  const rawConfig = cloneDeep(taskConfig[key] || {})
  const fallbackId = toNumber(rawConfig.engine_id)

  let resource = null
  const embeddedId = toNumber(rawConfig.system_engine_id || rawConfig.engine_id)
  if (embeddedId !== null) {
    resource = systemResourceMap.value.get(embeddedId) || null
  }
  if (!resource && fallbackId !== null) {
    resource = systemResourceMap.value.get(fallbackId) || null
  }

  if (resource) {
    const connector = resourceToConnectorForWorker(resource)
    mergeTaskConnectorConfig(connector, rawConfig, resource.resource_type)
    return sanitizeConnectorForWorker(connector)
  }

  return sanitizeConnectorForWorker(rawConfig)
}

const inferConnectorTypeForWorker = (config) => {
  if (!config || typeof config !== 'object') {
    return 'unknown'
  }

  // 优先使用 connector_type（数据库存储的标准字段）
  if (config.connector_type) {
    const connectorType = toLower(config.connector_type)
    switch (connectorType) {
      case 'spatialite':
      case 'sqlite':
        return 'spatialite'
      case 'postgresql':
      case 'mysql':
        return 'jdbc'
      case 's3':
      case 'minio':
      case 'oss':
        return 's3'
      case 'kafka':
        return 'kafka'
      case 'csv':
      case 'geojson':
      case 'shapefile':
      case 'geopackage':
        return 'file'
      default:
        return connectorType
    }
  }

  // 后备 1：使用 engine_type
  if (config.engine_type) {
    const engineType = toLower(config.engine_type)
    switch (engineType) {
      case 'spatialite':
      case 'sqlite':
        return 'spatialite'
      case 'postgresql':
      case 'mysql':
        return 'jdbc'
      case 's3':
      case 'minio':
      case 'oss':
        return 's3'
      case 'kafka':
        return 'kafka'
      case 'csv':
      case 'geojson':
      case 'shapefile':
      case 'geopackage':
        return 'file'
      default:
        return engineType
    }
  }

  // 后备 2：使用 type 字段
  const explicit = toLower(config.type)
  if (explicit) return explicit

  // 最后：根据配置字段推断
  if (config.driver) return 'jdbc'
  if (config.bucket || config.endpoint || config.file_name || config.file_type) return 's3'
  if (config.path || config.directory || config.file_path || config.full_name) return 'file'
  if (config.topic) return 'kafka'

  return 'unknown'
}

const valueToStringArray = (value) => {
  if (Array.isArray(value)) {
    return value
      .map((item) => {
        if (item === undefined || item === null) return ''
        return String(item).trim()
      })
      .filter(Boolean)
  }
  if (value === undefined || value === null) {
    return []
  }
  const str = String(value).trim()
  return str ? [str] : []
}

const extractGeometryFieldsForWorker = (config) => {
  if (!config || typeof config !== 'object') {
    return []
  }
  if (config.geometry_fields !== undefined) {
    return valueToStringArray(config.geometry_fields)
  }
  if (config.geometry_field !== undefined) {
    return valueToStringArray(config.geometry_field)
  }
  return []
}

const normalizeSpatialFormatForWorker = (config) => {
  if (!config || typeof config !== 'object') {
    return ''
  }
  const preferred = toLower(config.spatial_format)
  if (preferred) return preferred
  const fileType = toLower(config.file_type || config.format)
  switch (fileType) {
    case 'geojson':
      return 'geojson'
    case 'csv-wkt':
      return 'wkt'
    case 'shapefile':
      return 'wkb'
    case 'ewkb':
    case 'ewkt':
    case 'hexwkb':
    case 'wkb':
    case 'wkt':
      return fileType
    default:
      return ''
  }
}

const ensureGeometryMappingForWorker = (fieldMappings, geometryFields) => {
  if (!Array.isArray(fieldMappings) || !Array.isArray(geometryFields)) {
    return
  }
  const existing = new Set(
    fieldMappings
      .map((item) => (item?.target ? String(item.target).toLowerCase().trim() : ''))
      .filter(Boolean)
  )
  geometryFields.forEach((field) => {
    const name = String(field || '').trim()
    if (!name) return
    const lower = name.toLowerCase()
    if (existing.has(lower)) return
    fieldMappings.push({
      source: name,
      target: name,
      type: '',
      format: '',
      default_value: ''
    })
    existing.add(lower)
  })
}

const buildFieldMappingsForWorker = (rawMappings, sourceConfigResolved, targetConfigResolved) => {
  if (!Array.isArray(rawMappings) || rawMappings.length === 0) {
    return []
  }
  const mapped = rawMappings.map((item) => ({
    source: item.source_field || '',
    target: item.target_field || '',
    type: item.field_type || '',
    format: item.format || '',
    default_value: item.default_value === undefined ? '' : item.default_value,
    nullable: item.nullable !== undefined ? !!item.nullable : true
  }))

  const geometryFromTarget = extractGeometryFieldsForWorker(targetConfigResolved)
  const geometryFromSource = geometryFromTarget.length > 0
    ? geometryFromTarget
    : extractGeometryFieldsForWorker(sourceConfigResolved)

  if (geometryFromSource.length > 0) {
    ensureGeometryMappingForWorker(mapped, geometryFromSource)
  }

  return mapped
}

const hasSpatialTransformConfig = (transforms) => {
  if (!Array.isArray(transforms)) {
    return false
  }
  return transforms.some((item) => {
    if (!item || typeof item !== 'object') return false
    const type = toLower(item.type || item.name)
    return type === 'spatial' || type === 'spatialtransform'
  })
}

const mapTaskModeForWorker = (mode) => {
  const normalized = toLower(mode)
  if (normalized === 'stream') return 'stream'
  if (normalized === 'micro-batch') return 'micro-batch'
  return 'batch'
}

const buildWorkerConfig = () => {
  if (!task.value || !task.value.id) {
    return {}
  }

  const baseConfig = task.value.config || {}
  const sourceConfigResolved = resolveConnectorConfigForWorker('source')
  const targetConfigResolved = resolveConnectorConfigForWorker('target')

  const fieldMappingsForWorker = buildFieldMappingsForWorker(
    mappings.value,
    sourceConfigResolved,
    targetConfigResolved
  )

  const transformsList = []
  if (fieldMappingsForWorker.length > 0) {
    transformsList.push({
      type: 'field_mapping',
      name: 'FieldMapping',
      mappings: fieldMappingsForWorker
    })
  }

  const additionalTransforms = Array.isArray(baseConfig.transforms)
    ? cloneDeep(baseConfig.transforms)
    : []
  additionalTransforms.forEach((transform) => {
    if (transform && typeof transform === 'object') {
      transformsList.push(transform)
    }
  })

  const targetType = inferConnectorTypeForWorker(targetConfigResolved)
  const geometryFields = extractGeometryFieldsForWorker(targetConfigResolved)
  const fallbackGeometry = geometryFields.length > 0
    ? geometryFields
    : extractGeometryFieldsForWorker(sourceConfigResolved)

  if (
    targetType === 's3' &&
    fallbackGeometry.length > 0 &&
    !hasSpatialTransformConfig(transformsList)
  ) {
    const spatialTransform = {
      type: 'spatial',
      geometry_fields: fallbackGeometry,
      source_format: 'wkb'
    }
    const targetFormat = normalizeSpatialFormatForWorker(targetConfigResolved)
    if (targetFormat) {
      spatialTransform.target_format = targetFormat
    }
    transformsList.push(spatialTransform)
  }

  return {
    task_id: task.value.id,
    name: task.value.name,
    description: task.value.description,
    schedule: task.value.schedule || '',
    mode: mapTaskModeForWorker(task.value.mode),
    batch_size: task.value.batch_size,
    max_parallelism: task.value.max_parallelism,
    retry_policy: task.value.retry_policy || null,
    source_config: {
      type: inferConnectorTypeForWorker(sourceConfigResolved),
      batch_size: task.value.batch_size,
      config: sourceConfigResolved
    },
    target_config: {
      type: inferConnectorTypeForWorker(targetConfigResolved),
      config: targetConfigResolved
    },
    transforms: transformsList
  }
}

const copyToClipboard = (text) => {
  // 先尝试使用 document.execCommand (更兼容 iframe 环境)
  const textarea = document.createElement('textarea')
  textarea.value = text
  textarea.style.position = 'fixed'
  textarea.style.top = '0'
  textarea.style.left = '0'
  textarea.style.width = '2em'
  textarea.style.height = '2em'
  textarea.style.padding = '0'
  textarea.style.border = 'none'
  textarea.style.outline = 'none'
  textarea.style.boxShadow = 'none'
  textarea.style.background = 'transparent'
  document.body.appendChild(textarea)
  textarea.focus()
  textarea.select()

  try {
    const successful = document.execCommand('copy')
    document.body.removeChild(textarea)
    if (!successful) {
      throw new Error(t('transfer.taskDetail.copyFailed'))
    }
  } catch (err) {
    document.body.removeChild(textarea)
    throw err
  }
}

const formatScopeLabel = (scope) => {
  if (!scope) return ''
  if (scope === 'system') return t('transfer.taskDetail.scopeSystem')
  if (scope === 'local') return t('transfer.taskDetail.scopeLocal')
  return scope
}

const formatValue = (value) => {
  if (value === undefined || value === null) return '-'
  if (typeof value === 'boolean') return value ? t('transfer.taskDetail.boolYes') : t('transfer.taskDetail.boolNo')
  if (Array.isArray(value)) {
    const joined = value.filter(item => item !== undefined && item !== null && item !== '').join('、')
    return joined || '-'
  }
  if (typeof value === 'object') {
    try {
      const json = JSON.stringify(value)
      return json && json !== '{}' ? json : '-'
    } catch {
      return '-'
    }
  }
  const str = String(value)
  return str.trim() ? str : '-'
}

const getConnectorTypeLabel = (type) => {
  const lower = toLower(type)
  return connectorTypeLabels.value[lower] || (type ? type.toUpperCase() : '')
}

const inferScope = (config, fallbackId) => {
  const raw = toLower(config.scope)
  if (raw) return raw
  if (fallbackId !== undefined && fallbackId !== null) return 'system'
  if (config.connection_info) return 'local'
  return ''
}

const addItem = (items, label, value, span) => {
  const display = formatValue(value)
  if (display === '-') return
  items.push({ label, value: display, span })
}

const buildConnectorDetails = (role) => {
  const config = task.value?.config?.[role] || {}
  const fallbackId = toNumber(config.engine_id)
  const items = []

  const scope = inferScope(config, fallbackId)
  addItem(items, t('transfer.taskDetail.rangeLabel'), formatScopeLabel(scope))

  if (scope === 'system') {
    const resourceId = fallbackId ?? config.system_engine_id
    const resource = resourceId !== undefined && resourceId !== null
      ? systemResourceMap.value.get(Number(resourceId))
      : null
    const resourceName = resource?.name
    const resourceType = resource?.resource_type || config.resource_type
    const labelParts = []
    if (resourceName) labelParts.push(resourceName)
    if (resourceId !== undefined && resourceId !== null) labelParts.push(`ID: ${resourceId}`)
    addItem(items, t('transfer.taskDetail.dataSource'), labelParts.length ? labelParts.join(' / ') : t('transfer.taskDetail.notConfigured'))
    addItem(items, t('transfer.taskDetail.connectionType'), getConnectorTypeLabel(resourceType))
  } else if (scope === 'local') {
    const resourceId = config.engine_id
    const resourceName = config.local_resource_name
    const labelParts = []
    if (resourceName) labelParts.push(resourceName)
    if (resourceId !== undefined && resourceId !== null) labelParts.push(`ID: ${resourceId}`)
    addItem(items, t('transfer.taskDetail.dataSource'), labelParts.length ? labelParts.join(' / ') : t('transfer.taskDetail.notConfigured'))
    addItem(items, t('transfer.taskDetail.connectionType'), getConnectorTypeLabel(config.resource_type || config.type || config.driver))
  } else {
    addItem(items, t('transfer.taskDetail.dataSource'), t('transfer.taskDetail.notConfigured'))
    addItem(items, t('transfer.taskDetail.connectionType'), getConnectorTypeLabel(config.resource_type || config.type || config.driver))
  }

  const queryType = toLower(config.queryType || config.query_type)
  if (config.table) {
    addItem(items, t('transfer.taskDetail.tableName'), config.table)
  }
  if (config.query) {
    const label = queryType === 'sql' ? t('transfer.taskDetail.sqlQuery') : t('transfer.taskDetail.query')
    addItem(items, label, config.query, 2)
  }
  if (config.path) {
    addItem(items, t('transfer.taskDetail.path'), config.path, 2)
  }

  if (config.prefix) {
    addItem(items, t('transfer.taskDetail.prefix'), config.prefix)
  }
  if (config.format) {
    addItem(items, t('transfer.taskDetail.format'), config.format)
  }
  if (config.encoding) {
    addItem(items, t('transfer.taskDetail.encoding'), config.encoding)
  }
  if (config.delimiter) {
    addItem(items, t('transfer.taskDetail.delimiter'), config.delimiter)
  }
  if (config.incremental_field) {
    addItem(items, t('transfer.taskDetail.incrementalField'), config.incremental_field)
    addItem(items, t('transfer.taskDetail.incrementalType'), incrementalTypeLabels.value[toLower(config.incremental_type)] || config.incremental_type)
  }
  if (config.include_patterns) {
    addItem(items, t('transfer.taskDetail.includePatterns'), config.include_patterns)
  }
  if (config.exclude_patterns) {
    addItem(items, t('transfer.taskDetail.excludePatterns'), config.exclude_patterns)
  }
  if (config.geometry_fields || config.geometry_field) {
    const fields = Array.isArray(config.geometry_fields)
      ? config.geometry_fields
      : [config.geometry_field].filter(Boolean)
    addItem(items, t('transfer.taskDetail.spatialFields'), fields)
  }
  if (config.spatial_format) {
    addItem(items, t('transfer.taskDetail.spatialFormat'), config.spatial_format)
  }
  if (config.has_header !== undefined) {
    addItem(items, t('transfer.taskDetail.hasHeader'), !!config.has_header)
  }
  if (config.recursive !== undefined) {
    addItem(items, t('transfer.taskDetail.recursive'), !!config.recursive)
  }
  if (config.overwrite !== undefined) {
    addItem(items, t('transfer.taskDetail.overwrite'), !!config.overwrite)
  }

  const connection =
    config.connection_info ||
    (scope === 'system'
      ? (systemResourceMap.value.get(Number(config.system_engine_id ?? fallbackId))?.connection_info || {})
      : {})
  if (connection.host) {
    const host = connection.port ? `${connection.host}:${connection.port}` : connection.host
    addItem(items, t('transfer.taskDetail.host'), host)
  }
  if (connection.database) {
    addItem(items, t('transfer.taskDetail.database'), connection.database)
  }
  if (connection.schema) {
    addItem(items, 'Schema', connection.schema)
  }
  if (connection.endpoint) {
    addItem(items, 'Endpoint', connection.endpoint)
  }
  if (connection.bucket) {
    addItem(items, t('transfer.taskDetail.bucket'), connection.bucket)
  }
  if (connection.region) {
    addItem(items, t('transfer.taskDetail.region'), connection.region)
  }
  if (connection.path) {
    addItem(items, t('transfer.taskDetail.directory'), connection.path)
  }

  return items
}

const sourceDetails = computed(() => buildConnectorDetails('source'))
const targetDetails = computed(() => buildConnectorDetails('target'))

const formattedConfig = computed(() => {
  try {
    // 显示数据库中存储的原始任务配置，而不是解析后的 Worker 配置
    const originalTaskConfig = {
      task_id: task.value.id,
      name: task.value.name,
      description: task.value.description,
      schedule: task.value.schedule || '',
      mode: task.value.mode || 'batch',
      batch_size: task.value.batch_size,
      retry_policy: task.value.retry_policy || null,
      source_config: task.value.config?.source || {},
      target_config: task.value.config?.target || {},
      mappings: mappings.value || []
    }
    return JSON.stringify(originalTaskConfig, null, 2)
  } catch (error) {
    console.error('格式化配置失败:', error)
    return '{}'
  }
})

const openJsonDialog = () => {
  jsonDialogVisible.value = true
}

const handleCopyJson = () => {
  try {
    copyToClipboard(formattedConfig.value)
    ElMessage.success(t('transfer.taskDetail.copiedToClipboard'))
  } catch (error) {
    console.error('复制失败:', error)
    ElMessage.error(t('transfer.taskDetail.copyFailed'))
  }
}

let refreshTimer = null

onMounted(() => {
  loadTask()
  loadSystemResources()
  refreshTimer = setInterval(loadTask, 5000)
})

onBeforeUnmount(() => {
  if (refreshTimer) {
    clearInterval(refreshTimer)
    refreshTimer = null
  }
})
</script>

<style scoped>
.task-detail {
  padding: 20px;
}

.header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.detail-text {
  white-space: pre-wrap;
  word-break: break-word;
}

.json-dialog-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.json-pre {
  background-color: var(--addp-bg-secondary);
  border-radius: 6px;
  padding: 16px;
  font-size: 12px;
  line-height: 1.6;
  max-height: 420px;
  overflow: auto;
  color: var(--addp-text-primary);
}
</style>
