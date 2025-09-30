<template>

    <el-card>
      <template #header>
        <div class="card-header">
          <span>存储引擎管理</span>
          <el-button type="primary" :icon="Plus" @click="showAddDialog">新增存储引擎</el-button>
        </div>
      </template>

      <el-table :data="resources" v-loading="loading" stripe>
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column prop="name" label="名称" min-width="150" />
        <el-table-column prop="resource_type" label="类型" width="150">
          <template #default="{ row }">
            <el-tag :type="getResourceTypeColor(row.resource_type)">
              {{ getResourceTypeLabel(row.resource_type) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="description" label="描述" min-width="200" />
        <el-table-column label="状态" width="100">
          <template #default="{ row }">
            <el-tag :type="row.is_active ? 'success' : 'danger'">
              {{ row.is_active ? '激活' : '禁用' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="创建时间" width="180">
          <template #default="{ row }">
            {{ formatDate(row.created_at) }}
          </template>
        </el-table-column>
        <el-table-column label="操作" width="250" fixed="right">
          <template #default="{ row }">
            <el-button size="small" type="success" @click="testConnection(row)">测试连接</el-button>
            <el-button size="small" type="primary" :icon="Edit" @click="editResource(row)">编辑</el-button>
            <el-button size="small" type="danger" :icon="Delete" @click="deleteResource(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>

      <el-pagination
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
      <el-form :model="form" :rules="rules" ref="formRef" label-width="120px">
        <el-form-item label="存储引擎类型" prop="resource_type">
          <el-select
            v-model="form.resource_type"
            placeholder="请选择存储引擎类型"
            @change="handleTypeChange"
            :disabled="isEdit"
          >
            <el-option label="PostgreSQL" value="postgresql" />
            <el-option label="MinIO" value="minio" />
          </el-select>
        </el-form-item>

        <el-form-item label="名称" prop="name">
          <el-input v-model="form.name" placeholder="请输入资源名称" />
        </el-form-item>

        <el-form-item label="描述" prop="description">
          <el-input
            v-model="form.description"
            type="textarea"
            :rows="2"
            placeholder="请输入资源描述"
          />
        </el-form-item>

        <!-- PostgreSQL 配置 -->
        <template v-if="form.resource_type === 'postgresql'">
          <el-form-item label="主机地址" prop="connection_info.host">
            <el-input v-model="form.connection_info.host" placeholder="localhost" />
          </el-form-item>
          <el-form-item label="端口" prop="connection_info.port">
            <el-input-number v-model="form.connection_info.port" :min="1" :max="65535" />
          </el-form-item>
          <el-form-item label="数据库名" prop="connection_info.database">
            <el-input v-model="form.connection_info.database" placeholder="数据库名称" />
          </el-form-item>
          <el-form-item label="用户名" prop="connection_info.user">
            <el-input v-model="form.connection_info.user" placeholder="数据库用户名" />
          </el-form-item>
          <el-form-item label="密码" prop="connection_info.password">
            <el-input
              v-model="form.connection_info.password"
              type="password"
              placeholder="数据库密码"
              show-password
            />
          </el-form-item>
          <el-form-item label="SSL 模式">
            <el-select v-model="form.connection_info.sslmode">
              <el-option label="禁用 (disable)" value="disable" />
              <el-option label="要求 (require)" value="require" />
              <el-option label="验证CA (verify-ca)" value="verify-ca" />
              <el-option label="完全验证 (verify-full)" value="verify-full" />
            </el-select>
          </el-form-item>
        </template>

        <!-- MinIO 配置 -->
        <template v-if="form.resource_type === 'minio'">
          <el-form-item label="端点地址" prop="connection_info.endpoint">
            <el-input v-model="form.connection_info.endpoint" placeholder="localhost:9000" />
          </el-form-item>
          <el-form-item label="Access Key" prop="connection_info.access_key">
            <el-input v-model="form.connection_info.access_key" placeholder="Access Key" />
          </el-form-item>
          <el-form-item label="Secret Key" prop="connection_info.secret_key">
            <el-input
              v-model="form.connection_info.secret_key"
              type="password"
              placeholder="Secret Key"
              show-password
            />
          </el-form-item>
          <el-form-item label="Bucket">
            <el-input v-model="form.connection_info.bucket" placeholder="存储桶名称（可选）" />
          </el-form-item>
          <el-form-item label="使用 SSL">
            <el-switch v-model="form.connection_info.use_ssl" />
          </el-form-item>
        </template>

        <el-form-item label="激活状态">
          <el-switch v-model="form.is_active" />
        </el-form-item>
      </el-form>

      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="warning" :loading="testing" @click="testBeforeCreate">测试连接</el-button>
        <el-button type="primary" :loading="submitting" @click="submitForm">保存</el-button>
      </template>
    </el-dialog>
</template>

<script setup>
import { ref, onMounted, computed } from 'vue'
import { resourcesAPI } from '../api/resources'
import { Plus, Edit, Delete } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'

const resources = ref([])
const loading = ref(false)
const currentPage = ref(1)
const pageSize = ref(10)
const total = ref(0)

const dialogVisible = ref(false)
const formRef = ref(null)
const testing = ref(false)
const submitting = ref(false)
const isEdit = ref(false)
const editId = ref(null)

const form = ref({
  resource_type: '',
  name: '',
  description: '',
  is_active: true,
  connection_info: {}
})

const rules = {
  resource_type: [{ required: true, message: '请选择存储引擎类型', trigger: 'change' }],
  name: [{ required: true, message: '请输入资源名称', trigger: 'blur' }],
  'connection_info.host': [{ required: true, message: '请输入主机地址', trigger: 'blur' }],
  'connection_info.port': [{ required: true, message: '请输入端口', trigger: 'blur' }],
  'connection_info.database': [{ required: true, message: '请输入数据库名', trigger: 'blur' }],
  'connection_info.user': [{ required: true, message: '请输入用户名', trigger: 'blur' }],
  'connection_info.password': [{ required: true, message: '请输入密码', trigger: 'blur' }],
  'connection_info.endpoint': [{ required: true, message: '请输入端点地址', trigger: 'blur' }],
  'connection_info.access_key': [{ required: true, message: '请输入Access Key', trigger: 'blur' }],
  'connection_info.secret_key': [{ required: true, message: '请输入Secret Key', trigger: 'blur' }]
}

const dialogTitle = computed(() => isEdit.value ? '编辑存储引擎' : '新增存储引擎')

const resourceTypeMap = {
  'postgresql': 'PostgreSQL',
  'minio': 'MinIO',
  'database': '数据库',
  'compute_engine': '计算引擎'
}

const getResourceTypeLabel = (type) => {
  return resourceTypeMap[type] || type
}

const getResourceTypeColor = (type) => {
  const colorMap = {
    'postgresql': 'primary',
    'minio': 'warning',
    'database': 'success',
    'compute_engine': 'info'
  }
  return colorMap[type] || ''
}

const formatDate = (dateString) => {
  return new Date(dateString).toLocaleString('zh-CN')
}

const loadResources = async () => {
  loading.value = true
  try {
    const response = await resourcesAPI.list(currentPage.value, pageSize.value)
    resources.value = response.data || []
    total.value = (response.data || []).length
  } catch (error) {
    ElMessage.error('加载资源列表失败')
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
    resource_type: row.resource_type,
    name: row.name,
    description: row.description,
    is_active: row.is_active,
    connection_info: { ...row.connection_info }
  }
  dialogVisible.value = true
}

const handleTypeChange = (type) => {
  // 初始化连接信息
  if (type === 'postgresql') {
    form.value.connection_info = {
      host: 'localhost',
      port: 5432,
      database: '',
      user: '',
      password: '',
      sslmode: 'disable'
    }
  } else if (type === 'minio') {
    form.value.connection_info = {
      endpoint: 'localhost:9000',
      access_key: '',
      secret_key: '',
      bucket: '',
      use_ssl: false
    }
  }
}

const testBeforeCreate = async () => {
  const valid = await formRef.value.validate().catch(() => false)
  if (!valid) {
    ElMessage.warning('请完整填写连接信息')
    return
  }

  testing.value = true
  try {
    const response = await resourcesAPI.testConnection(form.value)
    if (response.data.success) {
      ElMessage.success('连接测试成功！')
    } else {
      ElMessage.error(`连接测试失败: ${response.data.error || response.data.message}`)
    }
  } catch (error) {
    ElMessage.error(`连接测试失败: ${error.response?.data?.error || error.message}`)
  } finally {
    testing.value = false
  }
}

const testConnection = async (row) => {
  try {
    const response = await resourcesAPI.testExistingConnection(row.id)
    if (response.data.success) {
      ElMessage.success('连接测试成功！')
    } else {
      ElMessage.error(`连接测试失败: ${response.data.error || response.data.message}`)
    }
  } catch (error) {
    ElMessage.error(`连接测试失败: ${error.response?.data?.error || error.message}`)
  }
}

const submitForm = async () => {
  const valid = await formRef.value.validate().catch(() => false)
  if (!valid) return

  submitting.value = true
  try {
    if (isEdit.value) {
      await resourcesAPI.update(editId.value, form.value)
      ElMessage.success('更新成功')
    } else {
      await resourcesAPI.create(form.value)
      ElMessage.success('创建成功')
    }
    dialogVisible.value = false
    loadResources()
  } catch (error) {
    ElMessage.error(error.response?.data?.error || '操作失败')
  } finally {
    submitting.value = false
  }
}

const deleteResource = (row) => {
  ElMessageBox.confirm(`确定要删除存储引擎 "${row.name}" 吗？`, '确认删除', {
    confirmButtonText: '确定',
    cancelButtonText: '取消',
    type: 'warning'
  }).then(async () => {
    try {
      await resourcesAPI.delete(row.id)
      ElMessage.success('删除成功')
      loadResources()
    } catch (error) {
      ElMessage.error(error.response?.data?.error || '删除失败')
    }
  }).catch(() => {})
}

const resetForm = () => {
  form.value = {
    resource_type: '',
    name: '',
    description: '',
    is_active: true,
    connection_info: {}
  }
  formRef.value?.clearValidate()
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
</style>