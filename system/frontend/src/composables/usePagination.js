import { ref } from 'vue'

/**
 * 分页 Composable
 * 消除 5+ 处重复的分页状态管理
 * @param {Object} options 配置选项
 * @param {number} options.defaultPage 默认页码
 * @param {number} options.defaultPageSize 默认每页数量
 * @returns {Object} 分页状态和方法
 */
export function usePagination(options = {}) {
  const currentPage = ref(options.defaultPage || 1)
  const pageSize = ref(options.defaultPageSize || 10)
  const total = ref(0)

  /**
   * 重置分页到初始状态
   */
  const resetPagination = () => {
    currentPage.value = options.defaultPage || 1
    total.value = 0
  }

  /**
   * 设置总数
   */
  const setTotal = (newTotal) => {
    total.value = newTotal
  }

  /**
   * 页码改变处理
   */
  const handlePageChange = (page) => {
    currentPage.value = page
  }

  /**
   * 每页数量改变处理
   */
  const handleSizeChange = (size) => {
    pageSize.value = size
    currentPage.value = 1 // 重置到第一页
  }

  return {
    currentPage,
    pageSize,
    total,
    resetPagination,
    setTotal,
    handlePageChange,
    handleSizeChange
  }
}
