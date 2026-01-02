import { ref } from 'vue'

/**
 * 表单对话框 Composable
 * 消除 6+ 处重复的对话框状态管理
 * @returns {Object} 对话框状态和方法
 */
export function useFormDialog() {
  const visible = ref(false)
  const isEdit = ref(false)
  const editingItem = ref(null)
  const submitLoading = ref(false)

  /**
   * 打开添加对话框
   */
  const openAddDialog = (defaultValues = {}) => {
    visible.value = true
    isEdit.value = false
    editingItem.value = { ...defaultValues }
  }

  /**
   * 打开编辑对话框
   */
  const openEditDialog = (item) => {
    visible.value = true
    isEdit.value = true
    editingItem.value = { ...item }
  }

  /**
   * 关闭对话框
   */
  const closeDialog = () => {
    visible.value = false
    isEdit.value = false
    editingItem.value = null
    submitLoading.value = false
  }

  /**
   * 设置提交加载状态
   */
  const setSubmitLoading = (loading) => {
    submitLoading.value = loading
  }

  return {
    visible,
    isEdit,
    editingItem,
    submitLoading,
    openAddDialog,
    openEditDialog,
    closeDialog,
    setSubmitLoading
  }
}
