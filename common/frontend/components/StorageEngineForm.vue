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
        <el-input v-model="formState.connection_info.endpoint" placeholder="localhost:9000" />
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
      <el-form-item label="Bucket">
        <el-input v-model="formState.connection_info.bucket" placeholder="存储桶名称（可选）" />
      </el-form-item>
      <el-form-item label="使用 SSL">
        <el-switch v-model="formState.connection_info.use_ssl" />
      </el-form-item>
    </template>

    <el-form-item v-if="showActiveSwitch" label="激活状态">
      <el-switch v-model="formState.is_active" />
    </el-form-item>
  </el-form>
</template>

<script setup>
import { computed, reactive, ref, watch } from 'vue'

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
      { label: 'MinIO', value: 'minio' }
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

const ensureConnectionDefaults = (form) => {
  if (!form.connection_info || typeof form.connection_info !== 'object') {
    form.connection_info = {}
  }

  if (form.resource_type === 'postgresql') {
    form.connection_info = {
      host: form.connection_info.host ?? 'localhost',
      port: form.connection_info.port ?? 5432,
      database: form.connection_info.database ?? '',
      user: form.connection_info.user ?? '',
      password: form.connection_info.password ?? '',
      sslmode: form.connection_info.sslmode ?? 'disable'
    }
  } else if (form.resource_type === 'minio' || form.resource_type === 's3') {
    form.connection_info = {
      endpoint: form.connection_info.endpoint ?? 'localhost:9000',
      access_key: form.connection_info.access_key ?? '',
      secret_key: form.connection_info.secret_key ?? '',
      bucket: form.connection_info.bucket ?? '',
      use_ssl: form.connection_info.use_ssl ?? false
    }
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
}

watch(
  () => props.modelValue,
  (value) => {
    syncFromProps(value || {})
  },
  { immediate: true, deep: true }
)

watch(
  formState,
  (value) => {
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
  'connection_info.secret_key': [{ required: true, message: '请输入 Secret Key', trigger: 'blur' }]
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

  return {
    resource_type: rules.resource_type,
    name: rules.name
  }
})

const handleTypeChange = (type) => {
  ensureConnectionDefaults(formState)
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
</script>
