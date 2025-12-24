<template>
  <div class="notebook-editor">
    <!-- 顶部工具栏 -->
    <div class="toolbar">
      <div class="toolbar-left">
        <el-icon :size="24"><Notebook /></el-icon>
        <span class="title">Notebook 开发</span>
      </div>
      <div class="toolbar-right">
        <el-button
          type="primary"
          :icon="RefreshRight"
          @click="refreshJupyter"
          size="small"
        >
          刷新
        </el-button>
        <el-button
          type="success"
          :icon="Link"
          @click="openInNewTab"
          size="small"
        >
          新窗口打开
        </el-button>
        <el-button
          :icon="QuestionFilled"
          @click="showHelp"
          size="small"
        >
          帮助
        </el-button>
      </div>
    </div>

    <!-- 状态提示 -->
    <div v-if="loading" class="loading-container">
      <el-icon class="is-loading" :size="32"><Loading /></el-icon>
      <p>正在加载 Jupyter Lab...</p>
    </div>

    <div v-else-if="error" class="error-container">
      <el-icon :size="48" color="#f56c6c"><WarningFilled /></el-icon>
      <p class="error-message">{{ error }}</p>
      <el-button type="primary" @click="checkHealth">重新检查</el-button>
    </div>

    <!-- Jupyter Lab iframe -->
    <div v-else class="jupyter-container">
      <iframe
        ref="jupyterFrame"
        :src="jupyterURL"
        class="jupyter-iframe"
        frameborder="0"
        @load="onJupyterLoad"
      />
    </div>

    <!-- 帮助对话框 -->
    <el-dialog v-model="helpDialogVisible" title="Notebook 开发帮助" width="600px">
      <div class="help-content">
        <h3>🚀 快速开始</h3>
        <p>Notebook 开发基于 Jupyter Lab，提供交互式的 Python 开发环境。</p>

        <h3>📝 主要功能</h3>
        <ul>
          <li><strong>代码执行</strong>: 在 Cell 中编写 Python 代码并执行</li>
          <li><strong>数据分析</strong>: 支持 pandas, numpy, geopandas 等数据分析库</li>
          <li><strong>可视化</strong>: 支持 matplotlib, seaborn, plotly 等可视化库</li>
          <li><strong>地理空间</strong>: 支持 GIS 数据处理和空间分析</li>
        </ul>

        <h3>⌨️ 常用快捷键</h3>
        <ul>
          <li><code>Shift + Enter</code>: 执行当前 Cell 并跳转到下一个</li>
          <li><code>Ctrl + Enter</code>: 执行当前 Cell 但停留在当前位置</li>
          <li><code>A</code>: 在上方插入新 Cell (命令模式)</li>
          <li><code>B</code>: 在下方插入新 Cell (命令模式)</li>
          <li><code>DD</code>: 删除当前 Cell (命令模式)</li>
          <li><code>M</code>: 转换为 Markdown Cell (命令模式)</li>
          <li><code>Y</code>: 转换为 Code Cell (命令模式)</li>
        </ul>

        <h3>📦 预装库</h3>
        <ul>
          <li>数据处理: pandas, numpy, scipy</li>
          <li>可视化: matplotlib, seaborn, plotly</li>
          <li>地理空间: geopandas, shapely, fiona</li>
          <li>数据库: psycopg2, sqlalchemy</li>
        </ul>

        <h3>💡 提示</h3>
        <ul>
          <li>使用 <code>!</code> 前缀可以执行 Shell 命令</li>
          <li>使用 <code>?</code> 后缀可以查看对象文档</li>
          <li>使用 <code>%%time</code> 魔术命令测量代码执行时间</li>
        </ul>
      </div>
      <template #footer>
        <el-button type="primary" @click="helpDialogVisible = false">
          知道了
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted, onBeforeUnmount } from 'vue'
import { ElMessage } from 'element-plus'
import {
  Notebook,
  RefreshRight,
  Link,
  QuestionFilled,
  Loading,
  WarningFilled
} from '@element-plus/icons-vue'
import { notebookAPI } from '@/api/notebook'

// 响应式状态
const jupyterURL = ref('')
const loading = ref(true)
const error = ref('')
const jupyterFrame = ref(null)
const helpDialogVisible = ref(false)

// 检查 Jupyter Engine 健康状态
const checkHealth = async () => {
  loading.value = true
  error.value = ''

  try {
    await notebookAPI.healthCheck()
    // 健康检查通过，获取 Jupyter URL
    await loadJupyterURL()
  } catch (err) {
    console.error('Jupyter 健康检查失败:', err)
    error.value = err.response?.data?.error || 'Jupyter Engine 服务不可用，请检查服务是否启动'
    loading.value = false
  }
}

// 获取 Jupyter Lab URL
const loadJupyterURL = async () => {
  try {
    // 直接使用本地 Jupyter Lab URL（开发模式）
    jupyterURL.value = 'http://localhost:8088/lab'
    loading.value = false
  } catch (err) {
    console.error('获取 Jupyter URL 失败:', err)
    error.value = '获取 Jupyter URL 失败'
    loading.value = false
  }
}

// Jupyter iframe 加载完成
const onJupyterLoad = () => {
  console.log('Jupyter Lab 加载完成')
}

// 刷新 Jupyter
const refreshJupyter = () => {
  if (jupyterFrame.value) {
    jupyterFrame.value.src = jupyterFrame.value.src
    ElMessage.success('正在刷新 Jupyter Lab')
  }
}

// 在新窗口打开
const openInNewTab = () => {
  if (jupyterURL.value) {
    window.open(jupyterURL.value, '_blank')
    ElMessage.success('已在新窗口打开 Jupyter Lab')
  }
}

// 显示帮助
const showHelp = () => {
  helpDialogVisible.value = true
}

// 组件挂载
onMounted(() => {
  checkHealth()
})

// 组件卸载
onBeforeUnmount(() => {
  // 清理工作
})
</script>

<style scoped>
.notebook-editor {
  display: flex;
  flex-direction: column;
  height: 100vh;
  background: #fff;
}

.toolbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 12px 20px;
  border-bottom: 1px solid #e4e7ed;
  background: #fff;
  min-height: 56px;
}

.toolbar-left {
  display: flex;
  align-items: center;
  gap: 10px;
}

.toolbar-left .title {
  font-size: 18px;
  font-weight: 600;
  color: #303133;
}

.toolbar-right {
  display: flex;
  gap: 10px;
}

.loading-container,
.error-container {
  flex: 1;
  display: flex;
  flex-direction: column;
  justify-content: center;
  align-items: center;
  gap: 16px;
  background: #f5f7fa;
}

.loading-container p,
.error-container p {
  font-size: 16px;
  color: #606266;
}

.error-message {
  color: #f56c6c;
  margin: 0;
}

.jupyter-container {
  flex: 1;
  overflow: hidden;
  position: relative;
}

.jupyter-iframe {
  width: 100%;
  height: 100%;
  border: none;
}

/* 帮助对话框样式 */
.help-content {
  line-height: 1.8;
}

.help-content h3 {
  margin-top: 16px;
  margin-bottom: 8px;
  color: #303133;
  font-size: 16px;
}

.help-content h3:first-child {
  margin-top: 0;
}

.help-content p {
  margin: 8px 0;
  color: #606266;
}

.help-content ul {
  margin: 8px 0;
  padding-left: 24px;
  color: #606266;
}

.help-content li {
  margin: 4px 0;
}

.help-content code {
  background: #f5f7fa;
  padding: 2px 6px;
  border-radius: 3px;
  font-family: 'Monaco', 'Courier New', monospace;
  font-size: 13px;
  color: #e83e8c;
}
</style>
