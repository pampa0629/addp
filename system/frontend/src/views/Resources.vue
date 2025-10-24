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
      <StorageEngineForm
        ref="storageFormRef"
        v-model="form"
        :is-edit="isEdit"
      />

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
import { StorageEngineForm } from '@common-ui'

const resources = ref([])
const loading = ref(false)
const currentPage = ref(1)
const pageSize = ref(10)
const total = ref(0)

const dialogVisible = ref(false)
const storageFormRef = ref(null)
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

const testBeforeCreate = async () => {
  const valid = await storageFormRef.value?.validate()
  if (!valid) {
    ElMessage.warning('请完整填写连接信息')
    return
  }

  testing.value = true
  try {
    // 如果是编辑模式，使用已保存资源的ID进行测试（会使用数据库中的真实密钥）
    if (isEdit.value && editId.value) {
      const response = await resourcesAPI.testExistingConnection(editId.value)
      if (response.data.success) {
        ElMessage.success('连接测试成功！')
      } else {
        ElMessage.error(`连接测试失败: ${response.data.error || response.data.message}`)
      }
    } else {
      // 新增模式，使用表单数据测试
      const response = await resourcesAPI.testConnection(form.value)
      if (response.data.success) {
        ElMessage.success('连接测试成功！')
      } else {
        ElMessage.error(`连接测试失败: ${response.data.error || response.data.message}`)
      }
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
  const valid = await storageFormRef.value?.validate()
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
  storageFormRef.value?.reset()
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
