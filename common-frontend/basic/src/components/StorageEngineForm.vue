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

    <el-form-item v-if="showActiveSwitch" label="激活状态">
      <el-switch v-model="formState.is_active" />
    </el-form-item>
  </el-form>
</template>

<script setup>
import { computed, reactive, ref, watch, nextTick } from 'vue'

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
      { label: 'MinIO', value: 'minio' },
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
  connection_info: {}
})

const syncFromProps = (value) => {
  formState.resource_type = value.resource_type || ''
  formState.name = value.name || ''
  formState.description = value.description || ''
  formState.is_active = value.is_active !== undefined ? value.is_active : true
  formState.connection_info = { ...(value.connection_info || {}) }
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

watch(
  formState,
  (value) => {
    // Skip emitting while we are syncing from props to prevent recursion
    if (syncingFromProps) return
    emit('update:modelValue', {
      resource_type: value.resource_type,
      name: value.name,
      description: value.description,
      is_active: value.is_active,
      connection_info: { ...value.connection_info }
    })
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
      if (formState.connection_info.password !== '') {
        formState.connection_info.password = ''
      }
    }
  }
)

watch(
  () => formState.connection_info.secret_key,
  (value) => {
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
      if (formState.connection_info.secret_key !== '') {
        formState.connection_info.secret_key = ''
      }
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
