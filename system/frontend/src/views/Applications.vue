<template>
  <div class="page-container">
    <el-card>
      <template #header>
        <div class="card-header">
          <span>{{ t('system.app.title') }}</span>
          <el-button type="primary" @click="openCreateDialog">
            <el-icon><Plus /></el-icon>
            {{ t('system.app.create') }}
          </el-button>
        </div>
      </template>

      <el-table :data="applications" v-loading="loading" stripe>
        <el-table-column prop="id" :label="t('system.app.columns.id')" width="80" />
        <el-table-column prop="name" :label="t('system.app.columns.name')" min-width="150" />
        <el-table-column prop="description" :label="t('system.app.columns.description')" min-width="200" show-overflow-tooltip />
        <el-table-column :label="t('system.app.columns.allowedServices')" min-width="200">
          <template #default="{ row }">
            <el-tag
              v-for="service in row.allowed_services"
              :key="service"
              size="small"
              style="margin-right: 5px"
            >
              {{ service }}
            </el-tag>
            <span v-if="!row.allowed_services || row.allowed_services.length === 0" class="text-secondary">
              {{ t('system.app.allServices') }}
            </span>
          </template>
        </el-table-column>
        <el-table-column :label="t('system.app.columns.rateLimit')" width="120">
          <template #default="{ row }">
            {{ t('system.app.ratePerMin', { rate: row.rate_limit_per_minute }) }}
          </template>
        </el-table-column>
        <el-table-column :label="t('system.app.columns.status')" width="100">
          <template #default="{ row }">
            <el-tag :type="row.status === 'active' ? 'success' : 'danger'">
              {{ row.status === 'active' ? t('system.app.status.active') : t('system.app.status.suspended') }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('system.app.columns.createdAt')" width="180">
          <template #default="{ row }">
            {{ formatDate(row.created_at) }}
          </template>
        </el-table-column>
        <el-table-column :label="t('system.app.columns.actions')" width="300" fixed="right">
          <template #default="{ row }">
            <el-button size="small" type="primary" @click="openKeysDialog(row)">
              <el-icon><Key /></el-icon>
              {{ t('system.app.actions.apiKeys') }}
            </el-button>
            <el-button size="small" @click="openEditDialog(row)">
              <el-icon><Edit /></el-icon>
              {{ t('system.app.actions.edit') }}
            </el-button>
            <el-button size="small" type="danger" @click="handleDelete(row)">
              <el-icon><Delete /></el-icon>
              {{ t('system.app.actions.delete') }}
            </el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <!-- 创建/编辑应用对话框 -->
    <el-dialog
      v-model="dialogVisible"
      :title="isEdit ? t('system.app.dialog.edit') : t('system.app.dialog.create')"
      width="600px"
    >
      <el-form :model="formData" :rules="rules" ref="formRef" label-width="120px">
        <el-form-item :label="t('system.app.form.name')" prop="name">
          <el-input v-model="formData.name" :placeholder="t('system.app.form.namePlaceholder')" />
        </el-form-item>
        <el-form-item :label="t('system.app.form.description')" prop="description">
          <el-input
            v-model="formData.description"
            type="textarea"
            :rows="3"
            :placeholder="t('system.app.form.descPlaceholder')"
          />
        </el-form-item>
        <el-form-item :label="t('system.app.form.allowedServices')">
          <el-select
            v-model="formData.allowed_services"
            multiple
            :placeholder="t('system.app.form.allowedServicesPlaceholder')"
            style="width: 100%"
          >
            <el-option :label="t('system.app.services.poi')" value="poi_service" />
            <el-option :label="t('system.app.services.address')" value="address_service" />
            <el-option :label="t('system.app.services.map')" value="map_service" />
            <el-option :label="t('system.app.services.data')" value="data_service" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('system.app.form.rateLimit')" prop="rate_limit_per_minute">
          <el-input-number
            v-model="formData.rate_limit_per_minute"
            :min="10"
            :max="10000"
            :step="10"
            style="width: 100%"
          />
          <div class="form-tip">{{ t('system.app.form.rateLimitTip') }}</div>
        </el-form-item>
        <el-form-item :label="t('system.app.form.status')" prop="status">
          <el-radio-group v-model="formData.status">
            <el-radio value="active">{{ t('system.app.form.active') }}</el-radio>
            <el-radio value="suspended">{{ t('system.app.form.suspended') }}</el-radio>
          </el-radio-group>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">{{ t('system.app.dialog.cancel') }}</el-button>
        <el-button type="primary" @click="handleSubmit">{{ t('system.app.dialog.confirm') }}</el-button>
      </template>
    </el-dialog>

    <!-- API Keys 管理对话框 -->
    <el-dialog
      v-model="keysDialogVisible"
      :title="t('system.app.keys.title', { name: currentApp?.name })"
      width="800px"
    >
      <div style="margin-bottom: 16px">
        <el-button type="primary" @click="openGenerateKeyDialog">
          <el-icon><Plus /></el-icon>
          {{ t('system.app.keys.generate') }}
        </el-button>
      </div>

      <el-table :data="apiKeys" v-loading="keysLoading" stripe>
        <el-table-column prop="id" :label="t('system.app.keys.columns.id')" width="80" />
        <el-table-column :label="t('system.app.keys.columns.prefix')" width="150">
          <template #default="{ row }">
            <code>{{ row.key_prefix }}***</code>
          </template>
        </el-table-column>
        <el-table-column prop="name" :label="t('system.app.keys.columns.name')" min-width="120" />
        <el-table-column :label="t('system.app.keys.columns.lastUsed')" width="180">
          <template #default="{ row }">
            <span v-if="row.last_used_at">{{ formatDate(row.last_used_at) }}</span>
            <span v-else class="text-secondary">{{ t('system.app.keys.neverUsed') }}</span>
          </template>
        </el-table-column>
        <el-table-column :label="t('system.app.keys.columns.status')" width="100">
          <template #default="{ row }">
            <el-tag :type="row.status === 'active' ? 'success' : 'danger'">
              {{ row.status === 'active' ? t('system.app.keys.status.active') : t('system.app.keys.status.revoked') }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('system.app.keys.columns.createdAt')" width="180">
          <template #default="{ row }">
            {{ formatDate(row.created_at) }}
          </template>
        </el-table-column>
        <el-table-column :label="t('system.app.keys.columns.actions')" width="100" fixed="right">
          <template #default="{ row }">
            <el-button
              v-if="row.status === 'active'"
              size="small"
              type="danger"
              @click="handleRevokeKey(row)"
            >
              {{ t('system.app.keys.revoke') }}
            </el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-dialog>

    <!-- 生成 API Key 对话框 -->
    <el-dialog
      v-model="generateKeyDialogVisible"
      :title="t('system.app.keys.generateDialog.title')"
      width="500px"
    >
      <el-form :model="keyFormData" :rules="keyRules" ref="keyFormRef" label-width="100px">
        <el-form-item :label="t('system.app.keys.generateDialog.name')" prop="name">
          <el-input v-model="keyFormData.name" :placeholder="t('system.app.keys.generateDialog.namePlaceholder')" />
        </el-form-item>
        <el-form-item :label="t('system.app.keys.generateDialog.expiresAt')">
          <el-date-picker
            v-model="keyFormData.expires_at"
            type="datetime"
            :placeholder="t('system.app.keys.generateDialog.expiresAtPlaceholder')"
            style="width: 100%"
          />
          <div class="form-tip">{{ t('system.app.keys.generateDialog.expiresAtTip') }}</div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="generateKeyDialogVisible = false">{{ t('system.app.keys.generateDialog.cancel') }}</el-button>
        <el-button type="primary" @click="handleGenerateKey">{{ t('system.app.keys.generateDialog.generate') }}</el-button>
      </template>
    </el-dialog>

    <!-- 显示生成的 API Key -->
    <el-dialog
      v-model="showKeyDialogVisible"
      :title="t('system.app.keys.showDialog.title')"
      width="600px"
      :close-on-click-modal="false"
      :close-on-press-escape="false"
    >
      <el-alert
        :title="t('system.app.keys.showDialog.warning')"
        type="warning"
        :closable="false"
        style="margin-bottom: 16px"
      >
        {{ t('system.app.keys.showDialog.warningMsg') }}
      </el-alert>

      <div class="api-key-display">
        <el-input
          v-model="generatedApiKey"
          readonly
          ref="apiKeyInput"
        >
          <template #append>
            <el-button @click="copyApiKey">
              <el-icon><DocumentCopy /></el-icon>
              {{ t('system.app.keys.showDialog.copy') }}
            </el-button>
          </template>
        </el-input>
      </div>

      <template #footer>
        <el-button type="primary" @click="showKeyDialogVisible = false">
          {{ t('system.app.keys.showDialog.close') }}
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
import { applicationsAPI } from '@/api/applications'

const { t } = useI18n()

const loading = ref(false)
const applications = ref([])
const dialogVisible = ref(false)
const isEdit = ref(false)
const formRef = ref(null)
const formData = ref({
  name: '',
  description: '',
  allowed_services: [],
  rate_limit_per_minute: 60,
  status: 'active'
})

const rules = {
  name: [{ required: true, message: () => t('system.app.rules.nameRequired'), trigger: 'blur' }],
  rate_limit_per_minute: [{ required: true, message: () => t('system.app.rules.rateLimitRequired'), trigger: 'blur' }]
}

// API Keys 管理
const keysDialogVisible = ref(false)
const currentApp = ref(null)
const apiKeys = ref([])
const keysLoading = ref(false)

// 生成 Key
const generateKeyDialogVisible = ref(false)
const keyFormRef = ref(null)
const keyFormData = ref({
  name: '',
  expires_at: null
})
const keyRules = {
  name: [{ required: true, message: () => t('system.app.keys.generateDialog.rules.nameRequired'), trigger: 'blur' }]
}

// 显示生成的 Key
const showKeyDialogVisible = ref(false)
const generatedApiKey = ref('')
const apiKeyInput = ref(null)

// 加载应用列表
const loadApplications = async () => {
  loading.value = true
  try {
    const response = await applicationsAPI.list()
    applications.value = response.applications || []
  } catch (error) {
    ElMessage.error(t('system.app.msg.loadFailed') + ': ' + (error.response?.data?.error || error.message))
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
    allowed_services: [],
    rate_limit_per_minute: 60,
    status: 'active'
  }
  dialogVisible.value = true
}

// 打开编辑对话框
const openEditDialog = (app) => {
  isEdit.value = true
  formData.value = {
    id: app.id,
    name: app.name,
    description: app.description,
    allowed_services: app.allowed_services || [],
    rate_limit_per_minute: app.rate_limit_per_minute,
    status: app.status
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
          await applicationsAPI.update(formData.value.id, formData.value)
          ElMessage.success(t('system.app.msg.updateSuccess'))
        } else {
          await applicationsAPI.create(formData.value)
          ElMessage.success(t('system.app.msg.createSuccess'))
        }
        dialogVisible.value = false
        loadApplications()
      } catch (error) {
        ElMessage.error(t('system.app.msg.opFailed') + ': ' + (error.response?.data?.error || error.message))
      }
    }
  })
}

// 删除应用
const handleDelete = (app) => {
  ElMessageBox.confirm(
    t('system.app.msg.deleteConfirm', { name: app.name }),
    t('system.app.msg.deleteTitle'),
    {
      confirmButtonText: t('system.app.dialog.confirm'),
      cancelButtonText: t('system.app.dialog.cancel'),
      type: 'warning'
    }
  ).then(async () => {
    try {
      await applicationsAPI.delete(app.id)
      ElMessage.success(t('system.app.msg.deleteSuccess'))
      loadApplications()
    } catch (error) {
      ElMessage.error(t('system.app.msg.deleteFailed') + ': ' + (error.response?.data?.error || error.message))
    }
  })
}

// 打开 Keys 管理对话框
const openKeysDialog = async (app) => {
  currentApp.value = app
  keysDialogVisible.value = true
  await loadApiKeys(app.id)
}

// 加载 API Keys
const loadApiKeys = async (appId) => {
  keysLoading.value = true
  try {
    const response = await applicationsAPI.listKeys(appId)
    apiKeys.value = response.keys || []
  } catch (error) {
    ElMessage.error(t('system.app.msg.keysLoadFailed') + ': ' + (error.response?.data?.error || error.message))
  } finally {
    keysLoading.value = false
  }
}

// 打开生成 Key 对话框
const openGenerateKeyDialog = () => {
  keyFormData.value = {
    name: '',
    expires_at: null
  }
  generateKeyDialogVisible.value = true
}

// 生成 API Key
const handleGenerateKey = async () => {
  if (!keyFormRef.value) return
  await keyFormRef.value.validate(async (valid) => {
    if (valid) {
      try {
        const response = await applicationsAPI.generateKey(currentApp.value.id, keyFormData.value)
        generatedApiKey.value = response.plain_text_key
        generateKeyDialogVisible.value = false
        showKeyDialogVisible.value = true
        await loadApiKeys(currentApp.value.id)
      } catch (error) {
        ElMessage.error(t('system.app.msg.keyGenFailed') + ': ' + (error.response?.data?.error || error.message))
      }
    }
  })
}

// 复制 API Key
const copyApiKey = async () => {
  try {
    // 优先使用 Clipboard API
    if (navigator.clipboard && navigator.clipboard.writeText) {
      await navigator.clipboard.writeText(generatedApiKey.value)
      ElMessage.success(t('system.app.msg.keyCopied'))
    } else {
      // 回退方案：使用传统的选择和复制方法
      const input = apiKeyInput.value?.input || apiKeyInput.value?.$el?.querySelector('input')
      if (input) {
        input.select()
        input.setSelectionRange(0, input.value.length)
        const successful = document.execCommand('copy')
        if (successful) {
          ElMessage.success(t('system.app.msg.keyCopied'))
        } else {
          ElMessage.error(t('system.app.msg.keyCopyFailed'))
        }
      } else {
        ElMessage.error(t('system.app.msg.keyCopyFailed'))
      }
    }
  } catch (error) {
    console.error('复制失败:', error)
    // 最后的回退：尝试传统方法
    try {
      const input = apiKeyInput.value?.input || apiKeyInput.value?.$el?.querySelector('input')
      if (input) {
        input.select()
        document.execCommand('copy')
        ElMessage.success(t('system.app.msg.keyCopied'))
      } else {
        ElMessage.error(t('system.app.msg.keyCopyFailed'))
      }
    } catch (e) {
      ElMessage.error(t('system.app.msg.keyCopyFailed'))
    }
  }
}

// 撤销 API Key
const handleRevokeKey = (key) => {
  ElMessageBox.confirm(
    t('system.app.msg.revokeConfirm', { name: key.name }),
    t('system.app.msg.revokeTitle'),
    {
      confirmButtonText: t('system.app.dialog.confirm'),
      cancelButtonText: t('system.app.dialog.cancel'),
      type: 'warning'
    }
  ).then(async () => {
    try {
      await applicationsAPI.revokeKey(currentApp.value.id, key.id)
      ElMessage.success(t('system.app.msg.keyRevoked'))
      await loadApiKeys(currentApp.value.id)
    } catch (error) {
      ElMessage.error(t('system.app.msg.keyRevokeFailed') + ': ' + (error.response?.data?.error || error.message))
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
  loadApplications()
})
</script>

<style scoped>
.page-container {
  padding: 20px;
}

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

.api-key-display {
  padding: 16px;
  background: var(--addp-bg-secondary);
  border-radius: 4px;
}

.api-key-display .el-input {
  font-family: 'Courier New', monospace;
  font-size: 14px;
}
</style>
