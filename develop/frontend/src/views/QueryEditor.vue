<template>
  <div class="query-editor-page">
    <!-- 顶部工具栏 -->
    <div class="toolbar">
      <div class="toolbar-left">
        <h2>{{ t('develop.query.title') }}</h2>

        <!-- 数据源选择 -->
        <el-select
          v-model="selectedEngineId"
          :placeholder="t('develop.query.selectDataSource')"
          style="width: 280px; margin-left: 20px;"
          @change="onEngineChange"
        >
          <el-option
            v-for="engine in engines"
            :key="engine.id"
            :label="`${engine.name} (${engine.engine_type})`"
            :value="engine.id"
          >
            <span style="float: left">{{ engine.name }}</span>
            <span style="float: right; color: var(--addp-text-tertiary); font-size: 13px">
              {{ engine.engine_type }}
            </span>
          </el-option>
        </el-select>

        <el-button
          type="primary"
          :loading="testingConnection"
          @click="handleTestConnection"
          :disabled="selectedEngineId == null"
        >
          <el-icon><Connection /></el-icon>
          {{ t('develop.query.testConnection') }}
        </el-button>
      </div>

      <div class="toolbar-right">
        <el-button @click="formatSQL" :disabled="!queryContent">
          <el-icon><MagicStick /></el-icon>
          {{ t('develop.query.format') }}
        </el-button>

        <el-button
          type="primary"
          @click="showSaveDialog = true"
          :disabled="selectedEngineId == null || !queryContent"
        >
          <el-icon><FolderAdd /></el-icon>
          {{ t('develop.query.saveAsTask') }}
        </el-button>

        <el-button
          type="success"
          @click="executeQuery"
          :loading="executing"
          :disabled="selectedEngineId == null || !queryContent"
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
        <div class="editor-content">
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
import { ref, computed, onMounted } from 'vue'
import { useRoute } from 'vue-router'
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
import { executeQuery as executeAPI, testConnection, saveQueryTask, getSampleQuery } from '../api/query.js'
import { executeFederatedQuery, testDuckDBConnection, getDuckDBSampleQuery } from '../api/duckdb.js'
import { getDevItem } from '../api/devItem.js'
import client from '../api/client.js'

const route = useRoute()
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

// 当前选中引擎是否为 DuckDB 联邦查询引擎
const isDuckDB = computed(() => {
  const engine = engines.value.find(e => e.id === selectedEngineId.value)
  return engine?.engine_type === 'duckdb'
})

// 状态
const currentTaskId = ref(null)
const currentTaskName = ref('')
const selectedEngineId = ref(null)
const engines = ref([])
const queryContent = ref('')
const executionResult = ref(null)
const executing = ref(false)
const testingConnection = ref(false)
const editorRef = ref(null)
const showSaveDialog = ref(false)
const resultViewMode = ref('table') // 'table' | 'graph'

// 是否有图形数据（用于显示图形/表格切换）
const hasGraphData = computed(
  () => executionResult.value?.graph_data?.nodes?.length > 0
)

// 加载数据源列表
const loadEngines = async () => {
  try {
    const response = await client.get('/develop/engines')

    // 添加 null 安全检查
    if (!response || !Array.isArray(response)) {
      console.warn('Develop: 获取到的数据源列表为空或格式不正确:', response)
      engines.value = []
      return
    }

    // 后端已按 dev_modes=query 过滤，直接使用全部结果
    engines.value = response

    // 默认选择第一个
    if (engines.value.length > 0 && !selectedEngineId.value) {
      selectedEngineId.value = engines.value[0].id
      applyEngineLanguage(engines.value[0])
    }
  } catch (error) {
    console.error('Develop: 加载数据源失败:', error)
    ElMessage.error(t('develop.query.loadEnginesFailed') + (error.response?.data?.error || error.message))
    engines.value = []
  }
}

// 测试连接
const handleTestConnection = async () => {
  if (selectedEngineId.value == null) return

  testingConnection.value = true
  try {
    if (isDuckDB.value) {
      await testDuckDBConnection()
    } else {
      await testConnection(selectedEngineId.value)
    }
    ElMessage.success(t('develop.query.testConnectionSuccess'))
  } catch (error) {
    ElMessage.error(t('develop.query.testConnectionFailed') + (error.response?.data?.error || error.message))
  } finally {
    testingConnection.value = false
  }
}

// 执行查询
const executeQuery = async () => {
  if (selectedEngineId.value == null) {
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
    let response
    if (isDuckDB.value) {
      // DuckDB 联邦查询走独立端点
      response = await executeFederatedQuery(queryContent.value.trim(), 120)
    } else {
      response = await executeAPI(
        selectedEngineId.value,
        queryContent.value.trim(),
        120000 // 2分钟超时
      )
    }

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
  if (!queryContent.value) return

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

// 数据源切换
const onEngineChange = () => {
  executionResult.value = null
  const engine = engines.value.find(e => e.id === selectedEngineId.value)
  if (engine) {
    applyEngineLanguage(engine)
  }
}

// 根据引擎类型切换编辑器语言，并自动拉取样例查询填充编辑器
const applyEngineLanguage = async (engine) => {
  const type = engine.engine_type?.toLowerCase() || ''
  const lang = ENGINE_LANGUAGE_MAP[type] || 'sql'
  editorRef.value?.setLanguage(lang)

  try {
    let query, language
    if (type === 'duckdb') {
      // DuckDB 走独立的 sample-query 接口（虚拟引擎无 ID）
      ;({ query, language } = await getDuckDBSampleQuery())
    } else {
      ;({ query, language } = await getSampleQuery(engine.id))
    }
    queryContent.value = query
    editorRef.value?.setLanguage(language)
  } catch {
    // 降级：使用静态模板（网络失败或引擎未连接时）
    const fallbackMap = {
      mongodb: '{"find": "collection_name", "filter": {}, "limit": 10}',
      neo4j: 'MATCH (n)\nRETURN n\nLIMIT 10',
    }
    queryContent.value = fallbackMap[type] ?? 'SELECT 1'
  }
}

// 保存任务
const handleSaveTask = async (taskData) => {
  try {
    await saveQueryTask(taskData)
    ElMessage.success(t('develop.query.saveTaskSuccess'))
    showSaveDialog.value = false
  } catch (error) {
    console.error('保存 查询任务失败:', error)
    ElMessage.error(t('develop.query.saveTaskFailed') + (error.response?.data?.error || error.message))
  }
}

// 加载已有任务
const loadTask = async (taskId) => {
  try {
    const task = await getDevItem(taskId)

    // 设置当前任务信息
    currentTaskId.value = task.id
    currentTaskName.value = task.name

    // 加载 SQL 内容
    if (task.content && task.content.sql) {
      queryContent.value = task.content.sql

      // 如果有关联资源,也设置资源ID
      if (task.engine_id) {
        selectedEngineId.value = task.engine_id
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

// 页面加载
onMounted(async () => {
  await loadEngines()

  // 检查是否有任务 ID
  const taskId = route.query.taskId
  if (taskId) {
    await loadTask(taskId)
  }
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
