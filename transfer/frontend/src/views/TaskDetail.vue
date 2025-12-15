<template>
  <div class="task-detail">
    <el-button @click="$router.back()" style="margin-bottom: 20px;">
      <el-icon><ArrowLeft /></el-icon>
      返回
    </el-button>

    <el-card v-loading="loading">
      <template #header>
        <div class="header">
          <span>任务详情 - {{ task.name }}</span>
          <div>
            <template v-if="isManualTask">
              <el-button type="primary" @click="handleExecute" :disabled="task.status === 'running'">
                执行
              </el-button>
              <el-button type="warning" @click="handleStop" :disabled="task.status !== 'running'">
                停止
              </el-button>
            </template>
            <template v-else>
              <el-button type="primary" @click="handleResume" :disabled="!canStartSchedule">
                启动
              </el-button>
              <el-button type="warning" @click="handlePause" :disabled="!canPauseSchedule">
                暂停
              </el-button>
              <el-button @click="handleExecute" :disabled="task.status === 'running'">
                单次执行
              </el-button>
            </template>
            <el-button @click="handleEdit" :disabled="!canEditTask">编辑</el-button>
            <el-button @click="openJsonDialog">查看JSON配置</el-button>
          </div>
        </div>
      </template>

      <el-descriptions :column="2" border>
        <el-descriptions-item label="任务ID">{{ task.id }}</el-descriptions-item>
        <el-descriptions-item label="任务名称">{{ task.name }}</el-descriptions-item>
        <el-descriptions-item label="执行模式">{{ task.mode }}</el-descriptions-item>
        <el-descriptions-item label="状态">
          <el-tag :type="getTaskStatusTagType(task)">{{ getTaskStatusLabel(task) }}</el-tag>
        </el-descriptions-item>
        <el-descriptions-item label="最后执行状态">
          <el-tag :type="getExecutionTagType(task.last_execution_status)">
            {{ getLastExecutionLabel(task.last_execution_status) }}
          </el-tag>
        </el-descriptions-item>
        <el-descriptions-item label="最后执行时间">
          {{ formatDate(task.last_execution_finished_at || task.last_execution_started_at) }}
        </el-descriptions-item>
        <el-descriptions-item label="批量大小">{{ task.batch_size }}</el-descriptions-item>
        <el-descriptions-item label="定时调度">{{ formatSchedule(task.schedule) }}</el-descriptions-item>
        <el-descriptions-item label="创建时间" :span="2">
          {{ formatDate(task.created_at) }}
        </el-descriptions-item>
        <el-descriptions-item label="描述" :span="2">
          {{ task.description || '-' }}
        </el-descriptions-item>
      </el-descriptions>

      <el-divider content-position="left">源数据源</el-divider>
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

      <el-divider content-position="left">目标数据源</el-divider>
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

      <el-divider>执行记录</el-divider>
      <el-table :data="executions" stripe>
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column prop="status" label="状态" width="100">
          <template #default="{ row }">
            <el-tag :type="getExecutionTagType(row.status)">
              {{ getExecutionLabel(row.status) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="records_written" label="已写入记录" width="120" />
        <el-table-column prop="start_time" label="开始时间" width="180">
          <template #default="{ row }">
            {{ formatDate(row.start_time) }}
          </template>
        </el-table-column>
        <el-table-column prop="end_time" label="结束时间" width="180">
          <template #default="{ row }">
            {{ formatDate(row.end_time) }}
          </template>
        </el-table-column>
        <el-table-column label="操作" width="150">
          <template #default="{ row }">
            <el-button size="small" @click="viewExecution(row.id)">详情</el-button>
            <el-button size="small" type="primary" @click="retryExecution(row.id)"
              v-if="row.status === 'failed'">
              重试
            </el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <el-dialog v-model="jsonDialogVisible" width="700px">
      <template #header>
        <div class="json-dialog-header">
          <span>JSON 配置</span>
          <el-button size="small" type="primary" @click="handleCopyJson">复制</el-button>
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
import { taskAPI, executionAPI } from '@/api/tasks'
import { describeCron } from '@common-ui'
import { systemResourcesAPI } from '@/api/systemResources'

const route = useRoute()
const router = useRouter()
const loading = ref(false)
const task = ref({})
const executions = ref([])
const mappings = ref([])
const isManualTask = computed(() => !task.value?.schedule)
const canStartSchedule = computed(() => {
  const status = task.value?.status
  return !!status && ['pending', 'paused'].includes(status)
})
const canPauseSchedule = computed(() => {
  const status = task.value?.status
  return !!status && ['scheduled', 'running'].includes(status)
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

const connectorTypeLabels = {
  postgresql: 'PostgreSQL',
  mysql: 'MySQL',
  s3: '对象存储（S3 兼容）',
  minio: '对象存储（MinIO）',
  oss: '对象存储（OSS）',
  csv: 'CSV 文件',
  json: 'JSON 文件'
}

const incrementalTypeLabels = {
  timestamp: '时间戳',
  numeric: '数值',
  string: '字符串'
}

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
    const data = await systemResourcesAPI.list()
    systemResources.value = Array.isArray(data) ? data : []
  } catch (error) {
    console.error('加载系统资源失败:', error)
  }
}

const handleExecute = async () => {
  await taskAPI.start(route.params.id)
  const message = isManualTask.value ? '任务执行已提交' : '单次执行已提交'
  ElMessage.success(message)
  await loadTask()
}

const handleStop = async () => {
  try {
    await ElMessageBox.confirm('确定要停止该任务吗？', '提示', {
      confirmButtonText: '确定',
      cancelButtonText: '取消',
      type: 'warning'
    })
    await taskAPI.stop(route.params.id)
    ElMessage.success('任务已停止')
    await loadTask()
  } catch (error) {
    if (error !== 'cancel') {
      console.error('停止任务失败:', error)
    }
  }
}

const handlePause = async () => {
  try {
    await ElMessageBox.confirm('确定要暂停该定时任务吗？', '提示', {
      confirmButtonText: '确定',
      cancelButtonText: '取消',
      type: 'warning'
    })
    await taskAPI.pause(route.params.id)
    ElMessage.success('任务已暂停')
    await loadTask()
  } catch (error) {
    if (error !== 'cancel') {
      console.error('暂停任务失败:', error)
    }
  }
}

const handleResume = async () => {
  await taskAPI.resume(route.params.id)
  ElMessage.success('任务已启用')
  await loadTask()
}

const handleEdit = () => {
  if (!canEditTask.value) {
    ElMessage.warning('任务执行中，无法编辑')
    return
  }
  router.push(`/tasks/${route.params.id}/edit`)
}

const viewExecution = (id) => {
  router.push(`/executions/${id}`)
}

const retryExecution = async (id) => {
  await executionAPI.retry(id)
  ElMessage.success('重试已提交')
  loadTask()
}

const getTaskStatusLabel = (taskData) => {
  if (!taskData) return '未执行'
  if (!taskData.schedule) {
    const labels = {
      pending: '未执行',
      running: '执行中',
      stopped: '已停止',
      completed: '已完成'
    }
    return labels[taskData.status] || '未执行'
  }
  if (['scheduled', 'running'].includes(taskData.status)) return '已启动'
  if (['pending', 'paused'].includes(taskData.status)) return '未启动'
  if (taskData.status === 'stopped') return '已停止'
  return '未启动'
}

const getTaskStatusTagType = (taskData) => {
  const label = getTaskStatusLabel(taskData)
  const types = {
    未执行: 'info',
    执行中: 'primary',
    已停止: 'info',
    已完成: 'success',
    已启动: 'primary',
    未启动: 'info'
  }
  return types[label] || ''
}

const getLastExecutionLabel = (status) => {
  if (!status || status === 'pending') return '未执行'
  if (status === 'running') return '执行中'
  if (status === 'success') return '成功'
  if (status === 'failed' || status === 'cancelled') return '失败'
  return status
}

const getExecutionTagType = (status) => {
  const label = getLastExecutionLabel(status)
  const types = {
    未执行: 'info',
    执行中: 'primary',
    成功: 'success',
    失败: 'danger'
  }
  return types[label] || 'info'
}

const getExecutionLabel = (status) => {
  const label = getLastExecutionLabel(status)
  return label === '未执行' ? '待开始' : label
}

const formatDate = (date) => {
  if (!date) return '-'
  try {
    const d = new Date(date)
    // 检查是否为有效日期
    if (isNaN(d.getTime())) return '-'

    // 使用更明确的格式化（避免年份错误）
    const year = d.getFullYear()
    const month = String(d.getMonth() + 1).padStart(2, '0')
    const day = String(d.getDate()).padStart(2, '0')
    const hours = String(d.getHours()).padStart(2, '0')
    const minutes = String(d.getMinutes()).padStart(2, '0')
    const seconds = String(d.getSeconds()).padStart(2, '0')

    return `${year}-${month}-${day} ${hours}:${minutes}:${seconds}`
  } catch (e) {
    return '-'
  }
}

const formatSchedule = (cron) => {
  if (!cron) return '手动执行'
  return describeCron(cron)
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
    'system_resource_id',
    'local_resource_id',
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
    if (['scope', 'system_resource_id', 'local_resource_id', 'local_resource_name', 'resource_type'].includes(key)) {
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
  const fallbackId = key === 'source' ? toNumber(task.value?.source_id) : toNumber(task.value?.target_id)

  let resource = null
  const embeddedId = toNumber(rawConfig.system_resource_id || rawConfig.resource_id)
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
  const explicit = toLower(config.type)
  if (explicit) return explicit
  if (config.driver) return 'jdbc'
  if (config.bucket || config.endpoint || config.file_name || config.file_type) return 's3'
  if (config.path || config.directory || config.file_path) return 'file'
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
    nullable: item.nullable !== undefined ? !!item.nullable : true,
    transform: item.transform || ''
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
    source_id: task.value.source_id ?? null,
    target_id: task.value.target_id ?? null,
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

const copyToClipboard = async (text) => {
  // 尝试使用现代 Clipboard API
  if (navigator?.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(text)
      return
    } catch (err) {
      console.warn('Clipboard API 失败，尝试降级方案:', err)
      // 降级到 execCommand
    }
  }

  // 降级方案：使用 document.execCommand
  const textarea = document.createElement('textarea')
  textarea.value = text
  textarea.style.position = 'fixed'
  textarea.style.left = '-9999px'
  textarea.style.top = '-9999px'
  textarea.style.opacity = '0'
  textarea.setAttribute('readonly', '')
  document.body.appendChild(textarea)

  try {
    textarea.focus()
    textarea.select()

    // 兼容 iOS
    const range = document.createRange()
    range.selectNodeContents(textarea)
    const selection = window.getSelection()
    if (selection) {
      selection.removeAllRanges()
      selection.addRange(range)
    }
    textarea.setSelectionRange(0, textarea.value.length)

    const successful = document.execCommand('copy')
    if (!successful) {
      throw new Error('execCommand 返回 false')
    }
  } finally {
    document.body.removeChild(textarea)
  }
}

const formatScopeLabel = (scope) => {
  if (!scope) return ''
  if (scope === 'system') return '系统资源'
  if (scope === 'local') return '本地存储引擎'
  return scope
}

const formatValue = (value) => {
  if (value === undefined || value === null) return '-'
  if (typeof value === 'boolean') return value ? '是' : '否'
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
  return connectorTypeLabels[lower] || (type ? type.toUpperCase() : '')
}

const inferScope = (config, fallbackId) => {
  const raw = toLower(config.scope)
  if (raw) return raw
  if (fallbackId !== undefined && fallbackId !== null) return 'system'
  if (config.system_resource_id !== undefined && config.system_resource_id !== null) return 'system'
  if (config.local_resource_id !== undefined && config.local_resource_id !== null) return 'local'
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
  const fallbackId = role === 'source' ? task.value?.source_id : task.value?.target_id
  const items = []

  const scope = inferScope(config, fallbackId)
  addItem(items, '范围', formatScopeLabel(scope))

  if (scope === 'system') {
    const resourceId = fallbackId ?? config.system_resource_id
    const resource = resourceId !== undefined && resourceId !== null
      ? systemResourceMap.value.get(Number(resourceId))
      : null
    const resourceName = resource?.name
    const resourceType = resource?.resource_type || config.resource_type
    const labelParts = []
    if (resourceName) labelParts.push(resourceName)
    if (resourceId !== undefined && resourceId !== null) labelParts.push(`ID: ${resourceId}`)
    addItem(items, '数据源', labelParts.length ? labelParts.join(' / ') : '未配置')
    addItem(items, '连接类型', getConnectorTypeLabel(resourceType))
  } else if (scope === 'local') {
    const resourceId = config.local_resource_id
    const resourceName = config.local_resource_name
    const labelParts = []
    if (resourceName) labelParts.push(resourceName)
    if (resourceId !== undefined && resourceId !== null) labelParts.push(`ID: ${resourceId}`)
    addItem(items, '数据源', labelParts.length ? labelParts.join(' / ') : '未配置')
    addItem(items, '连接类型', getConnectorTypeLabel(config.resource_type || config.type || config.driver))
  } else {
    addItem(items, '数据源', '未配置')
    addItem(items, '连接类型', getConnectorTypeLabel(config.resource_type || config.type || config.driver))
  }

  const queryType = toLower(config.queryType || config.query_type)
  if (config.table) {
    addItem(items, '表名', config.table)
  }
  if (config.query) {
    const label = queryType === 'sql' ? 'SQL 查询' : '查询'
    addItem(items, label, config.query, 2)
  }
  if (config.path) {
    addItem(items, '路径', config.path, 2)
  }

  if (config.prefix) {
    addItem(items, '前缀', config.prefix)
  }
  if (config.format) {
    addItem(items, '格式', config.format)
  }
  if (config.encoding) {
    addItem(items, '编码', config.encoding)
  }
  if (config.delimiter) {
    addItem(items, '分隔符', config.delimiter)
  }
  if (config.incremental_field) {
    addItem(items, '增量字段', config.incremental_field)
    addItem(items, '增量类型', incrementalTypeLabels[toLower(config.incremental_type)] || config.incremental_type)
  }
  if (config.include_patterns) {
    addItem(items, '包含模式', config.include_patterns)
  }
  if (config.exclude_patterns) {
    addItem(items, '排除模式', config.exclude_patterns)
  }
  if (config.geometry_fields || config.geometry_field) {
    const fields = Array.isArray(config.geometry_fields)
      ? config.geometry_fields
      : [config.geometry_field].filter(Boolean)
    addItem(items, '空间字段', fields)
  }
  if (config.spatial_format) {
    addItem(items, '空间格式', config.spatial_format)
  }
  if (config.has_header !== undefined) {
    addItem(items, '包含表头', !!config.has_header)
  }
  if (config.recursive !== undefined) {
    addItem(items, '递归遍历', !!config.recursive)
  }
  if (config.overwrite !== undefined) {
    addItem(items, '覆盖写入', !!config.overwrite)
  }

  const connection =
    config.connection_info ||
    (scope === 'system'
      ? (systemResourceMap.value.get(Number(config.system_resource_id ?? fallbackId))?.connection_info || {})
      : {})
  if (connection.host) {
    const host = connection.port ? `${connection.host}:${connection.port}` : connection.host
    addItem(items, '主机', host)
  }
  if (connection.database) {
    addItem(items, '数据库', connection.database)
  }
  if (connection.schema) {
    addItem(items, 'Schema', connection.schema)
  }
  if (connection.endpoint) {
    addItem(items, 'Endpoint', connection.endpoint)
  }
  if (connection.bucket) {
    addItem(items, '存储桶', connection.bucket)
  }
  if (connection.region) {
    addItem(items, '区域', connection.region)
  }
  if (connection.path) {
    addItem(items, '目录', connection.path)
  }

  return items
}

const sourceDetails = computed(() => buildConnectorDetails('source'))
const targetDetails = computed(() => buildConnectorDetails('target'))

const formattedConfig = computed(() => {
  try {
    const workerConfig = buildWorkerConfig()
    return JSON.stringify(workerConfig, null, 2)
  } catch (error) {
    console.error('格式化配置失败:', error)
    return '{}'
  }
})

const openJsonDialog = () => {
  jsonDialogVisible.value = true
}

const handleCopyJson = async () => {
  try {
    await copyToClipboard(formattedConfig.value)
    ElMessage.success('已复制到剪贴板')
  } catch (error) {
    console.error('复制失败:', error)
    ElMessage.error('复制失败，请重试')
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
  background-color: #f5f7fa;
  border-radius: 6px;
  padding: 16px;
  font-size: 12px;
  line-height: 1.6;
  max-height: 420px;
  overflow: auto;
  color: #303133;
}
</style>
