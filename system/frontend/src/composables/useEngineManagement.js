import { ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { enginesAPI } from '../api/engines'
import { useI18n } from 'vue-i18n'

/**
 * 引擎管理 Composable
 * 封装引擎 CRUD 逻辑,消除重复代码
 */
export function useEngineManagement() {
  const { t } = useI18n()
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
  const getConnectionStatusMap = () => ({
    'online': { text: t('system.engine.connection.online'), type: 'success' },
    'offline': { text: t('system.engine.connection.offline'), type: 'danger' },
    'unknown': { text: t('system.engine.connection.unknown'), type: 'info' },
    'checking': { text: t('system.engine.connection.checking'), type: 'warning' }
  })

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
    return getConnectionStatusMap()[status] || { text: status, type: 'info' }
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
      ElMessage.error(t('system.engine.msg.loadFailed'))
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
      ElMessage.success(t('system.engine.msg.createSuccess'))
      return { success: true }
    } catch (error) {
      ElMessage.error(error.response?.data?.error || t('system.engine.msg.opFailed'))
      return { success: false, error }
    }
  }

  /**
   * 更新引擎
   */
  const updateEngine = async (engineId, engineData) => {
    try {
      await enginesAPI.update(engineId, engineData)
      ElMessage.success(t('system.engine.msg.updateSuccess'))
      return { success: true }
    } catch (error) {
      ElMessage.error(error.response?.data?.error || t('system.engine.msg.opFailed'))
      return { success: false, error }
    }
  }

  /**
   * 删除引擎（带确认）
   */
  const deleteEngine = async (engine) => {
    try {
      await ElMessageBox.confirm(
        t('system.engine.msg.deleteConfirm', { name: engine.name }),
        t('system.engine.msg.deleteTitle'),
        {
          confirmButtonText: 'OK',
          cancelButtonText: 'Cancel',
          type: 'warning',
        }
      )

      await enginesAPI.delete(engine.id)
      ElMessage.success(t('system.engine.msg.deleteSuccess'))
      return { success: true }
    } catch (error) {
      if (error !== 'cancel') {
        ElMessage.error(error.response?.data?.error || t('system.engine.msg.opFailed'))
      }
      return { success: false, error }
    }
  }

  /**
   * 测试引擎连接
   */
  const testConnection = async (engineId) => {
    try {
      const response = await enginesAPI.testExistingConnection(engineId)
      if (response.success) {
        ElMessage.success(t('system.engine.msg.testSuccess'))
      } else {
        ElMessage.error(t('system.engine.msg.testFailed', { error: response.error || 'Unknown error' }))
      }
      return response
    } catch (error) {
      ElMessage.error(t('system.engine.msg.testFailed', { error: error.response?.data?.error || error.message }))
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
        ElMessage.success(t('system.engine.msg.testSuccess'))
      } else {
        ElMessage.error(t('system.engine.msg.testFailed', { error: response.error || 'Unknown error' }))
      }
      return response
    } catch (error) {
      ElMessage.error(t('system.engine.msg.testFailed', { error: error.response?.data?.error || error.message }))
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
