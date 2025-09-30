<template>
  <div class="page-container">
    <el-card>
      <template #header>
        <div class="card-header">
          <span>用户管理</span>
          <el-button type="primary" :icon="Plus" @click="openAddDialog">新增用户</el-button>
        </div>
      </template>

      <el-table :data="users" v-loading="loading" stripe>
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column prop="username" label="用户名" />
        <el-table-column prop="email" label="邮箱" />
        <el-table-column prop="full_name" label="姓名" />
        <el-table-column label="用户类型" width="120">
          <template #default="{ row }">
            <el-tag :type="row.is_superuser ? 'danger' : 'info'">
              {{ row.is_superuser ? '管理员' : '普通用户' }}
            </el-tag>
          </template>
        </el-table-column>
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
              v-if="!row.is_superuser"
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
        :page-size="pageSize"
        :total="total"
        layout="total, prev, pager, next"
        style="margin-top: 20px; justify-content: flex-end"
        @current-change="loadUsers"
      />
    </el-card>

    <!-- 新增/编辑用户对话框 -->
    <el-dialog
      v-model="dialogVisible"
      :title="dialogTitle"
      width="500px"
    >
      <el-form
        ref="formRef"
        :model="userForm"
        :rules="rules"
        label-width="80px"
      >
        <el-form-item label="用户名" prop="username">
          <el-input v-model="userForm.username" :disabled="isEdit" placeholder="请输入用户名" />
        </el-form-item>

        <el-form-item label="密码" prop="password">
          <el-input
            v-model="userForm.password"
            type="password"
            show-password
            :placeholder="isEdit ? '不修改请留空' : '请输入密码'"
          />
        </el-form-item>

        <el-form-item label="邮箱" prop="email">
          <el-input v-model="userForm.email" placeholder="请输入邮箱" />
        </el-form-item>

        <el-form-item label="姓名" prop="full_name">
          <el-input v-model="userForm.full_name" placeholder="请输入姓名" />
        </el-form-item>

        <el-form-item label="用户类型" prop="is_superuser">
          <el-radio-group v-model="userForm.is_superuser">
            <el-radio :value="false">普通用户</el-radio>
            <el-radio :value="true">管理员</el-radio>
          </el-radio-group>
        </el-form-item>

        <el-form-item label="状态" v-if="isEdit">
          <el-switch v-model="userForm.is_active" active-text="激活" inactive-text="禁用" />
        </el-form-item>
      </el-form>

      <template #footer>
        <span class="dialog-footer">
          <el-button @click="dialogVisible = false">取消</el-button>
          <el-button type="primary" @click="handleSubmit" :loading="submitLoading">
            确定
          </el-button>
        </span>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import { usersAPI } from '../api/users'
import { authAPI } from '../api/auth'
import { Plus, Edit, Delete } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'

const users = ref([])
const loading = ref(false)
const currentPage = ref(1)
const pageSize = ref(10)
const total = ref(0)

// 对话框相关
const dialogVisible = ref(false)
const isEdit = ref(false)
const submitLoading = ref(false)
const formRef = ref(null)
const editingUserId = ref(null)

const userForm = reactive({
  username: '',
  password: '',
  email: '',
  full_name: '',
  is_active: true,
  is_superuser: false
})

const dialogTitle = computed(() => isEdit.value ? '编辑用户' : '新增用户')

const rules = {
  username: [
    { required: true, message: '请输入用户名', trigger: 'blur' },
    { min: 3, max: 20, message: '用户名长度为 3-20 个字符', trigger: 'blur' }
  ],
  password: [
    {
      required: computed(() => !isEdit.value),
      message: '请输入密码',
      trigger: 'blur'
    },
    { min: 6, message: '密码长度不能少于 6 位', trigger: 'blur' }
  ],
  email: [
    { type: 'email', message: '请输入正确的邮箱地址', trigger: 'blur' }
  ]
}

const formatDate = (dateString) => {
  return new Date(dateString).toLocaleString('zh-CN')
}

const loadUsers = async () => {
  loading.value = true
  try {
    const response = await usersAPI.list(currentPage.value, pageSize.value)
    users.value = response.data
    total.value = response.data.length
  } catch (error) {
    ElMessage.error('加载用户列表失败')
    console.error(error)
  } finally {
    loading.value = false
  }
}

const resetForm = () => {
  userForm.username = ''
  userForm.password = ''
  userForm.email = ''
  userForm.full_name = ''
  userForm.is_active = true
  userForm.is_superuser = false
  formRef.value?.resetFields()
}

const openAddDialog = () => {
  resetForm()
  isEdit.value = false
  editingUserId.value = null
  dialogVisible.value = true
}

const openEditDialog = (row) => {
  resetForm()
  isEdit.value = true
  editingUserId.value = row.id
  userForm.username = row.username
  userForm.email = row.email
  userForm.full_name = row.full_name
  userForm.is_active = row.is_active
  userForm.is_superuser = row.is_superuser
  dialogVisible.value = true
}

const handleSubmit = async () => {
  if (!formRef.value) return

  await formRef.value.validate(async (valid) => {
    if (valid) {
      submitLoading.value = true
      try {
        if (isEdit.value) {
          // 编辑用户
          const data = {
            email: userForm.email || null,
            full_name: userForm.full_name || null,
            is_active: userForm.is_active,
            is_superuser: userForm.is_superuser
          }
          if (userForm.password) {
            data.password = userForm.password
          }
          await usersAPI.update(editingUserId.value, data)
          ElMessage.success('更新用户成功')
        } else {
          // 新增用户
          await authAPI.register({
            username: userForm.username,
            password: userForm.password,
            email: userForm.email,
            full_name: userForm.full_name,
            is_superuser: userForm.is_superuser
          })
          ElMessage.success('新增用户成功')
        }
        dialogVisible.value = false
        loadUsers()
      } catch (error) {
        ElMessage.error(error.response?.data?.error || (isEdit.value ? '更新用户失败' : '新增用户失败'))
      } finally {
        submitLoading.value = false
      }
    }
  })
}

const handleDelete = (row) => {
  // 检查是否为超级管理员
  if (row.is_superuser) {
    ElMessage.error('超级管理员账号不能被删除')
    return
  }

  ElMessageBox.confirm(
    `确定要删除用户 "${row.username}" 吗？`,
    '警告',
    {
      confirmButtonText: '确定',
      cancelButtonText: '取消',
      type: 'warning',
    }
  ).then(async () => {
    try {
      await usersAPI.delete(row.id)
      ElMessage.success('删除成功')
      loadUsers()
    } catch (error) {
      ElMessage.error(error.response?.data?.error || '删除失败')
    }
  }).catch(() => {
    // 用户取消
  })
}

onMounted(() => {
  loadUsers()
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