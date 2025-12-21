<template>
  <div class="sql-editor-page">
    <!-- 顶部工具栏 -->
    <div class="toolbar">
      <div class="toolbar-left">
        <h2>SQL 开发</h2>

        <!-- 数据源选择 -->
        <el-select
          v-model="selectedResourceId"
          placeholder="选择数据源"
          style="width: 280px; margin-left: 20px;"
          @change="onResourceChange"
        >
          <el-option
            v-for="resource in resources"
            :key="resource.id"
            :label="`${resource.name} (${resource.resource_type})`"
            :value="resource.id"
          >
            <span style="float: left">{{ resource.name }}</span>
            <span style="float: right; color: #8492a6; font-size: 13px">
              {{ resource.resource_type }}
            </span>
          </el-option>
        </el-select>

        <el-button
          type="primary"
          :loading="testingConnection"
          @click="handleTestConnection"
          :disabled="!selectedResourceId"
        >
          <el-icon><Connection /></el-icon>
          测试连接
        </el-button>
      </div>

      <div class="toolbar-right">
        <el-button @click="formatSQL" :disabled="!sqlContent">
          <el-icon><MagicStick /></el-icon>
          格式化
        </el-button>

        <el-button
          type="primary"
          @click="showSaveDialog = true"
          :disabled="!selectedResourceId || !sqlContent"
        >
          <el-icon><FolderAdd /></el-icon>
          保存为任务
        </el-button>

        <el-button
          type="success"
          @click="executeSQL"
          :loading="executing"
          :disabled="!selectedResourceId || !sqlContent"
        >
          <el-icon><VideoPlay /></el-icon>
          执行 (Ctrl+Enter)
        </el-button>
      </div>
    </div>

    <!-- 编辑器和结果分栏 -->
    <div class="content-area">
      <!-- SQL 编辑器 -->
      <div class="editor-panel">
        <div class="panel-header">
          <span class="panel-title">
            <el-icon><Edit /></el-icon>
            SQL 编辑器
          </span>
          <span class="hint">提示: 使用 Ctrl+Enter 快速执行</span>
        </div>
        <div class="editor-content">
          <MonacoEditor
            ref="editorRef"
            v-model="sqlContent"
            language="sql"
            theme="vs-dark"
            @execute="executeSQL"
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
            执行结果
          </span>
          <el-button
            v-if="executionResult"
            type="text"
            size="small"
            @click="clearResult"
          >
            <el-icon><Close /></el-icon>
            清空
          </el-button>
        </div>
        <div class="result-content">
          <SQLResult :result="executionResult" />
        </div>
      </div>
    </div>

    <!-- 保存任务对话框 -->
    <SaveSQLDialog
      v-model="showSaveDialog"
      :resource-id="selectedResourceId"
      :sql="sqlContent"
      @saved="handleSaveTask"
    />
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { useRoute } from 'vue-router'
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
import SQLResult from '../components/SQLResult.vue'
import SaveSQLDialog from '../components/SaveSQLDialog.vue'
import { executeSQL as executeAPI, testConnection, saveSQLTask } from '../api/sql.js'
import { getDevItem } from '../api/devItem.js'
import client from '../api/client.js'

const route = useRoute()

// 状态
const currentTaskId = ref(null)
const currentTaskName = ref('')
const selectedResourceId = ref(null)
const resources = ref([])
const sqlContent = ref('SELECT * FROM users LIMIT 10;')
const executionResult = ref(null)
const executing = ref(false)
const testingConnection = ref(false)
const editorRef = ref(null)
const showSaveDialog = ref(false)

// 加载数据源列表
const loadResources = async () => {
  try {
    const response = await client.get('/develop/resources')

    // 添加 null 安全检查
    if (!response || !Array.isArray(response)) {
      console.warn('Develop: 获取到的数据源列表为空或格式不正确:', response)
      resources.value = []
      return
    }

    resources.value = response.filter(r =>
      ['postgresql', 'mysql', 'doris', 'spark_sql'].includes(r.resource_type.toLowerCase())
    )

    // 默认选择第一个
    if (resources.value.length > 0 && !selectedResourceId.value) {
      selectedResourceId.value = resources.value[0].id
    }
  } catch (error) {
    console.error('Develop: 加载数据源失败:', error)
    ElMessage.error('加载数据源失败: ' + (error.response?.data?.error || error.message))
    resources.value = []
  }
}

// 测试连接
const handleTestConnection = async () => {
  if (!selectedResourceId.value) return

  testingConnection.value = true
  try {
    await testConnection(selectedResourceId.value)
    ElMessage.success('连接测试成功')
  } catch (error) {
    ElMessage.error('连接失败: ' + (error.response?.data?.error || error.message))
  } finally {
    testingConnection.value = false
  }
}

// 执行 SQL
const executeSQL = async () => {
  if (!selectedResourceId.value) {
    ElMessage.warning('请先选择数据源')
    return
  }

  if (!sqlContent.value.trim()) {
    ElMessage.warning('请输入 SQL 语句')
    return
  }

  executing.value = true
  const loadingInstance = ElLoading.service({
    lock: true,
    text: '正在执行 SQL...',
    background: 'rgba(0, 0, 0, 0.7)'
  })

  try {
    const response = await executeAPI(
      selectedResourceId.value,
      sqlContent.value.trim(),
      120000 // 2分钟超时
    )

    executionResult.value = {
      success: true,
      columns: response.columns || [],
      rows: response.rows || [],
      rows_count: response.rows_count,
      rows_affected: response.rows_affected,
      execution_time_ms: response.execution_time_ms
    }

    ElMessage.success('执行成功')
  } catch (error) {
    executionResult.value = {
      success: false,
      error: error.response?.data?.error || error.message
    }
    ElMessage.error('执行失败')
  } finally {
    executing.value = false
    loadingInstance.close()
  }
}

// 格式化 SQL
const formatSQL = () => {
  if (!sqlContent.value) return

  try {
    const formatted = format(sqlContent.value, {
      language: 'postgresql',
      indent: '  ',
      uppercase: true,
      linesBetweenQueries: 2
    })
    sqlContent.value = formatted
    ElMessage.success('格式化成功')
  } catch (error) {
    ElMessage.error('格式化失败: ' + error.message)
  }
}

// 清空结果
const clearResult = () => {
  executionResult.value = null
}

// 数据源切换
const onResourceChange = () => {
  executionResult.value = null
}

// 保存任务
const handleSaveTask = async (taskData) => {
  try {
    await saveSQLTask(taskData)
    ElMessage.success('SQL 任务保存成功')
    showSaveDialog.value = false
  } catch (error) {
    console.error('保存 SQL 任务失败:', error)
    ElMessage.error('保存失败: ' + (error.response?.data?.error || error.message))
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
      sqlContent.value = task.content.sql

      // 如果有关联资源,也设置资源ID
      if (task.resource_id) {
        selectedResourceId.value = task.resource_id
      }

      ElMessage.success(`已加载 SQL 任务: ${task.name}`)
    } else {
      ElMessage.warning('该任务没有 SQL 内容')
    }
  } catch (error) {
    console.error('加载任务失败:', error)
    ElMessage.error('加载任务失败: ' + (error.response?.data?.error || error.message))
  }
}

// 页面加载
onMounted(async () => {
  await loadResources()

  // 检查是否有任务 ID
  const taskId = route.query.taskId
  if (taskId) {
    await loadTask(taskId)
  }
})
</script>

<style scoped>
.sql-editor-page {
  display: flex;
  flex-direction: column;
  height: 100vh;
  background: #f5f7fa;
}

.toolbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 12px 20px;
  background: #fff;
  border-bottom: 1px solid #e4e7ed;
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
  color: #303133;
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
  background: #fafafa;
  border-bottom: 1px solid #e4e7ed;
  color: #303133;
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
  color: #909399;
}

.editor-content,
.result-content {
  flex: 1;
  overflow: hidden;
}

.divider {
  width: 1px;
  background: #e4e7ed;
  cursor: col-resize;
}

.divider:hover {
  background: #409eff;
}
</style>
