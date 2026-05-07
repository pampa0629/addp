<template>
  <el-card>
    <template #header>
      <div class="card-header">
        <span>{{ t('transfer.localEngines.title') }}</span>
        <div class="header-actions">
          <el-button type="info" :icon="RefreshRight" @click="loadResources">{{ t('transfer.localEngines.refresh') }}</el-button>
          <el-button type="primary" :icon="Plus" @click="showAddDialog">{{ t('transfer.localEngines.addEngine') }}</el-button>
        </div>
      </div>
    </template>

    <el-alert
      :title="t('transfer.localEngines.infoTitle')"
      type="info"
      :closable="false"
      style="margin-bottom: 16px"
    >
      <p>{{ t('transfer.localEngines.infoDesc1') }}</p>
      <p>{{ t('transfer.localEngines.infoDesc2') }}</p>
    </el-alert>

    <el-table :data="engines" v-loading="loading" stripe>
      <el-table-column prop="id" label="ID" width="80" />
      <el-table-column prop="name" :label="t('transfer.localEngines.name')" min-width="150" />
      <el-table-column prop="engine_type" :label="t('transfer.localEngines.type')" width="150">
        <template #default="{ row }">
          <el-tag :type="getResourceTypeColor(row.engine_type)">
            {{ getResourceTypeLabel(row.engine_type) }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="description" :label="t('transfer.localEngines.description')" min-width="200" show-overflow-tooltip />
      <el-table-column :label="t('transfer.localEngines.statusLabel')" width="100">
        <template #default="{ row }">
          <el-tag :type="row.is_active ? 'success' : 'danger'">
            {{ row.is_active ? t('transfer.localEngines.active') : t('transfer.localEngines.disabled') }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column :label="t('transfer.localEngines.createdAt')" width="180">
        <template #default="{ row }">
          {{ formatDate(row.created_at) }}
        </template>
      </el-table-column>
      <el-table-column :label="t('transfer.localEngines.actions')" width="420" fixed="right">
        <template #default="{ row }">
          <el-button size="small" type="success" @click="testConnection(row)">{{ t('transfer.localEngines.testConnection') }}</el-button>
          <el-button size="small" type="primary" :icon="Edit" @click="editResource(row)">{{ t('transfer.localEngines.edit') }}</el-button>
          <el-button size="small" type="warning" :icon="Upload" @click="syncToSystem(row)">{{ t('transfer.localEngines.pushToSystem') }}</el-button>
          <el-button size="small" type="danger" :icon="Delete" @click="deleteResource(row)">{{ t('transfer.localEngines.delete') }}</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-pagination
      v-if="total > pageSize"
      v-model:current-page="currentPage"
      :page-size="pageSize"
      :total="total"
      layout="total, prev, pager, next"
      style="margin-top: 20px; justify-content: flex-end"
      @current-change="loadResources"
    />
  </el-card>

  <!-- 新增/编辑对话框 -->
  <el-dialog
    v-model="dialogVisible"
    :title="dialogTitle"
    width="600px"
    @close="resetForm"
  >
    <StorageEngineForm
      ref="storageFormRef"
      v-model="form"
      :is-edit="isEdit"
      :type-options="localEngineTypeOptions"
    />

    <template #footer>
      <el-button @click="dialogVisible = false">{{ t('transfer.localEngines.cancel') }}</el-button>
      <el-button type="warning" :loading="testing" @click="testBeforeCreate">{{ t('transfer.localEngines.testConnection') }}</el-button>
      <el-button type="primary" :loading="submitting" @click="submitForm">{{ t('transfer.localEngines.save') }}</el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { ref, onMounted, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { localEnginesAPI } from "../api/localEngines"
import { Plus, Edit, Delete, Upload, RefreshRight } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { StorageEngineForm, formatDate } from '@common-ui'

const engines = ref([])
const loading = ref(false)
const currentPage = ref(1)
const pageSize = ref(10)
const total = ref(0)
const { t } = useI18n()

const dialogVisible = ref(false)
const storageFormRef = ref(null)
const testing = ref(false)
const submitting = ref(false)
const isEdit = ref(false)
const editId = ref(null)

const form = ref({
  engine_type: '',
  name: '',
  description: '',
  is_active: true,
  connection_info: {}
})

const SENSITIVE_PLACEHOLDER = '********'

const dialogTitle = computed(() => isEdit.value ? t('transfer.localEngines.editTitle') : t('transfer.localEngines.addTitle'))

const localEngineTypeOptions = [
  { label: 'PostgreSQL', value: 'postgresql' },
  { label: 'MySQL', value: 'mysql' },
  { label: 'MinIO', value: 'minio' },
  { label: 'Amazon S3', value: 's3' },
  { label: 'SpatiaLite/SQLite', value: 'spatialite' }
]

const resourceTypeMap = {
  'postgresql': 'PostgreSQL',
  'mysql': 'MySQL',
  'minio': 'MinIO',
  's3': 'Amazon S3',
  'oss': '阿里云 OSS',
  'spatialite': 'SpatiaLite/SQLite',
  'sqlite': 'SQLite'
}

const getResourceTypeLabel = (type) => {
  return resourceTypeMap[type] || type
}

const getResourceTypeColor = (type) => {
  const colorMap = {
    'postgresql': 'primary',
    'mysql': 'success',
    'minio': 'warning',
    's3': 'success',
    'oss': 'info',
    'spatialite': 'info',
    'sqlite': 'info'
  }
  return colorMap[type] || 'info'
}

const loadResources = async () => {
  loading.value = true
  try {
    const list = await localEnginesAPI.list()
    engines.value = Array.isArray(list) ? list : []
    total.value = engines.value.length
  } catch (error) {
    ElMessage.error(t('transfer.localEngines.loadFailed', { error: error.response?.data?.error || error.message }))
    console.error(error)
  } finally {
    loading.value = false
  }
}

const showAddDialog = () => {
  isEdit.value = false
  editId.value = null
  resetForm()
  dialogVisible.value = true
}

const editResource = (row) => {
  isEdit.value = true
  editId.value = row.id
  form.value = {
    engine_type: row.engine_type,
    name: row.name,
    description: row.description,
    is_active: row.is_active,
    connection_info: { ...row.connection_info }
  }
  dialogVisible.value = true
}

const testBeforeCreate = async () => {
  const valid = await storageFormRef.value?.validate()
  if (!valid) {
    ElMessage.warning(t('transfer.localEngines.fillConnectionInfo'))
    return
  }

  testing.value = true
  try {
    // 如果是编辑模式，使用已保存资源的ID进行测试（会使用数据库中的真实密钥）
    if (isEdit.value && editId.value) {
      const response = await localEnginesAPI.testExisting(editId.value)
      if (response.success) {
        ElMessage.success(t('transfer.localEngines.connectionSuccess'))
      } else {
        ElMessage.error(t('transfer.localEngines.connectionFailed', { error: response.error || response.message || '未知错误' }))
      }
    } else {
      // 新增模式，使用表单数据测试
      const response = await localEnginesAPI.testConnection(buildRequestPayload(form.value))
      if (response.success) {
        ElMessage.success(t('transfer.localEngines.connectionSuccess'))
      } else {
        ElMessage.error(t('transfer.localEngines.connectionFailed', { error: response.error || response.message || '未知错误' }))
      }
    }
  } catch (error) {
    ElMessage.error(t('transfer.localEngines.connectionFailed', { error: error.response?.data?.error || error.message }))
  } finally {
    testing.value = false
  }
}

const testConnection = async (row) => {
  try {
    const response = await localEnginesAPI.testExisting(row.id)
    if (response.success) {
      ElMessage.success(t('transfer.localEngines.connectionSuccess'))
    } else {
      ElMessage.error(t('transfer.localEngines.connectionFailed', { error: response.error || response.message || '未知错误' }))
    }
  } catch (error) {
    ElMessage.error(t('transfer.localEngines.connectionFailed', { error: error.response?.data?.error || error.message }))
  }
}

const submitForm = async () => {
  const valid = await storageFormRef.value?.validate()
  if (!valid) return

  submitting.value = true
  try {
    const payload = buildRequestPayload(form.value)
    if (isEdit.value) {
      await localEnginesAPI.update(editId.value, payload)
      ElMessage.success(t('transfer.localEngines.updateSuccess'))
    } else {
      await localEnginesAPI.create(payload)
      ElMessage.success(t('transfer.localEngines.createSuccess'))
    }
    dialogVisible.value = false
    loadResources()
  } catch (error) {
    ElMessage.error(error.response?.data?.error || t('transfer.localEngines.deleteFailed'))
  } finally {
    submitting.value = false
  }
}

const syncToSystem = async (row) => {
  ElMessageBox.confirm(
    t('transfer.localEngines.pushConfirm', { name: row.name }),
    t('transfer.localEngines.pushConfirmTitle'),
    {
      confirmButtonText: t('transfer.localEngines.confirm'),
      cancelButtonText: t('transfer.localEngines.cancel'),
      type: 'warning'
    }
  ).then(async () => {
    try {
      await localEnginesAPI.syncToSystem(row.id)
      ElMessage.success(t('transfer.localEngines.pushSuccess'))
    } catch (error) {
      ElMessage.error(t('transfer.localEngines.pushFailed', { error: error.response?.data?.error || error.message }))
    }
  }).catch(() => {})
}

const deleteResource = (row) => {
  ElMessageBox.confirm(t('transfer.localEngines.deleteConfirm', { name: row.name }), t('transfer.localEngines.deleteConfirmTitle'), {
    confirmButtonText: t('transfer.localEngines.confirm'),
    cancelButtonText: t('transfer.localEngines.cancel'),
    type: 'warning'
  }).then(async () => {
    try {
      await localEnginesAPI.delete(row.id)
      ElMessage.success(t('transfer.localEngines.deleteSuccess'))
      loadResources()
    } catch (error) {
      ElMessage.error(error.response?.data?.error || t('transfer.localEngines.deleteFailed'))
    }
  }).catch(() => {})
}

const resetForm = () => {
  form.value = {
    engine_type: '',
    name: '',
    description: '',
    is_active: true,
    connection_info: {}
  }
  storageFormRef.value?.reset()
}

const buildRequestPayload = (data) => {
  const connectionInfo = sanitizeConnectionInfoForSubmit(data.connection_info)
  return {
    engine_type: data.engine_type,
    name: data.name,
    description: data.description,
    is_active: data.is_active,
    connection_info: connectionInfo
  }
}

const sanitizeConnectionInfoForSubmit = (connectionInfo) => {
  const result = {}
  const source = connectionInfo || {}

  Object.entries(source).forEach(([key, value]) => {
    if (key === '_has_password' || key === '_has_secret_key') {
      return
    }

    if ((key === 'password' || key === 'secret_key') &&
      (value === SENSITIVE_PLACEHOLDER || value === '' || value === null)) {
      return
    }

    result[key] = value
  })

  return result
}

onMounted(() => {
  loadResources()
})
</script>

<style scoped>
.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-weight: 600;
}

.header-actions {
  display: flex;
  gap: 8px;
}
</style>
