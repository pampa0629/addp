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

      <UserList
        :users="users"
        :loading="loading"
        :current-page="currentPage"
        :page-size="pageSize"
        :total="total"
        :current-user="currentUser"
        :get-user-type-text="getUserTypeText"
        :get-user-type-tag="getUserTypeTag"
        @edit="handleEdit"
        @delete="handleDelete"
        @change-password="handleOpenPasswordDialog"
        @page-change="handlePageChange"
      />
    </el-card>

    <!-- 新增/编辑用户对话框 -->
    <UserFormDialog
      v-model:visible="dialogVisible"
      :is-edit="isEdit"
      :editing-user-id="editingUserId"
      :user-form="userForm"
      :rules="rules"
      :form-ref="formRef"
      :submit-loading="submitLoading"
      :current-user="currentUser"
      @submit="handleSubmit"
    />

    <!-- 修改密码对话框 -->
    <PasswordDialog
      v-model:visible="passwordDialogVisible"
      :password-form="passwordForm"
      :password-rules="passwordRules"
      :password-form-ref="passwordFormRef"
      :submit-loading="passwordSubmitLoading"
      @submit="handleChangePassword"
    />
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { Plus } from '@element-plus/icons-vue'
import { useAuthStore } from '../store/auth'
import { usePagination } from '../composables/usePagination'
import { useFormDialog } from '../composables/useFormDialog'
import { useUserManagement, useUserForm, usePasswordForm } from '../composables/useUserManagement'
import Tenants from './Tenants.vue'
import UserList from '../components/users/UserList.vue'
import UserFormDialog from '../components/users/UserFormDialog.vue'
import PasswordDialog from '../components/users/PasswordDialog.vue'

const authStore = useAuthStore()
const currentUser = computed(() => authStore.user)

// 分页
const { currentPage, pageSize, total } = usePagination()

// 用户管理
const { users, loading, getUserTypeText, getUserTypeTag, loadUsers, createUser, updateUser, deleteUser, changePassword } = useUserManagement()

// 用户表单
const { formRef, isEdit, userForm, rules, resetForm, setFormData, clearValidation } = useUserForm()

// 密码表单
const { passwordFormRef, passwordForm, passwordRules, resetPasswordForm } = usePasswordForm()

// 对话框状态
const { visible: dialogVisible, isEdit: _, editingItem: editingUserId, submitLoading, openAddDialog: openAdd, openEditDialog: openEdit, closeDialog } = useFormDialog()

const passwordDialogVisible = ref(false)
const passwordSubmitLoading = ref(false)
const changingUserId = ref(null)

// 打开新增对话框
const openAddDialog = () => {
  resetForm()
  openAdd()
  clearValidation()
}

// 处理编辑
const handleEdit = (row) => {
  setFormData(row)
  openEdit(row.id)
  isEdit.value = true
}

// 处理删除
const handleDelete = async (row) => {
  const result = await deleteUser(row)
  if (result.success) {
    await loadUsersData()
  }
}

// 打开修改密码对话框
const handleOpenPasswordDialog = (row) => {
  changingUserId.value = row.id
  resetPasswordForm()
  passwordDialogVisible.value = true
}

// 处理表单提交
const handleSubmit = async () => {
  submitLoading.value = true
  try {
    let result
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

      result = await updateUser(editingUserId.value, data)
    } else {
      // 新增用户
      result = await createUser({
        username: userForm.username,
        password: userForm.password,
        email: userForm.email,
        full_name: userForm.full_name,
        user_type: userForm.user_type
      })
    }

    if (result.success) {
      dialogVisible.value = false
      await loadUsersData()
    }
  } finally {
    submitLoading.value = false
  }
}

// 处理密码修改
const handleChangePassword = async () => {
  passwordSubmitLoading.value = true
  try {
    const result = await changePassword(changingUserId.value, {
      old_password: passwordForm.old_password,
      new_password: passwordForm.new_password
    })

    if (result.success) {
      passwordDialogVisible.value = false
      resetPasswordForm()
    }
  } finally {
    passwordSubmitLoading.value = false
  }
}

// 处理分页变化
const handlePageChange = async () => {
  await loadUsersData()
}

// 加载用户数据
const loadUsersData = async () => {
  const result = await loadUsers(currentPage.value, pageSize.value)
  if (result.success) {
    total.value = result.data.length
  }
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
  await loadUsersData()
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
