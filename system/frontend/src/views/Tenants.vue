<template>
  <div class="page-container">
    <el-card>
      <template #header>
        <div class="card-header">
          <span>租户管理</span>
          <el-button type="primary" :icon="Plus" @click="openAddDialog">新增租户</el-button>
        </div>
      </template>

      <el-table :data="tenants" v-loading="loading" stripe>
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column prop="name" label="租户名称" />
        <el-table-column prop="description" label="描述" />
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
        <el-table-column label="操作" width="180" fixed="right">
          <template #default="{ row }">
            <el-button size="small" type="primary" :icon="Edit" @click="openEditDialog(row)">编辑</el-button>
            <el-button
              size="small"
              type="danger"
              :icon="Delete"
              @click="handleDelete(row)"
            >删除</el-button>
          </template>
        </el-table-column>
      </el-table>

      <el-pagination
        v-model:current-page="currentPage"
        v-model:page-size="pageSize"
        :total="total"
        :page-sizes="[10, 20, 50, 100]"
        layout="total, sizes, prev, pager, next, jumper"
        @size-change="fetchTenants"
        @current-change="fetchTenants"
        style="margin-top: 20px; justify-content: flex-end;"
      />
    </el-card>

    <!-- 新增/编辑租户对话框 -->
    <el-dialog
      v-model="dialogVisible"
      :title="isEdit ? '编辑租户' : '新增租户'"
      width="600px"
      @close="resetForm"
    >
      <el-form
        ref="formRef"
        :model="tenantForm"
        :rules="formRules"
        label-width="120px"
      >
        <el-form-item label="租户名称" prop="name">
          <el-input v-model="tenantForm.name" placeholder="请输入租户名称" />
        </el-form-item>
        <el-form-item label="描述" prop="description">
          <el-input
            v-model="tenantForm.description"
            type="textarea"
            :rows="3"
            placeholder="请输入租户描述"
          />
        </el-form-item>

        <!-- 新增租户时需要设置管理员 -->
        <template v-if="!isEdit">
          <el-divider content-position="left">租户管理员信息</el-divider>
          <el-form-item label="管理员用户名" prop="admin_username">
            <el-input v-model="tenantForm.admin_username" placeholder="请输入管理员用户名" />
          </el-form-item>
          <el-form-item label="管理员密码" prop="admin_password">
            <el-input v-model="tenantForm.admin_password" type="password" placeholder="请输入管理员密码" />
          </el-form-item>
          <el-form-item label="管理员邮箱" prop="admin_email">
            <el-input v-model="tenantForm.admin_email" placeholder="请输入管理员邮箱" />
          </el-form-item>
          <el-form-item label="管理员姓名" prop="admin_full_name">
            <el-input v-model="tenantForm.admin_full_name" placeholder="请输入管理员姓名" />
          </el-form-item>
        </template>
      </el-form>

      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" @click="handleSubmit" :loading="submitting">
          {{ isEdit ? '更新' : '创建' }}
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted, nextTick } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Edit, Delete } from '@element-plus/icons-vue'
import { tenantAPI } from '../api/tenant'

const tenants = ref([])
const loading = ref(false)
const currentPage = ref(1)
const pageSize = ref(10)
const total = ref(0)

const dialogVisible = ref(false)
const isEdit = ref(false)
const editingTenantId = ref(null)
const submitting = ref(false)
const formRef = ref(null)

const tenantForm = ref({
  name: '',
  description: '',
  admin_username: '',
  admin_password: '',
  admin_email: '',
  admin_full_name: ''
})

const formRules = {
  name: [
    { required: true, message: '请输入租户名称', trigger: 'blur' }
  ],
  admin_username: [
    { required: true, message: '请输入管理员用户名', trigger: 'blur' }
  ],
  admin_password: [
    { required: true, message: '请输入管理员密码', trigger: 'blur' },
    { min: 6, message: '密码长度至少6位', trigger: 'blur' }
  ]
}

const fetchTenants = async () => {
  loading.value = true
  try {
    const response = await tenantAPI.list({
      page: currentPage.value,
      page_size: pageSize.value
    })
    tenants.value = response.data
    total.value = response.data.length
  } catch (error) {
    ElMessage.error('获取租户列表失败')
  } finally {
    loading.value = false
  }
}

const openAddDialog = () => {
  isEdit.value = false
  editingTenantId.value = null
  tenantForm.value = {
    name: '',
    description: '',
    admin_username: '',
    admin_password: '',
    admin_email: '',
    admin_full_name: ''
  }
  dialogVisible.value = true
  nextTick(() => {
    formRef.value?.clearValidate()
  })
}

const openEditDialog = (tenant) => {
  isEdit.value = true
  editingTenantId.value = tenant.id
  tenantForm.value = {
    name: tenant.name,
    description: tenant.description
  }
  dialogVisible.value = true
  nextTick(() => {
    formRef.value?.clearValidate()
  })
}

const resetForm = () => {
  formRef.value?.resetFields()
}

const handleSubmit = async () => {
  if (!formRef.value) return

  try {
    await formRef.value.validate()
    submitting.value = true

    if (isEdit.value) {
      await tenantAPI.update(editingTenantId.value, {
        name: tenantForm.value.name,
        description: tenantForm.value.description
      })
      ElMessage.success('租户更新成功')
    } else {
      await tenantAPI.create(tenantForm.value)
      ElMessage.success('租户创建成功')
    }

    dialogVisible.value = false
    fetchTenants()
  } catch (error) {
    if (error.response?.data?.error) {
      ElMessage.error(error.response.data.error)
    } else {
      ElMessage.error(isEdit.value ? '租户更新失败' : '租户创建失败')
    }
  } finally {
    submitting.value = false
  }
}

const handleDelete = (tenant) => {
  ElMessageBox.confirm(
    `确定要删除租户"${tenant.name}"吗？删除租户将同时删除该租户下的所有用户！`,
    '警告',
    {
      confirmButtonText: '确定',
      cancelButtonText: '取消',
      type: 'warning',
    }
  ).then(async () => {
    try {
      await tenantAPI.delete(tenant.id)
      ElMessage.success('租户删除成功')
      fetchTenants()
    } catch (error) {
      ElMessage.error('租户删除失败')
    }
  }).catch(() => {
    // 用户取消删除
  })
}

const formatDate = (dateString) => {
  if (!dateString) return '-'
  return new Date(dateString).toLocaleString('zh-CN')
}

onMounted(() => {
  fetchTenants()
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
</style>
