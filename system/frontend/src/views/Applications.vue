<template>
  <div class="page-container">
    <el-card>
      <template #header>
        <div class="card-header">
          <span>应用管理</span>
          <el-button type="primary" @click="openCreateDialog">
            <el-icon><Plus /></el-icon>
            创建应用
          </el-button>
        </div>
      </template>

      <el-table :data="applications" v-loading="loading" stripe>
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column prop="name" label="应用名称" min-width="150" />
        <el-table-column prop="description" label="描述" min-width="200" show-overflow-tooltip />
        <el-table-column label="允许服务" min-width="200">
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
              全部服务
            </span>
          </template>
        </el-table-column>
        <el-table-column label="速率限制" width="120">
          <template #default="{ row }">
            {{ row.rate_limit_per_minute }} 次/分钟
          </template>
        </el-table-column>
        <el-table-column label="状态" width="100">
          <template #default="{ row }">
            <el-tag :type="row.status === 'active' ? 'success' : 'danger'">
              {{ row.status === 'active' ? '激活' : '暂停' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="创建时间" width="180">
          <template #default="{ row }">
            {{ formatDate(row.created_at) }}
          </template>
        </el-table-column>
        <el-table-column label="操作" width="300" fixed="right">
          <template #default="{ row }">
            <el-button size="small" type="primary" @click="openKeysDialog(row)">
              <el-icon><Key /></el-icon>
              API Keys
            </el-button>
            <el-button size="small" @click="openEditDialog(row)">
              <el-icon><Edit /></el-icon>
              编辑
            </el-button>
            <el-button size="small" type="danger" @click="handleDelete(row)">
              <el-icon><Delete /></el-icon>
              删除
            </el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <!-- 创建/编辑应用对话框 -->
    <el-dialog
      v-model="dialogVisible"
      :title="isEdit ? '编辑应用' : '创建应用'"
      width="600px"
    >
      <el-form :model="formData" :rules="rules" ref="formRef" label-width="120px">
        <el-form-item label="应用名称" prop="name">
          <el-input v-model="formData.name" placeholder="请输入应用名称" />
        </el-form-item>
        <el-form-item label="描述" prop="description">
          <el-input
            v-model="formData.description"
            type="textarea"
            :rows="3"
            placeholder="请输入应用描述"
          />
        </el-form-item>
        <el-form-item label="允许服务">
          <el-select
            v-model="formData.allowed_services"
            multiple
            placeholder="选择允许访问的服务（留空表示全部）"
            style="width: 100%"
          >
            <el-option label="POI 服务" value="poi_service" />
            <el-option label="地址服务" value="address_service" />
            <el-option label="地图服务" value="map_service" />
            <el-option label="数据服务" value="data_service" />
          </el-select>
        </el-form-item>
        <el-form-item label="速率限制" prop="rate_limit_per_minute">
          <el-input-number
            v-model="formData.rate_limit_per_minute"
            :min="10"
            :max="10000"
            :step="10"
            placeholder="每分钟请求次数"
            style="width: 100%"
          />
          <div class="form-tip">每分钟允许的最大请求次数</div>
        </el-form-item>
        <el-form-item label="状态" prop="status">
          <el-radio-group v-model="formData.status">
            <el-radio value="active">激活</el-radio>
            <el-radio value="suspended">暂停</el-radio>
          </el-radio-group>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" @click="handleSubmit">确定</el-button>
      </template>
    </el-dialog>

    <!-- API Keys 管理对话框 -->
    <el-dialog
      v-model="keysDialogVisible"
      :title="`API Keys - ${currentApp?.name}`"
      width="800px"
    >
      <div style="margin-bottom: 16px">
        <el-button type="primary" @click="openGenerateKeyDialog">
          <el-icon><Plus /></el-icon>
          生成新 Key
        </el-button>
      </div>

      <el-table :data="apiKeys" v-loading="keysLoading" stripe>
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column label="Key 前缀" width="150">
          <template #default="{ row }">
            <code>{{ row.key_prefix }}***</code>
          </template>
        </el-table-column>
        <el-table-column prop="name" label="名称" min-width="120" />
        <el-table-column label="最后使用" width="180">
          <template #default="{ row }">
            <span v-if="row.last_used_at">{{ formatDate(row.last_used_at) }}</span>
            <span v-else class="text-secondary">从未使用</span>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="100">
          <template #default="{ row }">
            <el-tag :type="row.status === 'active' ? 'success' : 'danger'">
              {{ row.status === 'active' ? '激活' : '已撤销' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="创建时间" width="180">
          <template #default="{ row }">
            {{ formatDate(row.created_at) }}
          </template>
        </el-table-column>
        <el-table-column label="操作" width="100" fixed="right">
          <template #default="{ row }">
            <el-button
              v-if="row.status === 'active'"
              size="small"
              type="danger"
              @click="handleRevokeKey(row)"
            >
              撤销
            </el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-dialog>

    <!-- 生成 API Key 对话框 -->
    <el-dialog
      v-model="generateKeyDialogVisible"
      title="生成 API Key"
      width="500px"
    >
      <el-form :model="keyFormData" :rules="keyRules" ref="keyFormRef" label-width="100px">
        <el-form-item label="Key 名称" prop="name">
          <el-input v-model="keyFormData.name" placeholder="请输入 Key 名称（便于识别）" />
        </el-form-item>
        <el-form-item label="过期时间">
          <el-date-picker
            v-model="keyFormData.expires_at"
            type="datetime"
            placeholder="选择过期时间（可选）"
            style="width: 100%"
          />
          <div class="form-tip">留空表示永不过期</div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="generateKeyDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="handleGenerateKey">生成</el-button>
      </template>
    </el-dialog>

    <!-- 显示生成的 API Key -->
    <el-dialog
      v-model="showKeyDialogVisible"
      title="API Key 已生成"
      width="600px"
      :close-on-click-modal="false"
      :close-on-press-escape="false"
    >
      <el-alert
        title="重要提示"
        type="warning"
        :closable="false"
        style="margin-bottom: 16px"
      >
        请立即复制并保存 API Key，关闭窗口后将无法再次查看！
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
              复制
            </el-button>
          </template>
        </el-input>
      </div>

      <template #footer>
        <el-button type="primary" @click="showKeyDialogVisible = false">
          我已保存，关闭
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Edit, Delete, Key, DocumentCopy } from '@element-plus/icons-vue'
import { applicationsAPI } from '@/api/applications'

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
  name: [{ required: true, message: '请输入应用名称', trigger: 'blur' }],
  rate_limit_per_minute: [{ required: true, message: '请设置速率限制', trigger: 'blur' }]
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
  name: [{ required: true, message: '请输入 Key 名称', trigger: 'blur' }]
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
    applications.value = response.data.applications || []
  } catch (error) {
    ElMessage.error('加载应用列表失败: ' + (error.response?.data?.error || error.message))
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
          ElMessage.success('更新成功')
        } else {
          await applicationsAPI.create(formData.value)
          ElMessage.success('创建成功')
        }
        dialogVisible.value = false
        loadApplications()
      } catch (error) {
        ElMessage.error('操作失败: ' + (error.response?.data?.error || error.message))
      }
    }
  })
}

// 删除应用
const handleDelete = (app) => {
  ElMessageBox.confirm(
    `确定要删除应用 "${app.name}" 吗？这将同时删除该应用的所有 API Keys。`,
    '确认删除',
    {
      confirmButtonText: '确定',
      cancelButtonText: '取消',
      type: 'warning'
    }
  ).then(async () => {
    try {
      await applicationsAPI.delete(app.id)
      ElMessage.success('删除成功')
      loadApplications()
    } catch (error) {
      ElMessage.error('删除失败: ' + (error.response?.data?.error || error.message))
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
    apiKeys.value = response.data.keys || []
  } catch (error) {
    ElMessage.error('加载 API Keys 失败: ' + (error.response?.data?.error || error.message))
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
        generatedApiKey.value = response.data.plain_text_key
        generateKeyDialogVisible.value = false
        showKeyDialogVisible.value = true
        await loadApiKeys(currentApp.value.id)
      } catch (error) {
        ElMessage.error('生成 Key 失败: ' + (error.response?.data?.error || error.message))
      }
    }
  })
}

// 复制 API Key
const copyApiKey = async () => {
  try {
    await navigator.clipboard.writeText(generatedApiKey.value)
    ElMessage.success('已复制到剪贴板')
  } catch (error) {
    ElMessage.error('复制失败，请手动复制')
  }
}

// 撤销 API Key
const handleRevokeKey = (key) => {
  ElMessageBox.confirm(
    `确定要撤销 API Key "${key.name}" 吗？撤销后该 Key 将立即失效。`,
    '确认撤销',
    {
      confirmButtonText: '确定',
      cancelButtonText: '取消',
      type: 'warning'
    }
  ).then(async () => {
    try {
      await applicationsAPI.revokeKey(currentApp.value.id, key.id)
      ElMessage.success('撤销成功')
      await loadApiKeys(currentApp.value.id)
    } catch (error) {
      ElMessage.error('撤销失败: ' + (error.response?.data?.error || error.message))
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
  color: #909399;
  font-size: 13px;
}

.form-tip {
  font-size: 12px;
  color: #909399;
  margin-top: 4px;
}

.api-key-display {
  padding: 16px;
  background: #f5f7fa;
  border-radius: 4px;
}

.api-key-display .el-input {
  font-family: 'Courier New', monospace;
  font-size: 14px;
}
</style>
