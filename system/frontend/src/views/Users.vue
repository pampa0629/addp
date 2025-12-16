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
        <el-table-column label="操作" width="250" fixed="right">
          <template #default="{ row }">
            <!-- 普通用户只能编辑自己，可以修改自己的密码 -->
            <template v-if="currentUser?.user_type === 'user'">
              <el-button
                v-if="row.id === currentUser?.id"
                size="small"
                type="primary"
                :icon="Edit"
                @click="openEditDialog(row)"
              >编辑</el-button>
              <el-button
                v-if="row.id === currentUser?.id"
                size="small"
                type="warning"
                :icon="Key"
                @click="openChangePasswordDialog(row)"
              >修改密码</el-button>
            </template>
            <!-- 租户管理员可以编辑所有用户，可以修改自己的密码，可以删除普通用户 -->
            <template v-else-if="currentUser?.user_type === 'tenant_admin'">
              <el-button size="small" type="primary" :icon="Edit" @click="openEditDialog(row)">编辑</el-button>
              <el-button
                v-if="row.id === currentUser?.id"
                size="small"
                type="warning"
                :icon="Key"
                @click="openChangePasswordDialog(row)"
              >修改密码</el-button>
              <el-button
                v-if="row.user_type === 'user'"
                size="small"
                type="danger"
                :icon="Delete"
                @click="handleDelete(row)"
              >删除</el-button>
            </template>
            <!-- 超级管理员可以编辑自己，可以修改自己的密码 -->
            <template v-else-if="currentUser?.user_type === 'super_admin'">
              <el-button
                v-if="row.id === currentUser?.id"
                size="small"
                type="primary"
                :icon="Edit"
                @click="openEditDialog(row)"
              >编辑</el-button>
              <el-button
                v-if="row.id === currentUser?.id"
                size="small"
                type="warning"
                :icon="Key"
                @click="openChangePasswordDialog(row)"
              >修改密码</el-button>
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

        <!-- 密码字段只在创建新用户时显示 -->
        <el-form-item label="密码" prop="password" v-if="!isEdit">
          <el-input
            v-model="userForm.password"
            type="password"
            show-password
            placeholder="请输入密码"
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

    <!-- 修改密码对话框 -->
    <el-dialog
      v-model="passwordDialogVisible"
      title="修改密码"
      width="500px"
      @close="resetPasswordForm"
    >
      <el-form
        ref="passwordFormRef"
        :model="passwordForm"
        :rules="passwordRules"
        label-width="100px"
      >
        <el-form-item label="旧密码" prop="old_password">
          <el-input
            v-model="passwordForm.old_password"
            type="password"
            show-password
            placeholder="请输入旧密码"
          />
        </el-form-item>

        <el-form-item label="新密码" prop="new_password">
          <el-input
            v-model="passwordForm.new_password"
            type="password"
            show-password
            placeholder="请输入新密码（至少6位）"
          />
        </el-form-item>

        <el-form-item label="确认密码" prop="confirm_password">
          <el-input
            v-model="passwordForm.confirm_password"
            type="password"
            show-password
            placeholder="请再次输入新密码"
          />
        </el-form-item>
      </el-form>

      <template #footer>
        <span class="dialog-footer">
          <el-button @click="passwordDialogVisible = false">取消</el-button>
          <el-button type="primary" @click="handleChangePassword" :loading="passwordSubmitLoading">
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
import { Plus, Edit, Delete, Key } from '@element-plus/icons-vue'
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

// 密码修改对话框相关
const passwordDialogVisible = ref(false)
const passwordSubmitLoading = ref(false)
const passwordFormRef = ref(null)
const changingUserId = ref(null)

const passwordForm = reactive({
  old_password: '',
  new_password: '',
  confirm_password: ''
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

const passwordRules = {
  old_password: [
    { required: true, message: '请输入旧密码', trigger: 'blur' }
  ],
  new_password: [
    { required: true, message: '请输入新密码', trigger: 'blur' },
    { min: 6, message: '密码长度不能少于 6 位', trigger: 'blur' }
  ],
  confirm_password: [
    { required: true, message: '请再次输入新密码', trigger: 'blur' },
    {
      validator: (rule, value, callback) => {
        if (value !== passwordForm.new_password) {
          callback(new Error('两次输入的密码不一致'))
        } else {
          callback()
        }
      },
      trigger: 'blur'
    }
  ]
}

const formatDate = (dateString) => {
  return new Date(dateString).toLocaleString('zh-CN')
}

const loadUsers = async () => {
  loading.value = true
  try {
    const response = await usersAPI.list(currentPage.value, pageSize.value)
    users.value = response
    total.value = response.length
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
            full_name: userForm.full_name || null
          }

          // 只有租户管理员编辑其他用户时，才提交 is_active 和 user_type
          const isEditingOthers = editingUserId.value !== currentUser.value?.id
          const isTenantAdmin = currentUser.value?.user_type === 'tenant_admin'

          if (isEditingOthers && isTenantAdmin) {
            data.is_active = userForm.is_active
            data.user_type = userForm.user_type
          }

          // 密码字段条件化提交
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

// 打开修改密码对话框
const openChangePasswordDialog = (row) => {
  changingUserId.value = row.id
  resetPasswordForm()
  passwordDialogVisible.value = true
}

// 重置密码表单
const resetPasswordForm = () => {
  passwordForm.old_password = ''
  passwordForm.new_password = ''
  passwordForm.confirm_password = ''
  if (passwordFormRef.value) {
    nextTick(() => {
      passwordFormRef.value?.clearValidate()
    })
  }
}

// 修改密码
const handleChangePassword = async () => {
  if (!passwordFormRef.value) return

  await passwordFormRef.value.validate(async (valid) => {
    if (valid) {
      passwordSubmitLoading.value = true
      try {
        await usersAPI.changePassword(changingUserId.value, {
          old_password: passwordForm.old_password,
          new_password: passwordForm.new_password
        })
        ElMessage.success('密码修改成功')
        passwordDialogVisible.value = false
        resetPasswordForm()
      } catch (error) {
        console.error('修改密码失败:', error)
        ElMessage.error(error.response?.data?.error || '修改密码失败')
      } finally {
        passwordSubmitLoading.value = false
      }
    }
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