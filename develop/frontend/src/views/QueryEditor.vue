<template>
  <div class="query-editor-page">
    <!-- 顶部工具栏 -->
    <div class="toolbar">
      <div class="toolbar-left">
        <h2>{{ t('develop.query.title') }}</h2>

        <!-- 数据源选择 -->
        <el-select
          v-model="selectedQueryTarget"
          :placeholder="t('develop.query.selectDataSource')"
          style="width: 280px; margin-left: 20px;"
          @change="onQueryTargetChange"
        >
          <el-option
            v-for="target in queryTargets"
            :key="target.value"
            :label="target.label"
            :value="target.value"
          >
            <span style="float: left">{{ target.name }}</span>
            <span style="float: right; color: var(--addp-text-tertiary); font-size: 13px">
              {{ target.typeLabel }}
            </span>
          </el-option>
        </el-select>

        <el-button
          type="primary"
          :loading="testingConnection"
          @click="handleTestConnection"
          :disabled="!selectedQueryTarget"
        >
          <el-icon><Connection /></el-icon>
          {{ t('develop.query.testConnection') }}
        </el-button>
      </div>

      <div class="toolbar-right">
        <el-button @click="formatSQL" :disabled="loadingSampleQuery || !queryContent">
          <el-icon><MagicStick /></el-icon>
          {{ t('develop.query.format') }}
        </el-button>

        <el-button
          type="primary"
          @click="handlePersistQueryTask"
          :disabled="loadingSampleQuery || !selectedQueryTarget || !queryContent"
        >
          <el-icon><FolderAdd /></el-icon>
          {{ currentTaskId ? t('develop.query.updateTask') : t('develop.query.saveAsTask') }}
        </el-button>

        <el-button
          type="success"
          @click="executeQuery"
          :loading="executing"
          :disabled="loadingSampleQuery || !selectedQueryTarget || !queryContent"
        >
          <el-icon><VideoPlay /></el-icon>
          {{ t('develop.query.execute') }}
        </el-button>
      </div>
    </div>

    <!-- 编辑器和结果分栏 -->
    <div class="content-area">
      <!-- 查询编辑器 -->
      <div class="editor-panel">
        <div class="panel-header">
          <span class="panel-title">
            <el-icon><Edit /></el-icon>
            {{ t('develop.query.editorTitle') }}
          </span>
          <span class="hint">{{ t('develop.query.hint') }}</span>
        </div>
        <div
          v-loading="loadingSampleQuery"
          class="editor-content"
          :aria-busy="loadingSampleQuery"
        >
          <MonacoEditor
            ref="editorRef"
            v-model="queryContent"
            language="sql"
            theme="vs-dark"
            @execute="executeQuery"
          />
        </div>
      </div>

      <!-- 分割条 -->
      <div class="divider"></div>

      <!-- 结果面板 -->
      <div class="result-panel">
        <div class="panel-header">
          <span class="panel-title">
            <el-icon><List /></el-icon>
            {{ t('develop.query.resultTitle') }}
          </span>
          <div style="display:flex;align-items:center;gap:8px">
            <!-- 图形/表格切换（仅当查询结果含图数据时显示） -->
            <el-radio-group
              v-if="hasGraphData"
              v-model="resultViewMode"
              size="small"
            >
              <el-radio-button value="table">{{ t('develop.query.tableView') }}</el-radio-button>
              <el-radio-button value="graph">{{ t('develop.query.graphView') }}</el-radio-button>
            </el-radio-group>
            <el-button
              v-if="executionResult"
              text
              size="small"
              @click="clearResult"
            >
              <el-icon><Close /></el-icon>
              {{ t('develop.query.clearResult') }}
            </el-button>
          </div>
        </div>
        <div class="result-content">
          <GraphResultView
            v-if="resultViewMode === 'graph' && hasGraphData"
            :graph-data="executionResult.graph_data"
          />
          <QueryResult v-else :result="executionResult" />
        </div>
      </div>
    </div>

    <!-- 保存任务对话框 -->
    <SaveQueryDialog
      v-model="showSaveDialog"
      :engine-id="selectedEngineId"
      :sql="queryContent"
      @saved="handleSaveTask"
    />
  </div>
</template>

<script setup>
import { ref, computed, onMounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElLoading } from 'element-plus'
import {
  Connection,
  MagicStick,
  VideoPlay,
  Edit,
  List,
  Close,
  FolderAdd
} from '@element-plus/icons-vue'
import { format } from 'sql-formatter'
import MonacoEditor from '../components/MonacoEditor.vue'
import QueryResult from '../components/QueryResult.vue'
import SaveQueryDialog from '../components/SaveQueryDialog.vue'
import { GraphResultView } from '@addp/common-frontend/graph'
import { executeQuery as executeAPI, testConnection, saveQueryTask, updateQueryTask, getSampleQuery } from '../api/query.js'
import { listEngines } from '../api/engines.js'
import { getDevTask } from '../api/devTask.js'
import {
  buildDevelopTaskEditorLocation,
  developTaskIDFromRoute
} from '@/utils/developTaskRoute'
import { navigateDevelopTaskEditor } from '@/utils/developNavigation'
import { createLatestRequestCoordinator } from '@common-ui'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()

// 引擎类型 → 编辑器语言
const ENGINE_LANGUAGE_MAP = {
  postgresql: 'sql',
  mysql: 'sql',
  doris: 'sql',
  clickhouse: 'sql',
  spark: 'sql',
  duckdb: 'sql',
  mongodb: 'json',
  neo4j: 'cypher'
}

// 状态
const currentTaskId = ref(null)
const currentTaskName = ref('')
const currentTask = ref(null)
const selectedQueryTarget = ref('')
const engines = ref([])
const queryContent = ref('')
const currentQueryLanguage = ref('sql')
const executionResult = ref(null)
const executing = ref(false)
const testingConnection = ref(false)
const loadingSampleQuery = ref(false)
const editorRef = ref(null)
const showSaveDialog = ref(false)
const resultViewMode = ref('table') // 'table' | 'graph'

// 是否有图形数据（用于显示图形/表格切换）
const hasGraphData = computed(
  () => executionResult.value?.graph_data?.nodes?.length > 0
)

const queryTargets = computed(() => [
  ...engines.value.map(engine => ({
    kind: 'engine',
    value: `engine:${engine.id}`,
    name: engine.name,
    label: `${engine.name} (${engine.engine_type})`,
    typeLabel: engine.engine_type,
    engine
  }))
])

const selectedTarget = computed(() => queryTargets.value.find(target => target.value === selectedQueryTarget.value) || null)
const selectedEngineId = computed(() => selectedTarget.value?.engine.id || null)
const sampleRequests = createLatestRequestCoordinator()

// 加载数据源列表
const loadEngines = async () => {
  try {
    const engineResponse = await listEngines()

    // 添加 null 安全检查
    if (!Array.isArray(engineResponse)) {
      console.warn('Develop: 获取到的查询目标列表为空或格式不正确:', engineResponse)
      engines.value = []
      return
    }

    engines.value = engineResponse

    // 默认选择第一个
    if (queryTargets.value.length > 0 && !selectedQueryTarget.value) {
      selectedQueryTarget.value = queryTargets.value[0].value
      await applyQueryTarget(queryTargets.value[0])
    }
  } catch (error) {
    console.error('Develop: 加载查询目标失败:', error)
    ElMessage.error(t('develop.query.loadEnginesFailed') + (error.response?.data?.error || error.message))
    engines.value = []
  }
}

// 测试连接
const handleTestConnection = async () => {
  if (!selectedTarget.value) return

  testingConnection.value = true
  try {
    await testConnection(selectedEngineId.value)
    ElMessage.success(t('develop.query.testConnectionSuccess'))
  } catch (error) {
    ElMessage.error(t('develop.query.testConnectionFailed') + (error.response?.data?.error || error.message))
  } finally {
    testingConnection.value = false
  }
}

// 执行查询
const executeQuery = async () => {
  if (loadingSampleQuery.value) return

  if (!selectedTarget.value) {
    ElMessage.warning(t('develop.query.selectDataSourceFirst'))
    return
  }

  if (!queryContent.value.trim()) {
    ElMessage.warning(t('develop.query.enterQueryFirst'))
    return
  }

  executing.value = true
  const loadingInstance = ElLoading.service({
    lock: true,
    text: t('develop.query.executing'),
    background: 'rgba(0, 0, 0, 0.7)'
  })

  try {
    const response = await executeAPI(
      selectedEngineId.value,
      queryContent.value.trim(),
      currentQueryLanguage.value,
      120
    )

    executionResult.value = {
      success: true,
      columns: response.columns || [],
      rows: response.rows || [],
      rows_count: response.rows_count,
      rows_affected: response.rows_affected,
      execution_time_ms: response.execution_time_ms,
      graph_data: response.graph_data || null
    }

    // 有图数据时自动切换到图形视图
    if (response.graph_data?.nodes?.length > 0) {
      resultViewMode.value = 'graph'
    } else {
      resultViewMode.value = 'table'
    }

    ElMessage.success(t('develop.query.executeSuccess'))
  } catch (error) {
    const responseError = error.response?.data
    const errorMessage = responseError?.details || responseError?.detail || responseError?.error || error.message
    executionResult.value = {
      success: false,
      error: errorMessage
    }
    ElMessage.error(t('develop.query.executeFailed'))
  } finally {
    executing.value = false
    loadingInstance.close()
  }
}

// 格式化 SQL
const formatSQL = () => {
  if (loadingSampleQuery.value || !queryContent.value) return

  try {
    const formatted = format(queryContent.value, {
      language: 'postgresql',
      indent: '  ',
      uppercase: true,
      linesBetweenQueries: 2
    })
    queryContent.value = formatted
    ElMessage.success(t('develop.query.formatSuccess'))
  } catch (error) {
    ElMessage.error(t('develop.query.formatFailed') + error.message)
  }
}

// 清空结果
const clearResult = () => {
  executionResult.value = null
  resultViewMode.value = 'table'
}

// 查询目标切换
const onQueryTargetChange = (targetValue) => {
  const target = queryTargets.value.find(item => item.value === targetValue)
  if (target) applyQueryTarget(target)
}

// 根据查询目标切换编辑器语言，并自动拉取样例查询填充编辑器
const applyQueryTarget = async (target, options = {}) => {
  const { refreshQuery = true } = options
  const type = target.engine.engine_type?.toLowerCase() || ''
  const lang = ENGINE_LANGUAGE_MAP[type] || 'sql'
  if (!refreshQuery) {
    sampleRequests.invalidate()
    loadingSampleQuery.value = false
    editorRef.value?.setLanguage(currentQueryLanguage.value || lang)
    return
  }

  const request = sampleRequests.begin(target.value)
  queryContent.value = ''
  currentQueryLanguage.value = lang
  clearResult()
  loadingSampleQuery.value = true
  editorRef.value?.setLanguage(lang)

  try {
    const { query, language } = await getSampleQuery(target.engine.id)
    if (!sampleRequests.isCurrent(request, selectedQueryTarget.value)) return
    queryContent.value = query
    currentQueryLanguage.value = language
    editorRef.value?.setLanguage(language)
  } catch (error) {
    if (!sampleRequests.isCurrent(request, selectedQueryTarget.value)) return
    ElMessage.error(error.response?.data?.error || error.message)
  } finally {
    if (sampleRequests.isCurrent(request, selectedQueryTarget.value)) {
      loadingSampleQuery.value = false
    }
  }
}

// 保存任务
const handleSaveTask = async (taskData) => {
  try {
    const task = await saveQueryTask({ ...taskData, query_type: currentQueryLanguage.value })
    currentTaskId.value = task.id
    currentTaskName.value = task.name
    currentTask.value = task
    await navigateDevelopTaskEditor(router, 'query', task.id, { history: 'replace' })
    ElMessage.success(t('develop.query.saveTaskSuccess'))
    showSaveDialog.value = false
  } catch (error) {
    console.error('保存 查询任务失败:', error)
    ElMessage.error(t('develop.query.saveTaskFailed') + (error.response?.data?.error || error.message))
  }
}

const handlePersistQueryTask = async () => {
  if (loadingSampleQuery.value) return

  if (!currentTaskId.value) {
    showSaveDialog.value = true
    return
  }

  try {
    const task = currentTask.value || {}
    const updatedTask = await updateQueryTask(currentTaskId.value, {
      name: task.name || currentTaskName.value,
      display_name: task.display_name || currentTaskName.value,
      engine_id: selectedEngineId.value,
      query: queryContent.value,
      query_type: currentQueryLanguage.value,
      description: task.description,
      tags: task.tags || [],
      timeout: task.timeout
    })
    currentTask.value = updatedTask
    currentTaskName.value = updatedTask.name
    ElMessage.success(t('develop.query.updateTaskSuccess'))
  } catch (error) {
    console.error('更新查询任务失败:', error)
    ElMessage.error(t('develop.query.updateTaskFailed') + (error.response?.data?.error || error.message))
  }
}

// 加载已有任务
const loadTask = async (taskId) => {
  try {
    const task = await getDevTask(taskId)

    // 设置当前任务信息
    currentTaskId.value = task.id
    currentTaskName.value = task.name
    currentTask.value = task

    // 加载 SQL 内容
    if (task.content && task.content.query) {
      queryContent.value = task.content.query
      currentQueryLanguage.value = task.content.query_type || 'sql'

      const engineID = task.execution_config?.engine_id
      if (engineID) {
        selectedQueryTarget.value = `engine:${engineID}`
      }
      if (selectedTarget.value) {
        await applyQueryTarget(selectedTarget.value, { refreshQuery: false })
      }

      ElMessage.success(t('develop.query.taskLoaded', { name: task.name }))
    } else {
      ElMessage.warning(t('develop.query.taskNoSql'))
    }
  } catch (error) {
    console.error('加载任务失败:', error)
    ElMessage.error(t('develop.query.loadTaskFailed') + (error.response?.data?.error || error.message))
  }
}

const queryTaskRouteReady = ref(false)
let applyingQueryTaskRoute = false

async function resetQueryEditorForCreate() {
  if (!currentTaskId.value) return
  currentTaskId.value = null
  currentTaskName.value = ''
  currentTask.value = null
  clearResult()
  if (selectedTarget.value) await applyQueryTarget(selectedTarget.value)
}

async function applyQueryTaskRoute() {
  if (applyingQueryTaskRoute) return
  applyingQueryTaskRoute = true
  try {
    const taskId = developTaskIDFromRoute(route)
    const canonicalLocation = buildDevelopTaskEditorLocation('query', taskId)
    if (route.fullPath !== router.resolve(canonicalLocation).fullPath) {
      await navigateDevelopTaskEditor(router, 'query', taskId, { history: 'replace' })
    }

    if (taskId) {
      if (String(currentTaskId.value || '') !== taskId) await loadTask(taskId)
    } else {
      await resetQueryEditorForCreate()
    }
  } catch (error) {
    ElMessage.error(t('develop.query.loadTaskFailed') + error.message)
  } finally {
    applyingQueryTaskRoute = false
  }
}

// 页面加载
onMounted(async () => {
  await loadEngines()
  await applyQueryTaskRoute()
  queryTaskRouteReady.value = true
})

watch(() => route.fullPath, () => {
  if (queryTaskRouteReady.value) applyQueryTaskRoute()
})
</script>

<style scoped>
.query-editor-page {
  display: flex;
  flex-direction: column;
  height: 100vh;
  background: var(--addp-bg-secondary);
}

.toolbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 12px 20px;
  background: var(--addp-bg-primary);
  border-bottom: 1px solid var(--addp-border-color);
}

.toolbar-left,
.toolbar-right {
  display: flex;
  align-items: center;
  gap: 12px;
}

.toolbar h2 {
  margin: 0;
  font-size: 18px;
  color: var(--addp-text-primary);
  font-weight: 500;
}

.content-area {
  display: flex;
  flex: 1;
  overflow: hidden;
}

.editor-panel,
.result-panel {
  display: flex;
  flex-direction: column;
  flex: 1;
  min-width: 0;
}

.panel-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 10px 16px;
  background: var(--addp-bg-secondary);
  border-bottom: 1px solid var(--addp-border-color);
  color: var(--addp-text-primary);
}

.panel-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 14px;
  font-weight: 500;
}

.hint {
  font-size: 12px;
  color: var(--addp-text-tertiary);
}

.editor-content,
.result-content {
  flex: 1;
  overflow: hidden;
}

.divider {
  width: 1px;
  background: var(--addp-border-color);
  cursor: col-resize;
}

.divider:hover {
  background: var(--el-color-primary);
}
</style>
