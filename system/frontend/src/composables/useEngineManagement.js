import { ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { enginesAPI } from '../api/engines'

/**
 * 引擎管理 Composable
 * 封装引擎 CRUD 逻辑,消除重复代码
 */
export function useEngineManagement() {
  const engines = ref([])
  const loading = ref(false)

  // 引擎类型映射
  const engineTypeMap = {
    'postgresql': 'PostgreSQL',
    'mysql': 'MySQL',
    'doris': 'Doris',
    'clickhouse': 'ClickHouse',
    'mongodb': 'MongoDB',
    'spark_sql': 'Spark SQL',
    'minio': 'MinIO',
    's3': 'S3'
  }

  // 引擎类别映射
  const engineCategoryMap = {
    'postgresql': 'storage',
    'mysql': 'storage',
    'doris': 'storage',
    'clickhouse': 'storage',
    'mongodb': 'storage',
    'minio': 'storage',
    's3': 'storage',
    'spark_sql': 'compute'
  }

  // 连接状态映射
  const connectionStatusMap = {
    'online': { text: '在线', type: 'success' },
    'offline': { text: '离线', type: 'danger' },
    'unknown': { text: '未知', type: 'info' },
    'checking': { text: '检测中', type: 'warning' }
  }

  /**
   * 获取引擎类型显示文本
   */
  const getEngineTypeText = (engineType) => {
    return engineTypeMap[engineType] || engineType
  }

  /**
   * 获取引擎类别 (storage/compute)
   */
  const getEngineCategory = (engineType) => {
    return engineCategoryMap[engineType] || 'storage'
  }

  /**
   * 获取连接状态显示信息
   */
  const getConnectionStatus = (status) => {
    return connectionStatusMap[status] || { text: status, type: 'info' }
  }

  /**
   * 加载引擎列表
   */
  const loadEngines = async (page, pageSize, engineType = '') => {
    loading.value = true
    try {
      const response = await enginesAPI.list(page, pageSize, engineType)
      engines.value = response.engines || []
      return { success: true, data: response }
    } catch (error) {
      ElMessage.error('加载引擎列表失败')
      console.error(error)
      return { success: false, error }
    } finally {
      loading.value = false
    }
  }

  /**
   * 创建引擎
   */
  const createEngine = async (engineData) => {
    try {
      await enginesAPI.create(engineData)
      ElMessage.success('创建引擎成功')
      return { success: true }
    } catch (error) {
      ElMessage.error(error.response?.data?.error || '创建引擎失败')
      return { success: false, error }
    }
  }

  /**
   * 更新引擎
   */
  const updateEngine = async (engineId, engineData) => {
    try {
      await enginesAPI.update(engineId, engineData)
      ElMessage.success('更新引擎成功')
      return { success: true }
    } catch (error) {
      ElMessage.error(error.response?.data?.error || '更新引擎失败')
      return { success: false, error }
    }
  }

  /**
   * 删除引擎（带确认）
   */
  const deleteEngine = async (engine) => {
    try {
      await ElMessageBox.confirm(
        `确定要删除引擎 "${engine.name}" 吗？`,
        '警告',
        {
          confirmButtonText: '确定',
          cancelButtonText: '取消',
          type: 'warning',
        }
      )

      await enginesAPI.delete(engine.id)
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
   * 测试引擎连接
   */
  const testConnection = async (engineId) => {
    try {
      const response = await enginesAPI.testConnection(engineId)
      if (response.success) {
        ElMessage.success('连接测试成功')
      } else {
        ElMessage.error(`连接测试失败: ${response.error || '未知错误'}`)
      }
      return response
    } catch (error) {
      ElMessage.error(error.response?.data?.error || '连接测试失败')
      return { success: false, error }
    }
  }

  /**
   * 测试连接(创建前)
   */
  const testConnectionBeforeCreate = async (engineData) => {
    try {
      const response = await enginesAPI.testConnectionBeforeCreate(engineData)
      if (response.success) {
        ElMessage.success('连接测试成功')
      } else {
        ElMessage.error(`连接测试失败: ${response.error || '未知错误'}`)
      }
      return response
    } catch (error) {
      ElMessage.error(error.response?.data?.error || '连接测试失败')
      return { success: false, error }
    }
  }

  return {
    engines,
    loading,
    getEngineTypeText,
    getEngineCategory,
    getConnectionStatus,
    loadEngines,
    createEngine,
    updateEngine,
    deleteEngine,
    testConnection,
    testConnectionBeforeCreate
  }
}

/**
 * 引擎过滤 Composable
 * 管理引擎列表的过滤状态
 */
export function useEngineFilter() {
  const selectedEngineType = ref('')
  const selectedCategory = ref('')

  /**
   * 重置过滤条件
   */
  const resetFilters = () => {
    selectedEngineType.value = ''
    selectedCategory.value = ''
  }

  /**
   * 应用过滤(返回过滤后的引擎类型)
   */
  const getFilteredEngineType = () => {
    return selectedEngineType.value || ''
  }

  return {
    selectedEngineType,
    selectedCategory,
    resetFilters,
    getFilteredEngineType
  }
}
