import { ref, reactive, computed, nextTick } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { usersAPI } from '../api/users'
import { useI18n } from 'vue-i18n'

/**
 * 用户管理 Composable
 * 封装用户 CRUD 逻辑,消除重复代码
 */
export function useUserManagement() {
  const { t } = useI18n()
  const users = ref([])
  const loading = ref(false)

  const userTypeTagMap = {
    'super_admin': 'danger',
    'tenant_admin': 'warning',
    'user': 'info'
  }

  /**
   * 获取用户类型显示文本
   */
  const getUserTypeText = (userType) => {
    const map = {
      'super_admin': t('system.user.types.superAdmin'),
      'tenant_admin': t('system.user.types.tenantAdmin'),
      'user': t('system.user.types.user')
    }
    return map[userType] || t('system.user.types.unknown')
  }

  /**
   * 获取用户类型标签颜色
   */
  const getUserTypeTag = (userType) => {
    return userTypeTagMap[userType] || 'info'
  }

  /**
   * 加载用户列表
   */
  const loadUsers = async (page, pageSize) => {
    loading.value = true
    try {
      const response = await usersAPI.list(page, pageSize)
      users.value = response?.data || []
      return { success: true, data: response?.data || [], total: response?.total || 0 }
    } catch (error) {
      ElMessage.error(t('system.user.msg.loadFailed'))
      console.error(error)
      return { success: false, error }
    } finally {
      loading.value = false
    }
  }

  /**
   * 创建用户
   */
  const createUser = async (userData) => {
    try {
      await usersAPI.create(userData)
      ElMessage.success(t('system.user.msg.createSuccess'))
      return { success: true }
    } catch (error) {
      ElMessage.error(error.response?.data?.error || t('system.user.msg.createFailed'))
      return { success: false, error }
    }
  }

  /**
   * 更新用户
   */
  const updateUser = async (userId, userData) => {
    try {
      await usersAPI.update(userId, userData)
      ElMessage.success(t('system.user.msg.updateSuccess'))
      return { success: true }
    } catch (error) {
      ElMessage.error(error.response?.data?.error || t('system.user.msg.updateFailed'))
      return { success: false, error }
    }
  }

  /**
   * 删除用户（带确认）
   */
  const deleteUser = async (user) => {
    if (user.username === 'admin') {
      ElMessage.error(t('system.user.msg.adminCannotDelete'))
      return { success: false }
    }

    try {
      await ElMessageBox.confirm(
        t('system.user.msg.deleteConfirm', { username: user.username }),
        t('system.user.msg.deleteWarning'),
        {
          confirmButtonText: 'OK',
          cancelButtonText: 'Cancel',
          type: 'warning',
        }
      )

      await usersAPI.delete(user.id)
      ElMessage.success(t('system.user.msg.deleteSuccess'))
      return { success: true }
    } catch (error) {
      if (error !== 'cancel') {
        ElMessage.error(error.response?.data?.error || t('system.user.msg.deleteFailed'))
      }
      return { success: false, error }
    }
  }

  /**
   * 修改密码
   */
  const changePassword = async (userId, passwordData) => {
    try {
      await usersAPI.changePassword(userId, passwordData)
      ElMessage.success(t('system.user.msg.changePasswordSuccess'))
      return { success: true }
    } catch (error) {
      console.error('Change password failed:', error)
      ElMessage.error(error.response?.data?.error || t('system.user.msg.changePasswordFailed'))
      return { success: false, error }
    }
  }

  return {
    users,
    loading,
    getUserTypeText,
    getUserTypeTag,
    loadUsers,
    createUser,
    updateUser,
    deleteUser,
    changePassword
  }
}

/**
 * 用户表单 Composable
 * 管理用户表单状态和验证规则
 */
export function useUserForm() {
  const { t } = useI18n()
  const formRef = ref(null)
  const isEdit = ref(false)

  const userForm = reactive({
    username: '',
    password: '',
    email: '',
    full_name: '',
    is_active: true,
    user_type: 'user'
  })

  const rules = computed(() => ({
    username: [
      { required: true, message: t('system.user.rules.usernameRequired'), trigger: 'blur' },
      { min: 3, max: 20, message: t('system.user.rules.usernameLength'), trigger: 'blur' }
    ],
    password: [
      {
        required: !isEdit.value,
        message: t('system.user.rules.passwordRequired'),
        trigger: 'blur'
      },
      { min: 6, message: t('system.user.rules.passwordMin'), trigger: 'blur' }
    ],
    email: [
      { type: 'email', message: t('system.user.rules.emailFormat'), trigger: 'blur' }
    ]
  }))

  const resetForm = () => {
    userForm.username = ''
    userForm.password = ''
    userForm.email = ''
    userForm.full_name = ''
    userForm.is_active = true
    userForm.user_type = 'user'
    formRef.value?.resetFields()
  }

  const setFormData = (user) => {
    userForm.username = user.username
    userForm.email = user.email
    userForm.full_name = user.full_name
    userForm.is_active = user.is_active
    userForm.user_type = user.user_type || 'user'
  }

  const clearValidation = () => {
    nextTick(() => {
      formRef.value?.clearValidate()
    })
  }

  return {
    formRef,
    isEdit,
    userForm,
    rules,
    resetForm,
    setFormData,
    clearValidation
  }
}

/**
 * 密码修改表单 Composable
 */
export function usePasswordForm() {
  const { t } = useI18n()
  const passwordFormRef = ref(null)

  const passwordForm = reactive({
    old_password: '',
    new_password: '',
    confirm_password: ''
  })

  const passwordRules = computed(() => ({
    old_password: [
      { required: true, message: t('system.password.rules.oldRequired'), trigger: 'blur' }
    ],
    new_password: [
      { required: true, message: t('system.password.rules.newRequired'), trigger: 'blur' },
      { min: 6, message: t('system.password.rules.newMin'), trigger: 'blur' }
    ],
    confirm_password: [
      { required: true, message: t('system.password.rules.confirmRequired'), trigger: 'blur' },
      {
        validator: (rule, value, callback) => {
          if (value !== passwordForm.new_password) {
            callback(new Error(t('system.password.rules.confirmMismatch')))
          } else {
            callback()
          }
        },
        trigger: 'blur'
      }
    ]
  }))

  const resetPasswordForm = () => {
    passwordForm.old_password = ''
    passwordForm.new_password = ''
    passwordForm.confirm_password = ''
    nextTick(() => {
      passwordFormRef.value?.clearValidate()
    })
  }

  return {
    passwordFormRef,
    passwordForm,
    passwordRules,
    resetPasswordForm
  }
}
