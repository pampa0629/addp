<template>
  <el-form
    ref="formRef"
    :model="formState"
    :rules="computedRules"
    :label-width="labelWidth"
  >
    <el-form-item
      v-if="typeOptions && typeOptions.length"
      label="存储引擎类型"
      prop="resource_type"
    >
      <el-select
        v-model="formState.resource_type"
        placeholder="请选择存储引擎类型"
        :disabled="isEdit && disableTypeChange"
        @change="handleTypeChange"
      >
        <el-option
          v-for="option in typeOptions"
          :key="option.value"
          :label="option.label"
          :value="option.value"
        />
      </el-select>
    </el-form-item>

    <el-form-item label="名称" prop="name">
      <el-input v-model="formState.name" placeholder="请输入资源名称" />
    </el-form-item>

    <el-form-item label="描述" prop="description">
      <el-input
        v-model="formState.description"
        type="textarea"
        :rows="2"
        placeholder="请输入资源描述"
      />
    </el-form-item>

    <el-form-item v-if="showActiveSwitch" label="激活状态">
      <el-switch v-model="formState.is_active" />
      <span style="margin-left: 10px; font-size: 12px; color: var(--el-text-color-secondary)">
        禁用后，该资源将不会出现在可用列表中
      </span>
    </el-form-item>

    <!-- PostgreSQL -->
    <template v-if="formState.resource_type === 'postgresql'">
      <el-form-item label="主机地址" prop="connection_info.host">
        <el-input v-model="formState.connection_info.host" placeholder="localhost" />
      </el-form-item>
      <el-form-item label="端口" prop="connection_info.port">
        <el-input-number v-model="formState.connection_info.port" :min="1" :max="65535" />
      </el-form-item>
      <el-form-item label="数据库名" prop="connection_info.database">
        <el-input v-model="formState.connection_info.database" placeholder="数据库名称" />
      </el-form-item>
      <el-form-item label="用户名" prop="connection_info.user">
        <el-input v-model="formState.connection_info.user" placeholder="数据库用户名" />
      </el-form-item>
      <el-form-item label="密码" prop="connection_info.password">
        <el-input
          v-model="formState.connection_info.password"
          type="password"
          placeholder="数据库密码"
          show-password
        />
      </el-form-item>
      <div v-if="hasStoredPassword" class="field-hint">
        已存储密码，如无需修改请保持占位符不变。
      </div>
      <el-form-item label="SSL 模式">
        <el-select v-model="formState.connection_info.sslmode">
          <el-option label="禁用 (disable)" value="disable" />
          <el-option label="要求 (require)" value="require" />
          <el-option label="验证CA (verify-ca)" value="verify-ca" />
          <el-option label="完全验证 (verify-full)" value="verify-full" />
        </el-select>
      </el-form-item>
    </template>

    <!-- Apache Doris -->
    <template v-else-if="formState.resource_type === 'doris'">
      <el-form-item label="主机地址" prop="connection_info.host">
        <el-input v-model="formState.connection_info.host" placeholder="localhost" />
      </el-form-item>
      <el-form-item label="端口" prop="connection_info.port">
        <el-input-number v-model="formState.connection_info.port" :min="1" :max="65535" />
      </el-form-item>
      <el-form-item label="数据库名" prop="connection_info.database">
        <el-input v-model="formState.connection_info.database" placeholder="数据库名称" />
      </el-form-item>
      <el-form-item label="用户名" prop="connection_info.user">
        <el-input v-model="formState.connection_info.user" placeholder="数据库用户名" />
      </el-form-item>
      <el-form-item label="密码（可选）" prop="connection_info.password">
        <el-input
          v-model="formState.connection_info.password"
          type="password"
          placeholder="留空表示无密码（Doris 默认）"
          show-password
        />
      </el-form-item>
      <div v-if="hasStoredPassword" class="field-hint">
        已存储密码，如无需修改请保持占位符不变。
      </div>
      <div v-else class="field-hint">
        Doris 默认 root 用户无密码，可留空。如已设置密码请填写。
      </div>
    </template>

    <!-- ClickHouse -->
    <template v-else-if="formState.resource_type === 'clickhouse'">
      <el-form-item label="主机地址" prop="connection_info.host">
        <el-input v-model="formState.connection_info.host" placeholder="localhost" />
      </el-form-item>
      <el-form-item label="端口" prop="connection_info.port">
        <el-input-number v-model="formState.connection_info.port" :min="1" :max="65535" />
      </el-form-item>
      <el-form-item label="数据库名" prop="connection_info.database">
        <el-input v-model="formState.connection_info.database" placeholder="default" />
      </el-form-item>
      <el-form-item label="用户名" prop="connection_info.user">
        <el-input v-model="formState.connection_info.user" placeholder="default" />
      </el-form-item>
      <el-form-item label="密码（可选）" prop="connection_info.password">
        <el-input
          v-model="formState.connection_info.password"
          type="password"
          placeholder="留空表示无密码"
          show-password
        />
      </el-form-item>
      <div v-if="hasStoredPassword" class="field-hint">
        已存储密码，如无需修改请保持占位符不变。
      </div>
      <div v-else class="field-hint">
        ClickHouse 默认 default 用户无密码。默认端口: 9000 (Native), 8123 (HTTP)
      </div>
    </template>

    <!-- MongoDB -->
    <template v-else-if="formState.resource_type === 'mongodb'">
      <el-form-item label="主机地址" prop="connection_info.host">
        <el-input v-model="formState.connection_info.host" placeholder="localhost" />
      </el-form-item>
      <el-form-item label="端口" prop="connection_info.port">
        <el-input-number v-model="formState.connection_info.port" :min="1" :max="65535" />
      </el-form-item>
      <el-form-item label="数据库名" prop="connection_info.database">
        <el-input v-model="formState.connection_info.database" placeholder="admin" />
      </el-form-item>
      <el-form-item label="用户名（可选）">
        <el-input v-model="formState.connection_info.user" placeholder="留空表示无需认证" />
      </el-form-item>
      <el-form-item label="密码（可选）" prop="connection_info.password">
        <el-input
          v-model="formState.connection_info.password"
          type="password"
          placeholder="留空表示无需认证"
          show-password
        />
      </el-form-item>
      <div v-if="hasStoredPassword" class="field-hint">
        已存储密码，如无需修改请保持占位符不变。
      </div>
      <div v-else class="field-hint">
        MongoDB 默认端口: 27017。如果启用了认证，请填写用户名和密码。
      </div>
    </template>

    <!-- MinIO / S3 -->
    <template v-else-if="formState.resource_type === 'minio' || formState.resource_type === 's3'">
      <el-form-item label="端点地址" prop="connection_info.endpoint">
        <el-input v-model="formState.connection_info.endpoint" placeholder="localhost:9002" />
      </el-form-item>
      <el-form-item label="Access Key" prop="connection_info.access_key">
        <el-input v-model="formState.connection_info.access_key" placeholder="Access Key" />
      </el-form-item>
      <el-form-item label="Secret Key" prop="connection_info.secret_key">
        <el-input
          v-model="formState.connection_info.secret_key"
          type="password"
          placeholder="Secret Key"
          show-password
        />
      </el-form-item>
      <div v-if="hasStoredSecretKey" class="field-hint">
        已存储密钥，如无需修改请保持占位符不变。
      </div>
      <el-form-item label="Bucket">
        <el-input v-model="formState.connection_info.bucket" placeholder="存储桶名称（可选）" />
      </el-form-item>
      <el-form-item label="使用 SSL">
        <el-switch v-model="formState.connection_info.use_ssl" />
      </el-form-item>
    </template>

    <!-- SpatiaLite / SQLite (file-based) -->
    <template v-else-if="formState.resource_type === 'spatialite' || formState.resource_type === 'sqlite'">
      <el-form-item label="文件路径" prop="connection_info.file_path">
        <el-input v-model="formState.connection_info.file_path" placeholder="/path/to/data.sqlite 或 .spatialite" />
      </el-form-item>
      <div class="field-hint">
        该连接器读取本地 SQLite 数据库（建议为 SpatiaLite 扩展），任务执行节点需要能访问该文件路径。
      </div>
    </template>

    <!-- Apache Spark -->
    <template v-else-if="formState.resource_type === 'spark'">
      <el-form-item label="主机地址" prop="connection_info.host">
        <el-input v-model="formState.connection_info.host" placeholder="host.docker.internal" />
      </el-form-item>
      <el-form-item label="端口" prop="connection_info.port">
        <el-input-number v-model="formState.connection_info.port" :min="1" :max="65535" placeholder="10000" />
      </el-form-item>
      <el-form-item label="数据库名" prop="connection_info.database">
        <el-input v-model="formState.connection_info.database" placeholder="default" />
      </el-form-item>
      <el-form-item label="用户名（可选）">
        <el-input v-model="formState.connection_info.username" placeholder="留空表示无需认证" />
      </el-form-item>
      <el-form-item label="密码（可选）">
        <el-input
          v-model="formState.connection_info.password"
          type="password"
          placeholder="留空表示无需认证"
          show-password
        />
      </el-form-item>
      <div class="field-hint">
        Apache Spark 通过 Thrift Server (Hive2 协议) 连接，支持 Sedona 空间扩展。默认端口: 10000
      </div>
    </template>

    <!-- 元数据扫描配置 -->
    <el-divider content-position="left">
      <span style="cursor: pointer; user-select: none;" @click="scanConfigExpanded = !scanConfigExpanded">
        元数据扫描配置（可选）
        <el-icon style="margin-left: 4px;">
          <component :is="scanConfigExpanded ? 'ArrowDown' : 'ArrowRight'" />
        </el-icon>
      </span>
    </el-divider>

    <!-- 扫描配置内容（可折叠） -->
    <template v-if="scanConfigExpanded">
      <!-- 1. 立即扫描开关 -->
      <el-form-item label="注册后立即扫描">
        <el-switch v-model="immediateScanEnabled" />
        <span style="margin-left: 10px; font-size: 12px; color: var(--el-text-color-secondary)">
          保存资源后立即触发一次全量扫描，快速生成元数据
        </span>
      </el-form-item>

      <!-- 1.1 立即扫描深度配置（仅在立即扫描启用时显示） -->
      <template v-if="immediateScanEnabled">
        <el-form-item label="立即扫描深度" style="margin-left: 30px;">
          <el-radio-group v-model="formState.scan_config.immediate_depth">
            <el-radio label="basic">基础扫描（推荐）</el-radio>
            <el-radio label="deep">深度扫描</el-radio>
          </el-radio-group>
          <div class="field-hint">
            基础扫描仅获取结构信息（表名、字段类型），速度快（秒级）<br/>
            深度扫描包含统计信息（行数、大小等），可能需要较长时间（分钟级）
          </div>
        </el-form-item>
      </template>

      <!-- 2. 定时扫描开关 -->
      <el-form-item label="定时自动扫描">
        <el-switch v-model="scheduledScanEnabled" />
        <span style="margin-left: 10px; font-size: 12px; color: var(--el-text-color-secondary)">
          按设定的频率自动深度扫描，保持元数据最新
        </span>
      </el-form-item>

      <!-- 2.1 定时扫描详细配置（仅在开关打开时显示） -->
      <template v-if="scheduledScanEnabled">
        <el-form-item label="扫描频率" style="margin-left: 30px;">
          <el-radio-group v-model="formState.scan_config.schedule_type">
            <el-radio label="daily">每天</el-radio>
            <el-radio label="weekly">每周</el-radio>
          </el-radio-group>
        </el-form-item>

        <el-form-item label="执行时间" style="margin-left: 30px;">
          <el-time-select
            v-model="formState.scan_config.schedule_time"
            start="00:00"
            step="01:00"
            end="23:00"
            placeholder="选择时间"
          />
        </el-form-item>

        <el-form-item
          v-if="formState.scan_config.schedule_type === 'weekly'"
          label="执行日期"
          style="margin-left: 30px;"
        >
          <el-checkbox-group v-model="formState.scan_config.schedule_value">
            <el-checkbox :label="1">周一</el-checkbox>
            <el-checkbox :label="2">周二</el-checkbox>
            <el-checkbox :label="3">周三</el-checkbox>
            <el-checkbox :label="4">周四</el-checkbox>
            <el-checkbox :label="5">周五</el-checkbox>
            <el-checkbox :label="6">周六</el-checkbox>
            <el-checkbox :label="0">周日</el-checkbox>
          </el-checkbox-group>
        </el-form-item>

        <!-- 2.2 定时扫描深度提示（固定深度扫描） -->
        <el-form-item label="扫描深度" style="margin-left: 30px;">
          <span style="color: var(--el-text-color-regular)">深度扫描（固定）</span>
          <div class="field-hint">
            定时扫描固定使用深度扫描，获取完整的统计信息（行数、大小、字段分布等）
          </div>
        </el-form-item>
      </template>
    </template>
  </el-form>
</template>

<script setup>
import { computed, reactive, ref, watch, nextTick } from 'vue'
import { ArrowDown, ArrowRight } from '@element-plus/icons-vue'

const SENSITIVE_PLACEHOLDER = '********'

const props = defineProps({
  modelValue: {
    type: Object,
    default: () => ({})
  },
  isEdit: {
    type: Boolean,
    default: false
  },
  disableTypeChange: {
    type: Boolean,
    default: true
  },
  typeOptions: {
    type: Array,
    default: () => ([
      { label: 'PostgreSQL', value: 'postgresql' },
      { label: 'MySQL', value: 'mysql' },
      { label: 'Apache Doris', value: 'doris' },
      { label: 'ClickHouse', value: 'clickhouse' },
      { label: 'MongoDB', value: 'mongodb' },
      { label: 'MinIO', value: 'minio' },
      { label: 'Apache Spark', value: 'spark' },
      { label: 'SpatiaLite/SQLite', value: 'spatialite' }
    ])
  },
  showActiveSwitch: {
    type: Boolean,
    default: true
  },
  labelWidth: {
    type: String,
    default: '120px'
  }
})

const emit = defineEmits(['update:modelValue', 'type-change'])

const formRef = ref(null)
const hasStoredPassword = ref(false)
const hasStoredSecretKey = ref(false)
const immediateScanEnabled = ref(true)  // 默认启用立即扫描
const scheduledScanEnabled = ref(true)  // 默认启用定时扫描
const scanConfigExpanded = ref(false)   // 扫描配置折叠状态（默认折叠）

  const ensureConnectionDefaults = (form) => {
  if (!form.connection_info || typeof form.connection_info !== 'object') {
    form.connection_info = {}
  }

  const original = { ...(form.connection_info || {}) }
  const hadPassword = original._has_password === true
  const hadSecret = original._has_secret_key === true

  if (form.resource_type === 'postgresql') {
    form.connection_info = {
      host: original.host ?? 'localhost',
      port: original.port ?? 5432,
      database: original.database ?? '',
      user: original.user ?? '',
      password: original.password ?? '',
      sslmode: original.sslmode ?? 'disable'
    }
  } else if (form.resource_type === 'mysql') {
    form.connection_info = {
      host: original.host ?? 'localhost',
      port: original.port ?? 3306,
      database: original.database ?? '',
      user: original.user ?? '',
      password: original.password ?? ''
    }
  } else if (form.resource_type === 'doris') {
    form.connection_info = {
      host: original.host ?? 'localhost',
      port: original.port ?? 9030,
      database: original.database ?? '',
      user: original.user ?? 'root',
      password: original.password ?? ''
    }
  } else if (form.resource_type === 'clickhouse') {
    form.connection_info = {
      host: original.host ?? 'localhost',
      port: original.port ?? 9000,
      database: original.database ?? 'default',
      user: original.user ?? 'default',
      password: original.password ?? ''
    }
  } else if (form.resource_type === 'mongodb') {
    form.connection_info = {
      host: original.host ?? 'localhost',
      port: original.port ?? 27017,
      database: original.database ?? 'admin',
      user: original.user ?? '',
      password: original.password ?? ''
    }
  } else if (form.resource_type === 'minio' || form.resource_type === 's3') {
    form.connection_info = {
      endpoint: original.endpoint ?? 'localhost:9002',
      access_key: original.access_key ?? '',
      secret_key: original.secret_key ?? '',
      bucket: original.bucket ?? '',
      use_ssl: original.use_ssl ?? false
    }
  } else if (form.resource_type === 'spatialite' || form.resource_type === 'sqlite') {
    form.connection_info = {
      file_path: original.file_path ?? ''
    }
  } else if (form.resource_type === 'spark') {
    form.connection_info = {
      host: original.host ?? 'host.docker.internal',
      port: original.port ?? 10000,
      database: original.database ?? 'default',
      username: original.username ?? '',
      password: original.password ?? ''
    }
  } else {
    form.connection_info = { ...original }
  }

  if (hadPassword) {
    form.connection_info._has_password = true
  } else {
    delete form.connection_info._has_password
  }

  if (hadSecret) {
    form.connection_info._has_secret_key = true
  } else {
    delete form.connection_info._has_secret_key
  }
}

const applySensitiveHints = () => {
  hasStoredPassword.value = formState.connection_info?._has_password === true
  if (hasStoredPassword.value && (!formState.connection_info.password || formState.connection_info.password === '')) {
    formState.connection_info.password = SENSITIVE_PLACEHOLDER
  }

  hasStoredSecretKey.value = formState.connection_info?._has_secret_key === true
  if (hasStoredSecretKey.value && (!formState.connection_info.secret_key || formState.connection_info.secret_key === '')) {
    formState.connection_info.secret_key = SENSITIVE_PLACEHOLDER
  }
}

const formState = reactive({
  resource_type: '',
  name: '',
  description: '',
  is_active: true,
  connection_info: {},
  scan_config: {
    enabled: false,  // 兼容旧版
    immediate_scan: true,  // 默认启用立即扫描
    immediate_depth: 'basic',  // 立即扫描默认基础
    scheduled_scan: true,  // 默认启用定时扫描
    schedule_type: 'daily',  // 默认每天
    schedule_time: '00:00',  // 凌晨执行
    schedule_value: []
  }
})

const syncFromProps = (value) => {
  formState.resource_type = value.resource_type || ''
  formState.name = value.name || ''
  formState.description = value.description || ''
  formState.is_active = value.is_active !== undefined ? value.is_active : true
  formState.connection_info = { ...(value.connection_info || {}) }

  // 同步扫描配置
  if (value.scan_config) {
    formState.scan_config = {
      enabled: value.scan_config.enabled || false,
      immediate_scan: value.scan_config.immediate_scan !== undefined ? value.scan_config.immediate_scan : true,
      immediate_depth: value.scan_config.immediate_depth || 'basic',
      scheduled_scan: value.scan_config.scheduled_scan !== undefined ? value.scan_config.scheduled_scan : true,
      schedule_type: value.scan_config.schedule_type || 'daily',
      schedule_time: value.scan_config.schedule_time || '00:00',
      schedule_value: value.scan_config.schedule_value || []
    }
    immediateScanEnabled.value = formState.scan_config.immediate_scan
    scheduledScanEnabled.value = formState.scan_config.scheduled_scan
  } else {
    // 新建资源时使用默认值
    immediateScanEnabled.value = true
    scheduledScanEnabled.value = true
  }

  ensureConnectionDefaults(formState)
  applySensitiveHints()
}

// Avoid infinite update loop between props -> local state -> emit -> props
let syncingFromProps = false
watch(
  () => props.modelValue,
  async (value) => {
    syncingFromProps = true
    try {
      syncFromProps(value || {})
    } finally {
      // ensure local watchers run while the flag is set
      await nextTick()
      syncingFromProps = false
    }
  },
  { immediate: true, deep: true }
)

// 计算属性：判断是否启用了任何扫描配置
const scanConfigEnabled = computed(() => {
  return immediateScanEnabled.value || scheduledScanEnabled.value
})

// 监听立即扫描开关，同步到 formState
watch(immediateScanEnabled, (value) => {
  formState.scan_config.immediate_scan = value
})

// 监听定时扫描开关，同步到 formState
watch(scheduledScanEnabled, (value) => {
  formState.scan_config.scheduled_scan = value
  if (!value) {
    // 禁用定时扫描时，重置相关配置
    formState.scan_config.schedule_type = 'daily'
    formState.scan_config.schedule_time = '00:00'
    formState.scan_config.schedule_value = []
  }
})

watch(
  formState,
  (value) => {
    // Skip emitting while we are syncing from props to prevent recursion
    if (syncingFromProps) return
    const payload = {
      resource_type: value.resource_type,
      name: value.name,
      description: value.description,
      is_active: value.is_active,
      connection_info: { ...value.connection_info }
    }
    // 只在启用扫描时发送 scan_config
    if (scanConfigEnabled.value) {
      payload.scan_config = { ...value.scan_config }
    }
    emit('update:modelValue', payload)
  },
  { deep: true }
)

const rules = {
  resource_type: [{ required: true, message: '请选择存储引擎类型', trigger: 'change' }],
  name: [{ required: true, message: '请输入资源名称', trigger: 'blur' }],
  'connection_info.host': [{ required: true, message: '请输入主机地址', trigger: 'blur' }],
  'connection_info.port': [{ required: true, message: '请输入端口', trigger: 'change' }],
  'connection_info.database': [{ required: true, message: '请输入数据库名', trigger: 'blur' }],
  'connection_info.user': [{ required: true, message: '请输入用户名', trigger: 'blur' }],
  'connection_info.password': [{ required: true, message: '请输入密码', trigger: 'blur' }],
  'connection_info.endpoint': [{ required: true, message: '请输入端点地址', trigger: 'blur' }],
  'connection_info.access_key': [{ required: true, message: '请输入 Access Key', trigger: 'blur' }],
  'connection_info.secret_key': [{ required: true, message: '请输入 Secret Key', trigger: 'blur' }],
  'connection_info.file_path': [{ required: true, message: '请输入文件路径', trigger: 'blur' }]
}

const computedRules = computed(() => {
  if (formState.resource_type === 'postgresql') {
    return {
      resource_type: rules.resource_type,
      name: rules.name,
      'connection_info.host': rules['connection_info.host'],
      'connection_info.port': rules['connection_info.port'],
      'connection_info.database': rules['connection_info.database'],
      'connection_info.user': rules['connection_info.user'],
      'connection_info.password': rules['connection_info.password']
    }
  }

  if (formState.resource_type === 'mysql') {
    return {
      resource_type: rules.resource_type,
      name: rules.name,
      'connection_info.host': rules['connection_info.host'],
      'connection_info.port': rules['connection_info.port'],
      'connection_info.database': rules['connection_info.database'],
      'connection_info.user': rules['connection_info.user'],
      'connection_info.password': rules['connection_info.password']
    }
  }

  if (formState.resource_type === 'doris') {
    // Doris 默认 root 用户密码为空，所以密码非必填
    return {
      resource_type: rules.resource_type,
      name: rules.name,
      'connection_info.host': rules['connection_info.host'],
      'connection_info.port': rules['connection_info.port'],
      'connection_info.database': rules['connection_info.database'],
      'connection_info.user': rules['connection_info.user']
      // 注意：没有密码验证规则
    }
  }

  if (formState.resource_type === 'clickhouse') {
    // ClickHouse 默认 default 用户密码为空，所以密码非必填
    return {
      resource_type: rules.resource_type,
      name: rules.name,
      'connection_info.host': rules['connection_info.host'],
      'connection_info.port': rules['connection_info.port'],
      'connection_info.database': rules['connection_info.database'],
      'connection_info.user': rules['connection_info.user']
      // 注意：没有密码验证规则
    }
  }

  if (formState.resource_type === 'mongodb') {
    // MongoDB 用户名和密码可选（无认证模式）
    return {
      resource_type: rules.resource_type,
      name: rules.name,
      'connection_info.host': rules['connection_info.host'],
      'connection_info.port': rules['connection_info.port'],
      'connection_info.database': rules['connection_info.database']
      // 注意：user 和 password 都不是必填
    }
  }

  if (formState.resource_type === 'minio' || formState.resource_type === 's3') {
    return {
      resource_type: rules.resource_type,
      name: rules.name,
      'connection_info.endpoint': rules['connection_info.endpoint'],
      'connection_info.access_key': rules['connection_info.access_key'],
      'connection_info.secret_key': rules['connection_info.secret_key']
    }
  }

  if (formState.resource_type === 'spatialite' || formState.resource_type === 'sqlite') {
    return {
      resource_type: rules.resource_type,
      name: rules.name,
      'connection_info.file_path': rules['connection_info.file_path']
    }
  }

  if (formState.resource_type === 'spark') {
    return {
      resource_type: rules.resource_type,
      name: rules.name,
      'connection_info.host': rules['connection_info.host'],
      'connection_info.port': rules['connection_info.port'],
      'connection_info.database': rules['connection_info.database']
    }
  }

  return {
    resource_type: rules.resource_type,
    name: rules.name
  }
})

const handleTypeChange = (type) => {
  ensureConnectionDefaults(formState)
  applySensitiveHints()
  emit('type-change', type)
}

const validate = async () => {
  if (!formRef.value) return true
  try {
    await formRef.value.validate()
    return true
  } catch {
    return false
  }
}

const reset = () => {
  syncFromProps({})
  formRef.value?.clearValidate()
}

defineExpose({
  validate,
  reset,
  formRef,
  formState
})

watch(
  () => formState.connection_info.password,
  (value) => {
    // 跳过从 props 同步时的处理,避免循环
    if (syncingFromProps) {
      return
    }

    const metaFlag = formState.connection_info?._has_password === true
    if (!metaFlag && value === SENSITIVE_PLACEHOLDER) {
      formState.connection_info.password = ''
      return
    }

    if (value === SENSITIVE_PLACEHOLDER) {
      hasStoredPassword.value = true
      return
    }

    const hasValue = !!value
    formState.connection_info._has_password = hasValue
    hasStoredPassword.value = hasValue
    if (!hasValue) {
      delete formState.connection_info._has_password
    }
  }
)

watch(
  () => formState.connection_info.secret_key,
  (value) => {
    // 跳过从 props 同步时的处理,避免循环
    if (syncingFromProps) {
      return
    }

    const metaFlag = formState.connection_info?._has_secret_key === true
    if (!metaFlag && value === SENSITIVE_PLACEHOLDER) {
      formState.connection_info.secret_key = ''
      return
    }

    if (value === SENSITIVE_PLACEHOLDER) {
      hasStoredSecretKey.value = true
      return
    }

    const hasValue = !!value
    formState.connection_info._has_secret_key = hasValue
    hasStoredSecretKey.value = hasValue
    if (!hasValue) {
      delete formState.connection_info._has_secret_key
    }
  }
)
</script>

<style scoped>
.field-hint {
  margin: -8px 0 16px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
</style>
