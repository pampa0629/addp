import { ref, reactive, computed, nextTick } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { usersAPI } from '../api/users'

/**
 * 用户管理 Composable
 * 封装用户 CRUD 逻辑,消除重复代码
 */
export function useUserManagement() {
  const users = ref([])
  const loading = ref(false)

  // 用户类型映射
  const userTypeMap = {
    'super_admin': '超级管理员',
    'tenant_admin': '租户管理员',
    'user': '普通用户'
  }

  const userTypeTagMap = {
    'super_admin': 'danger',
    'tenant_admin': 'warning',
    'user': 'info'
  }

  /**
   * 获取用户类型显示文本
   */
  const getUserTypeText = (userType) => {
    return userTypeMap[userType] || '未知'
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
      // 后端返回分页数据格式: { data: [], total: 123, page: 1, page_size: 10 }
      users.value = response?.data || []
      return { success: true, data: response?.data || [], total: response?.total || 0 }
    } catch (error) {
      ElMessage.error('加载用户列表失败')
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
      ElMessage.success('新增用户成功')
      return { success: true }
    } catch (error) {
      ElMessage.error(error.response?.data?.error || '新增用户失败')
      return { success: false, error }
    }
  }

  /**
   * 更新用户
   */
  const updateUser = async (userId, userData) => {
    try {
      await usersAPI.update(userId, userData)
      ElMessage.success('更新用户成功')
      return { success: true }
    } catch (error) {
      ElMessage.error(error.response?.data?.error || '更新用户失败')
      return { success: false, error }
    }
  }

  /**
   * 删除用户（带确认）
   */
  const deleteUser = async (user) => {
    // 检查是否为admin用户
    if (user.username === 'admin') {
      ElMessage.error('admin账号不能被删除')
      return { success: false }
    }

    try {
      await ElMessageBox.confirm(
        `确定要删除用户 "${user.username}" 吗？`,
        '警告',
        {
          confirmButtonText: '确定',
          cancelButtonText: '取消',
          type: 'warning',
        }
      )

      await usersAPI.delete(user.id)
      ElMessage.success('删除成功')
      return { success: true }
    } catch (error) {
      // 用户取消或API错误
      if (error !== 'cancel') {
        ElMessage.error(error.response?.data?.error || '删除失败')
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
      ElMessage.success('密码修改成功')
      return { success: true }
    } catch (error) {
      console.error('修改密码失败:', error)
      ElMessage.error(error.response?.data?.error || '修改密码失败')
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
  const passwordFormRef = ref(null)

  const passwordForm = reactive({
    old_password: '',
    new_password: '',
    confirm_password: ''
  })

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
