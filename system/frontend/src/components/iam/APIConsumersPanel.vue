<template>
  <div class="iam-panel api-consumers-panel">
    <el-alert
      :title="t('system.apiConsumer.boundaryTitle')"
      :description="t('system.apiConsumer.boundaryDescription')"
      type="info"
      :closable="false"
      show-icon
      class="boundary-alert"
    />
    <el-card>
      <template #header>
        <div class="card-header">
          <span>{{ t('system.apiConsumer.title') }}</span>
          <el-button type="primary" @click="openCreateDialog">
            <el-icon><Plus /></el-icon>
            {{ t('system.apiConsumer.create') }}
          </el-button>
        </div>
      </template>

      <el-table :data="consumers" v-loading="loading" stripe>
        <el-table-column prop="id" :label="t('system.apiConsumer.columns.id')" width="80" />
        <el-table-column prop="name" :label="t('system.apiConsumer.columns.name')" min-width="150" />
        <el-table-column prop="description" :label="t('system.apiConsumer.columns.description')" min-width="200" show-overflow-tooltip />
        <el-table-column :label="t('system.apiConsumer.columns.allowedServices')" min-width="200">
          <template #default="{ row }">
            <el-tag
              v-for="grant in row.service_grants"
              :key="serviceReferenceKey(grant)"
              size="small"
              style="margin-right: 5px"
            >
              {{ serviceReferenceLabel(grant) }}
            </el-tag>
            <span v-if="!row.service_grants || row.service_grants.length === 0" class="text-secondary">
              {{ t('system.apiConsumer.noServices') }}
            </span>
          </template>
        </el-table-column>
        <el-table-column :label="t('system.apiConsumer.columns.rateLimit')" width="120">
          <template #default="{ row }">
            {{ t('system.apiConsumer.ratePerMin', { rate: row.rate_limit_per_minute }) }}
          </template>
        </el-table-column>
        <el-table-column :label="t('system.apiConsumer.columns.status')" width="100">
          <template #default="{ row }">
            <el-tag :type="row.status === 'active' ? 'success' : 'danger'">
              {{ row.status === 'active' ? t('system.apiConsumer.status.active') : t('system.apiConsumer.status.suspended') }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('system.apiConsumer.columns.createdAt')" width="180">
          <template #default="{ row }">
            {{ formatDate(row.created_at) }}
          </template>
        </el-table-column>
        <el-table-column :label="t('system.apiConsumer.columns.actions')" width="300" fixed="right">
          <template #default="{ row }">
            <el-button size="small" type="primary" @click="openCredentialsDialog(row)">
              <el-icon><Key /></el-icon>
              {{ t('system.apiConsumer.actions.credentials') }}
            </el-button>
            <el-button size="small" @click="openEditDialog(row)">
              <el-icon><Edit /></el-icon>
              {{ t('system.apiConsumer.actions.edit') }}
            </el-button>
            <el-button size="small" type="danger" @click="handleDelete(row)">
              <el-icon><Delete /></el-icon>
              {{ t('system.apiConsumer.actions.delete') }}
            </el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <!-- 创建/编辑 API 消费方 -->
    <el-dialog
      v-model="dialogVisible"
      :title="isEdit ? t('system.apiConsumer.dialog.edit') : t('system.apiConsumer.dialog.create')"
      width="600px"
    >
      <el-form :model="formData" :rules="rules" ref="formRef" label-width="120px">
        <el-form-item :label="t('system.apiConsumer.form.name')" prop="name">
          <el-input v-model="formData.name" :placeholder="t('system.apiConsumer.form.namePlaceholder')" />
        </el-form-item>
        <el-form-item :label="t('system.apiConsumer.form.description')" prop="description">
          <el-input
            v-model="formData.description"
            type="textarea"
            :rows="3"
            :placeholder="t('system.apiConsumer.form.descPlaceholder')"
          />
        </el-form-item>
        <el-form-item :label="t('system.apiConsumer.form.allowedServices')" prop="service_grant_keys">
          <el-select
            v-model="formData.service_grant_keys"
            multiple
            filterable
            :loading="servicesLoading"
            :placeholder="t('system.apiConsumer.form.allowedServicesPlaceholder')"
            style="width: 100%"
          >
            <el-option
              v-for="service in serviceOptions"
              :key="serviceReferenceKey(service.ref)"
              :label="service.title"
              :value="serviceReferenceKey(service.ref)"
            >
              <div class="service-option">
                <span>{{ service.title }}</span>
                <small>{{ service.access_mode === 'public' ? t('system.apiConsumer.services.public') : t('system.apiConsumer.services.private') }}</small>
              </div>
            </el-option>
          </el-select>
        </el-form-item>
        <el-form-item :label="t('system.apiConsumer.form.rateLimit')" prop="rate_limit_per_minute">
          <el-input-number
            v-model="formData.rate_limit_per_minute"
            :min="10"
            :max="10000"
            :step="10"
            style="width: 100%"
          />
          <div class="form-tip">{{ t('system.apiConsumer.form.rateLimitTip') }}</div>
        </el-form-item>
        <el-form-item :label="t('system.apiConsumer.form.status')" prop="status">
          <el-radio-group v-model="formData.status">
            <el-radio value="active">{{ t('system.apiConsumer.form.active') }}</el-radio>
            <el-radio value="suspended">{{ t('system.apiConsumer.form.suspended') }}</el-radio>
          </el-radio-group>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">{{ t('system.apiConsumer.dialog.cancel') }}</el-button>
        <el-button type="primary" @click="handleSubmit">{{ t('system.apiConsumer.dialog.confirm') }}</el-button>
      </template>
    </el-dialog>

    <!-- API 消费凭据 管理对话框 -->
    <el-dialog
      v-model="credentialsDialogVisible"
      :title="t('system.apiConsumer.credentials.title', { name: currentConsumer?.name })"
      width="800px"
    >
      <div style="margin-bottom: 16px">
        <el-button type="primary" @click="openGenerateCredentialDialog">
          <el-icon><Plus /></el-icon>
          {{ t('system.apiConsumer.credentials.generate') }}
        </el-button>
      </div>

      <el-table :data="credentials" v-loading="credentialsLoading" stripe>
        <el-table-column prop="id" :label="t('system.apiConsumer.credentials.columns.id')" width="80" />
        <el-table-column :label="t('system.apiConsumer.credentials.columns.prefix')" width="150">
          <template #default="{ row }">
            <code>{{ row.key_prefix }}***</code>
          </template>
        </el-table-column>
        <el-table-column prop="name" :label="t('system.apiConsumer.credentials.columns.name')" min-width="120" />
        <el-table-column :label="t('system.apiConsumer.credentials.columns.lastUsed')" width="180">
          <template #default="{ row }">
            <span v-if="row.last_used_at">{{ formatDate(row.last_used_at) }}</span>
            <span v-else class="text-secondary">{{ t('system.apiConsumer.credentials.neverUsed') }}</span>
          </template>
        </el-table-column>
        <el-table-column :label="t('system.apiConsumer.credentials.columns.status')" width="100">
          <template #default="{ row }">
            <el-tag :type="row.status === 'active' ? 'success' : 'danger'">
              {{ row.status === 'active' ? t('system.apiConsumer.credentials.status.active') : t('system.apiConsumer.credentials.status.revoked') }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('system.apiConsumer.credentials.columns.createdAt')" width="180">
          <template #default="{ row }">
            {{ formatDate(row.created_at) }}
          </template>
        </el-table-column>
        <el-table-column :label="t('system.apiConsumer.credentials.columns.actions')" width="100" fixed="right">
          <template #default="{ row }">
            <el-button
              v-if="row.status === 'active'"
              size="small"
              type="danger"
              @click="handleRevokeCredential(row)"
            >
              {{ t('system.apiConsumer.credentials.revoke') }}
            </el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-dialog>

    <!-- 生成 API 消费凭据 对话框 -->
    <el-dialog
      v-model="generateCredentialDialogVisible"
      :title="t('system.apiConsumer.credentials.generateDialog.title')"
      width="500px"
    >
      <el-form :model="credentialFormData" :rules="credentialRules" ref="credentialFormRef" label-width="100px">
        <el-form-item :label="t('system.apiConsumer.credentials.generateDialog.name')" prop="name">
          <el-input v-model="credentialFormData.name" :placeholder="t('system.apiConsumer.credentials.generateDialog.namePlaceholder')" />
        </el-form-item>
        <el-form-item :label="t('system.apiConsumer.credentials.generateDialog.expiresAt')">
          <el-date-picker
            v-model="credentialFormData.expires_at"
            type="datetime"
            :placeholder="t('system.apiConsumer.credentials.generateDialog.expiresAtPlaceholder')"
            style="width: 100%"
          />
          <div class="form-tip">{{ t('system.apiConsumer.credentials.generateDialog.expiresAtTip') }}</div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="generateCredentialDialogVisible = false">{{ t('system.apiConsumer.credentials.generateDialog.cancel') }}</el-button>
        <el-button type="primary" @click="handleGenerateCredential">{{ t('system.apiConsumer.credentials.generateDialog.generate') }}</el-button>
      </template>
    </el-dialog>

    <!-- 显示生成的 API 消费凭据 -->
    <el-dialog
      v-model="showCredentialDialogVisible"
      :title="t('system.apiConsumer.credentials.showDialog.title')"
      width="600px"
      :close-on-click-modal="false"
      :close-on-press-escape="false"
    >
      <el-alert
        :title="t('system.apiConsumer.credentials.showDialog.warning')"
        type="warning"
        :closable="false"
        style="margin-bottom: 16px"
      >
        {{ t('system.apiConsumer.credentials.showDialog.warningMsg') }}
      </el-alert>

      <div class="api-credential-display">
        <el-input
          v-model="generatedCredential"
          readonly
          ref="credentialInput"
        >
          <template #append>
            <el-button @click="copyCredential">
              <el-icon><DocumentCopy /></el-icon>
              {{ t('system.apiConsumer.credentials.showDialog.copy') }}
            </el-button>
          </template>
        </el-input>
      </div>

      <template #footer>
        <el-button type="primary" @click="showCredentialDialogVisible = false">
          {{ t('system.apiConsumer.credentials.showDialog.close') }}
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Edit, Delete, Key, DocumentCopy } from '@element-plus/icons-vue'
import { apiConsumersAPI } from '@/api/apiConsumers'

const { t } = useI18n()

const loading = ref(false)
const consumers = ref([])
const serviceOptions = ref([])
const servicesLoading = ref(false)
const dialogVisible = ref(false)
const isEdit = ref(false)
const formRef = ref(null)
const formData = ref({
  name: '',
  description: '',
  service_grant_keys: [],
  rate_limit_per_minute: 60,
  status: 'active'
})

const rules = {
  name: [{ required: true, message: () => t('system.apiConsumer.rules.nameRequired'), trigger: 'blur' }],
  service_grant_keys: [{ required: true, type: 'array', min: 1, message: () => t('system.apiConsumer.rules.serviceRequired'), trigger: 'change' }],
  rate_limit_per_minute: [{ required: true, message: () => t('system.apiConsumer.rules.rateLimitRequired'), trigger: 'blur' }]
}

const serviceReferenceKey = (reference) => `${reference?.service_type || ''}:${reference?.service_id || ''}`

const serviceReferenceLabel = (reference) => {
  const key = serviceReferenceKey(reference)
  return serviceOptions.value.find(service => serviceReferenceKey(service.ref) === key)?.title ||
    t('system.apiConsumer.services.fallback', { id: reference?.service_id || '-' })
}

const apiConsumerPayload = (form) => ({
  name: form.name,
  description: form.description,
  rate_limit_per_minute: form.rate_limit_per_minute,
  status: form.status,
  service_grants: form.service_grant_keys.map(key => {
    const [serviceType, serviceID] = key.split(':')
    return { service_type: serviceType, service_id: Number(serviceID) }
  })
})

const loadConsumableServices = async () => {
  servicesLoading.value = true
  try {
    const response = await apiConsumersAPI.listConsumableServices()
    serviceOptions.value = response.data || []
  } catch (error) {
    ElMessage.error(t('system.apiConsumer.msg.servicesLoadFailed') + ': ' + (error.response?.data?.error || error.message))
  } finally {
    servicesLoading.value = false
  }
}

// API 消费凭据 管理
const credentialsDialogVisible = ref(false)
const currentConsumer = ref(null)
const credentials = ref([])
const credentialsLoading = ref(false)

// 生成 Key
const generateCredentialDialogVisible = ref(false)
const credentialFormRef = ref(null)
const credentialFormData = ref({
  name: '',
  expires_at: null
})
const credentialRules = {
  name: [{ required: true, message: () => t('system.apiConsumer.credentials.generateDialog.rules.nameRequired'), trigger: 'blur' }]
}

// 显示生成的 Key
const showCredentialDialogVisible = ref(false)
const generatedCredential = ref('')
const credentialInput = ref(null)

// 加载 API 消费方列表
const loadConsumers = async () => {
  loading.value = true
  try {
    const response = await apiConsumersAPI.list()
    consumers.value = response.data || []
  } catch (error) {
    ElMessage.error(t('system.apiConsumer.msg.loadFailed') + ': ' + (error.response?.data?.error || error.message))
  } finally {
    loading.value = false
  }
}

// 打开创建对话框
const openCreateDialog = () => {
  isEdit.value = false
  formData.value = {
    name: '',
    description: '',
    service_grant_keys: [],
    rate_limit_per_minute: 60,
    status: 'active'
  }
  dialogVisible.value = true
}

// 打开编辑对话框
const openEditDialog = (consumer) => {
  isEdit.value = true
  formData.value = {
    id: consumer.id,
    name: consumer.name,
    description: consumer.description,
    service_grant_keys: (consumer.service_grants || []).map(serviceReferenceKey),
    rate_limit_per_minute: consumer.rate_limit_per_minute,
    status: consumer.status
  }
  dialogVisible.value = true
}

// 提交表单
const handleSubmit = async () => {
  if (!formRef.value) return
  await formRef.value.validate(async (valid) => {
    if (valid) {
      try {
        if (isEdit.value) {
          await apiConsumersAPI.update(formData.value.id, apiConsumerPayload(formData.value))
          ElMessage.success(t('system.apiConsumer.msg.updateSuccess'))
        } else {
          await apiConsumersAPI.create(apiConsumerPayload(formData.value))
          ElMessage.success(t('system.apiConsumer.msg.createSuccess'))
        }
        dialogVisible.value = false
        loadConsumers()
      } catch (error) {
        ElMessage.error(t('system.apiConsumer.msg.opFailed') + ': ' + (error.response?.data?.error || error.message))
      }
    }
  })
}

// 删除 API 消费方
const handleDelete = (consumer) => {
  ElMessageBox.confirm(
    t('system.apiConsumer.msg.deleteConfirm', { name: consumer.name }),
    t('system.apiConsumer.msg.deleteTitle'),
    {
      confirmButtonText: t('system.apiConsumer.dialog.confirm'),
      cancelButtonText: t('system.apiConsumer.dialog.cancel'),
      type: 'warning'
    }
  ).then(async () => {
    try {
      await apiConsumersAPI.delete(consumer.id)
      ElMessage.success(t('system.apiConsumer.msg.deleteSuccess'))
      loadConsumers()
    } catch (error) {
      ElMessage.error(t('system.apiConsumer.msg.deleteFailed') + ': ' + (error.response?.data?.error || error.message))
    }
  })
}

// 打开凭据管理对话框
const openCredentialsDialog = async (consumer) => {
  currentConsumer.value = consumer
  credentialsDialogVisible.value = true
  await loadCredentials(consumer.id)
}

// 加载 API 消费凭据
const loadCredentials = async (consumerId) => {
  credentialsLoading.value = true
  try {
    const response = await apiConsumersAPI.listCredentials(consumerId)
    credentials.value = response.data || []
  } catch (error) {
    ElMessage.error(t('system.apiConsumer.msg.keysLoadFailed') + ': ' + (error.response?.data?.error || error.message))
  } finally {
    credentialsLoading.value = false
  }
}

// 打开生成凭据对话框
const openGenerateCredentialDialog = () => {
  credentialFormData.value = {
    name: '',
    expires_at: null
  }
  generateCredentialDialogVisible.value = true
}

// 生成 API 消费凭据
const handleGenerateCredential = async () => {
  if (!credentialFormRef.value) return
  await credentialFormRef.value.validate(async (valid) => {
    if (valid) {
      try {
        const response = await apiConsumersAPI.createCredential(currentConsumer.value.id, credentialFormData.value)
        generatedCredential.value = response.plain_text_credential
        generateCredentialDialogVisible.value = false
        showCredentialDialogVisible.value = true
        await loadCredentials(currentConsumer.value.id)
      } catch (error) {
        ElMessage.error(t('system.apiConsumer.msg.keyGenFailed') + ': ' + (error.response?.data?.error || error.message))
      }
    }
  })
}

// 复制 API 消费凭据
const copyCredential = async () => {
  try {
    // 优先使用 Clipboard API
    if (navigator.clipboard && navigator.clipboard.writeText) {
      await navigator.clipboard.writeText(generatedCredential.value)
      ElMessage.success(t('system.apiConsumer.msg.keyCopied'))
    } else {
      // 回退方案：使用传统的选择和复制方法
      const input = credentialInput.value?.input || credentialInput.value?.$el?.querySelector('input')
      if (input) {
        input.select()
        input.setSelectionRange(0, input.value.length)
        const successful = document.execCommand('copy')
        if (successful) {
          ElMessage.success(t('system.apiConsumer.msg.keyCopied'))
        } else {
          ElMessage.error(t('system.apiConsumer.msg.keyCopyFailed'))
        }
      } else {
        ElMessage.error(t('system.apiConsumer.msg.keyCopyFailed'))
      }
    }
  } catch (error) {
    console.error('复制失败:', error)
    // 最后的回退：尝试传统方法
    try {
      const input = credentialInput.value?.input || credentialInput.value?.$el?.querySelector('input')
      if (input) {
        input.select()
        document.execCommand('copy')
        ElMessage.success(t('system.apiConsumer.msg.keyCopied'))
      } else {
        ElMessage.error(t('system.apiConsumer.msg.keyCopyFailed'))
      }
    } catch (e) {
      ElMessage.error(t('system.apiConsumer.msg.keyCopyFailed'))
    }
  }
}

// 撤销 API 消费凭据
const handleRevokeCredential = (credential) => {
  ElMessageBox.confirm(
    t('system.apiConsumer.msg.revokeConfirm', { name: credential.name }),
    t('system.apiConsumer.msg.revokeTitle'),
    {
      confirmButtonText: t('system.apiConsumer.dialog.confirm'),
      cancelButtonText: t('system.apiConsumer.dialog.cancel'),
      type: 'warning'
    }
  ).then(async () => {
    try {
      await apiConsumersAPI.revokeCredential(currentConsumer.value.id, credential.id)
      ElMessage.success(t('system.apiConsumer.msg.keyRevoked'))
      await loadCredentials(currentConsumer.value.id)
    } catch (error) {
      ElMessage.error(t('system.apiConsumer.msg.keyRevokeFailed') + ': ' + (error.response?.data?.error || error.message))
    }
  })
}

// 格式化日期
const formatDate = (dateString) => {
  if (!dateString) return '-'
  const date = new Date(dateString)
  return date.toLocaleString('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit'
  })
}

onMounted(() => {
  loadConsumableServices()
  loadConsumers()
})
</script>

<style scoped>
.api-consumers-panel { padding-top: 6px; }
.boundary-alert { margin-bottom: 14px; }

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.text-secondary {
  color: var(--addp-text-tertiary);
  font-size: 13px;
}

.form-tip {
  font-size: 12px;
  color: var(--addp-text-tertiary);
  margin-top: 4px;
}

.api-credential-display {
  padding: 16px;
  background: var(--addp-bg-secondary);
  border-radius: 4px;
}

.api-credential-display .el-input {
  font-family: 'Courier New', monospace;
  font-size: 14px;
}

.service-option { display: flex; align-items: center; justify-content: space-between; gap: 16px; }
.service-option small { color: var(--addp-text-tertiary); }
</style>
