<template>
  <div class="page-container">
    <!-- SuperAdmin 显示租户管理 -->
    <Tenants v-if="currentUser?.user_type === 'super_admin'" />

    <!-- 租户管理员和普通用户显示用户管理 -->
    <el-card v-else>
      <template #header>
        <div class="card-header">
          <span>{{ currentUser?.user_type === 'user' ? '我的信息' : '用户管理' }}</span>
          <el-button
            v-if="currentUser?.user_type === 'tenant_admin'"
            type="primary"
            :icon="Plus"
            @click="openAddDialog"
          >新增用户</el-button>
        </div>
      </template>

      <el-table :data="users" v-loading="loading" stripe>
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column prop="username" label="用户名" />
        <el-table-column prop="email" label="邮箱" />
        <el-table-column prop="full_name" label="姓名" />
        <el-table-column label="用户类型" width="120">
          <template #default="{ row }">
            <el-tag :type="getUserTypeTag(row.user_type)">
              {{ getUserTypeText(row.user_type) }}
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
            <!-- 普通用户只能编辑自己，不能删除 -->
            <template v-if="currentUser?.user_type === 'user'">
              <el-button
                v-if="row.id === currentUser?.id"
                size="small"
                type="primary"
                :icon="Edit"
                @click="openEditDialog(row)"
              >编辑</el-button>
            </template>
            <!-- 租户管理员可以编辑和删除普通用户 -->
            <template v-else-if="currentUser?.user_type === 'tenant_admin'">
              <el-button size="small" type="primary" :icon="Edit" @click="openEditDialog(row)">编辑</el-button>
              <el-button
                v-if="row.user_type === 'user'"
                size="small"
                type="danger"
                :icon="Delete"
                @click="handleDelete(row)"
              >删除</el-button>
            </template>
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

        <!-- 租户管理员创建用户时显示用户类型（固定为普通用户） -->
        <el-form-item label="用户类型" prop="user_type" v-if="currentUser?.user_type === 'tenant_admin'">
          <el-select v-model="userForm.user_type" placeholder="请选择用户类型" disabled>
            <el-option label="普通用户" value="user" />
          </el-select>
        </el-form-item>

        <!-- 只有租户管理员可以修改用户状态 -->
        <el-form-item label="状态" v-if="isEdit && currentUser?.user_type === 'tenant_admin'">
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
import { ref, reactive, computed, onMounted, nextTick } from 'vue'
import { usersAPI } from '../api/users'
import { authAPI } from '../api/auth'
import { Plus, Edit, Delete } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useAuthStore } from '../store/auth'
import Tenants from './Tenants.vue'

const authStore = useAuthStore()
const users = ref([])
const loading = ref(false)
const currentPage = ref(1)
const pageSize = ref(10)
const total = ref(0)
const currentUser = computed(() => authStore.user)

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
  user_type: 'user'
})

// 用户类型相关函数
const getUserTypeText = (userType) => {
  const typeMap = {
    'super_admin': '超级管理员',
    'tenant_admin': '租户管理员',
    'user': '普通用户'
  }
  return typeMap[userType] || '未知'
}

const getUserTypeTag = (userType) => {
  const tagMap = {
    'super_admin': 'danger',
    'tenant_admin': 'warning',
    'user': 'info'
  }
  return tagMap[userType] || 'info'
}

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
  userForm.user_type = 'user'
  formRef.value?.resetFields()
}

const openAddDialog = () => {
  isEdit.value = false
  editingUserId.value = null
  // Reset form data before opening dialog
  userForm.username = ''
  userForm.password = ''
  userForm.email = ''
  userForm.full_name = ''
  userForm.is_active = true
  userForm.user_type = 'user'

  dialogVisible.value = true

  // Clear validation after dialog opens
  nextTick(() => {
    formRef.value?.clearValidate()
  })
}

const openEditDialog = (row) => {
  resetForm()
  isEdit.value = true
  editingUserId.value = row.id
  userForm.username = row.username
  userForm.email = row.email
  userForm.full_name = row.full_name
  userForm.is_active = row.is_active
  userForm.user_type = row.user_type || 'user'
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
            user_type: userForm.user_type
          }
          if (userForm.password) {
            data.password = userForm.password
          }
          await usersAPI.update(editingUserId.value, data)
          ElMessage.success('更新用户成功')
        } else {
          // 新增用户
          await usersAPI.create({
            username: userForm.username,
            password: userForm.password,
            email: userForm.email,
            full_name: userForm.full_name,
            user_type: userForm.user_type
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
  // 检查是否为admin用户
  if (row.username === 'admin') {
    ElMessage.error('admin账号不能被删除')
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

onMounted(async () => {
  // 确保用户信息已加载
  if (!authStore.user) {
    try {
      await authStore.fetchUser()
    } catch (error) {
      console.error('Failed to load user:', error)
    }
  }
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