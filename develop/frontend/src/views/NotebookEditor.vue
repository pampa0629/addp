<template>
  <div class="notebook-editor">
    <!-- 左侧 Notebook 列表 -->
    <el-aside width="320px" class="notebook-sidebar">
      <div class="sidebar-header">
        <h3>Notebook 列表</h3>
        <div class="actions">
          <el-button type="primary" size="small" @click="showCreateDialog">
            <el-icon><Plus /></el-icon> 新建
          </el-button>
          <el-button type="success" size="small" @click="showUploadDialog">
            <el-icon><Upload /></el-icon> 上传
          </el-button>
          <el-button size="small" @click="loadNotebooks">
            <el-icon><Refresh /></el-icon>
          </el-button>
        </div>
      </div>

      <el-input
        v-model="searchKeyword"
        placeholder="搜索 Notebook"
        clearable
        class="search-input"
        @input="loadNotebooks"
      >
        <template #prefix>
          <el-icon><Search /></el-icon>
        </template>
      </el-input>

      <div class="notebook-list" v-loading="loading">
        <el-empty v-if="!loading && notebooks.length === 0" description="暂无 Notebook" />

        <div
          v-for="notebook in notebooks"
          :key="notebook.id"
          class="notebook-item"
          :class="{ active: currentNotebook && currentNotebook.id === notebook.id }"
          @click="selectNotebook(notebook)"
        >
          <div class="notebook-info">
            <div class="notebook-name">{{ notebook.display_name || notebook.name }}</div>
            <div class="notebook-meta">
              <span class="notebook-time">{{ formatTime(notebook.updated_at) }}</span>
            </div>
          </div>

          <div class="notebook-actions" @click.stop>
            <el-tooltip content="在 Jupyter Lab 中打开">
              <el-button type="primary" size="small" text @click="openInJupyter(notebook)">
                <el-icon><Document /></el-icon>
              </el-button>
            </el-tooltip>

            <el-tooltip content="执行">
              <el-button type="success" size="small" text @click="showExecuteDialog(notebook)">
                <el-icon><VideoPlay /></el-icon>
              </el-button>
            </el-tooltip>

            <el-tooltip content="执行历史">
              <el-button type="info" size="small" text @click="viewHistory(notebook)">
                <el-icon><Clock /></el-icon>
              </el-button>
            </el-tooltip>

            <el-dropdown @command="handleCommand($event, notebook)">
              <el-button size="small" text>
                <el-icon><More /></el-icon>
              </el-button>
              <template #dropdown>
                <el-dropdown-menu>
                  <el-dropdown-item command="download">
                    <el-icon><Download /></el-icon> 下载
                  </el-dropdown-item>
                  <el-dropdown-item command="delete" divided>
                    <el-icon><Delete /></el-icon> 删除
                  </el-dropdown-item>
                </el-dropdown-menu>
              </template>
            </el-dropdown>
          </div>
        </div>

        <el-pagination
          v-if="total > pageSize"
          v-model:current-page="currentPage"
          v-model:page-size="pageSize"
          :total="total"
          :page-sizes="[10, 20, 50]"
          layout="total, sizes, prev, pager, next"
          small
          @current-change="loadNotebooks"
          @size-change="loadNotebooks"
          class="pagination"
        />
      </div>
    </el-aside>

    <!-- 右侧 Jupyter Lab -->
    <el-main class="jupyter-container">
      <div v-if="!currentNotebook" class="empty-state">
        <el-empty description="请选择一个 Notebook 或创建新的 Notebook" />
      </div>

      <div v-else class="jupyter-wrapper">
        <div class="jupyter-toolbar">
          <span class="current-notebook-name">{{ currentNotebook.display_name || currentNotebook.name }}</span>
          <div class="toolbar-actions">
            <el-button size="small" @click="refreshJupyter">
              <el-icon><Refresh /></el-icon> 刷新
            </el-button>
            <el-button size="small" @click="openInNewTab">
              <el-icon><TopRight /></el-icon> 新窗口打开
            </el-button>
            <el-button size="small" @click="showHelp">
              <el-icon><QuestionFilled /></el-icon> 帮助
            </el-button>
          </div>
        </div>

        <!-- 虚拟环境初始化卡片 -->
        <div v-if="checkingVenv" class="venv-status-card" v-loading="true" element-loading-text="正在检查开发环境...">
          <el-empty description="正在检查开发环境状态..." />
        </div>

        <div v-else-if="!venvReady" class="venv-init-card">
          <el-result icon="warning" title="需要初始化开发环境">
            <template #sub-title>
              <div class="venv-init-tips">
                <p>首次使用 Notebook 需要初始化租户专属的 Python 虚拟环境</p>
                <p>初始化后您将拥有:</p>
                <ul>
                  <li>✅ 独立的 Python 虚拟环境（继承预装的常用库）</li>
                  <li>✅ 自动注入的数据源连接（无需手动配置）</li>
                  <li>✅ 可自由安装 Python 库（不影响其他租户）</li>
                  <li>✅ 环境持久化保存（ADDP 重启后仍然保留）</li>
                </ul>
                <p class="time-note">⏱️ 初始化过程约需 30 秒</p>
              </div>
            </template>
            <template #extra>
              <el-button
                type="primary"
                size="large"
                @click="initVenvEnvironment"
                :loading="initLoading"
              >
                {{ initLoading ? '正在初始化...' : '立即初始化' }}
              </el-button>
            </template>
          </el-result>
        </div>

        <!-- Jupyter Lab iframe -->
        <iframe
          v-else
          ref="jupyterIframe"
          :src="jupyterUrl"
          class="jupyter-iframe"
          @load="onJupyterLoad"
        />
      </div>
    </el-main>

    <!-- 新建 Notebook 对话框 -->
    <el-dialog
      v-model="createDialogVisible"
      title="新建 Notebook"
      width="600px"
    >
      <el-form :model="createForm" label-width="100px">
        <el-form-item label="名称" required>
          <el-input v-model="createForm.name" placeholder="请输入 Notebook 名称" />
        </el-form-item>

        <el-form-item label="描述">
          <el-input
            v-model="createForm.description"
            type="textarea"
            :rows="3"
            placeholder="请输入描述"
          />
        </el-form-item>

        <el-form-item label="Kernel">
          <el-select v-model="createForm.kernel" placeholder="选择 Kernel">
            <el-option label="Python 3" value="python3" />
          </el-select>
        </el-form-item>

        <el-form-item label="数据源">
          <el-select
            v-model="createForm.data_sources"
            multiple
            placeholder="选择可用的数据源"
            style="width: 100%"
          >
            <el-option
              v-for="ds in dataSources"
              :key="ds.id"
              :label="ds.display_name || ds.name"
              :value="ds.id"
            />
          </el-select>
        </el-form-item>
      </el-form>

      <template #footer>
        <el-button @click="createDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="confirmCreate" :loading="creating">确定</el-button>
      </template>
    </el-dialog>

    <!-- 上传 Notebook 对话框 -->
    <el-dialog
      v-model="uploadDialogVisible"
      title="上传 Notebook"
      width="600px"
    >
      <el-form :model="uploadForm" label-width="100px">
        <el-form-item label="文件" required>
          <el-upload
            ref="uploadRef"
            :auto-upload="false"
            :limit="1"
            accept=".ipynb"
            :on-change="handleFileChange"
          >
            <template #trigger>
              <el-button type="primary">选择文件</el-button>
            </template>
            <template #tip>
              <div class="el-upload__tip">只能上传 .ipynb 文件</div>
            </template>
          </el-upload>
        </el-form-item>

        <el-form-item label="名称" required>
          <el-input v-model="uploadForm.name" placeholder="请输入 Notebook 名称" />
        </el-form-item>

        <el-form-item label="描述">
          <el-input
            v-model="uploadForm.description"
            type="textarea"
            :rows="3"
            placeholder="请输入描述"
          />
        </el-form-item>

        <el-form-item label="数据源">
          <el-select
            v-model="uploadForm.data_sources"
            multiple
            placeholder="选择可用的数据源"
            style="width: 100%"
          >
            <el-option
              v-for="ds in dataSources"
              :key="ds.id"
              :label="ds.display_name || ds.name"
              :value="ds.id"
            />
          </el-select>
        </el-form-item>
      </el-form>

      <template #footer>
        <el-button @click="uploadDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="confirmUpload" :loading="uploading">确定上传</el-button>
      </template>
    </el-dialog>

    <!-- 执行 Notebook 对话框 -->
    <el-dialog
      v-model="executeDialogVisible"
      title="执行 Notebook"
      width="600px"
    >
      <el-form :model="executeForm" label-width="100px">
        <el-form-item label="Notebook">
          <el-input :value="executeNotebook?.display_name || executeNotebook?.name" disabled />
        </el-form-item>

        <el-form-item label="参数">
          <el-input
            v-model="executeForm.parameters"
            type="textarea"
            :rows="5"
            placeholder="输入 JSON 格式的参数，例如：&#10;{&#10;  &quot;city_name&quot;: &quot;北京&quot;,&#10;  &quot;buffer_distance&quot;: 1000&#10;}"
          />
        </el-form-item>

        <el-form-item label="数据源">
          <el-select
            v-model="executeForm.data_source_ids"
            multiple
            placeholder="选择数据源（可选）"
            style="width: 100%"
          >
            <el-option
              v-for="ds in dataSources"
              :key="ds.id"
              :label="ds.display_name || ds.name"
              :value="ds.id"
            />
          </el-select>
          <div class="form-tip">选择的数据源连接信息将自动注入到 Notebook 执行环境中</div>
        </el-form-item>
      </el-form>

      <template #footer>
        <el-button @click="executeDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="confirmExecute" :loading="executing">确定执行</el-button>
      </template>
    </el-dialog>

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
import { ref, onMounted, computed } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  Plus, Upload, Refresh, Search, Document, VideoPlay, Clock, More,
  Download, Delete, TopRight, QuestionFilled
} from '@element-plus/icons-vue'
import { notebookAPI } from '@/api/notebook'
import { listDevItems, deleteDevItem, executeDevItem } from '@/api/devItem'
import { listEngines } from '@/api/engines'
import { getVenvStatus, initVenv } from '@/api/jupyter'
import { useRouter } from 'vue-router'
import dayjs from 'dayjs'

const router = useRouter()

// 列表相关
const notebooks = ref([])
const currentNotebook = ref(null)
const loading = ref(false)
const currentPage = ref(1)
const pageSize = ref(20)
const total = ref(0)
const searchKeyword = ref('')

// 数据源列表
const dataSources = ref([])

// Jupyter Lab 相关
const jupyterIframe = ref(null)
const jupyterBaseUrl = ref('http://localhost:8088/lab')

// 虚拟环境状态
const venvInfo = ref(null)
const venvReady = ref(false)
const initLoading = ref(false)
const checkingVenv = ref(true)

const jupyterUrl = computed(() => {
  if (!currentNotebook.value || !venvInfo.value) return ''

  // 从 content 中获取 minio_path
  const minioPath = currentNotebook.value.content?.minio_path
  if (!minioPath) return jupyterBaseUrl.value

  // 构造 Jupyter Lab URL
  // 使用 /lab/tree/ 路径打开指定文件，Jupyter 会自动使用租户的 Kernel
  // 注意: 由于配置了 TenantKernelSpecManager，Jupyter 只会显示租户的 Kernel
  const kernelName = venvInfo.value.kernel_name
  return `${jupyterBaseUrl.value}/lab/tree/${minioPath}`
})

// 新建对话框
const createDialogVisible = ref(false)
const creating = ref(false)
const createForm = ref({
  name: '',
  description: '',
  kernel: 'python3',
  data_sources: []
})

// 上传对话框
const uploadDialogVisible = ref(false)
const uploading = ref(false)
const uploadRef = ref(null)
const uploadForm = ref({
  file: null,
  name: '',
  description: '',
  data_sources: []
})

// 执行对话框
const executeDialogVisible = ref(false)
const executing = ref(false)
const executeNotebook = ref(null)
const executeForm = ref({
  parameters: '{}',
  data_source_ids: []
})

// 帮助对话框
const helpDialogVisible = ref(false)

// 加载 Notebook 列表
const loadNotebooks = async () => {
  loading.value = true
  try {
    const params = {
      dev_type: 'notebook',
      page: currentPage.value,
      page_size: pageSize.value
    }

    if (searchKeyword.value) {
      params.keyword = searchKeyword.value
    }

    const response = await listDevItems(params)
    notebooks.value = response.items || []
    total.value = response.total || 0
  } catch (error) {
    console.error('加载 Notebook 列表失败:', error)
    ElMessage.error('加载 Notebook 列表失败')
  } finally {
    loading.value = false
  }
}

// 加载数据源列表（从 System 模块）
const loadDataSources = async () => {
  try {
    const data = await listEngines()
    dataSources.value = Array.isArray(data) ? data : []
  } catch (error) {
    console.error('加载数据源列表失败:', error)
    ElMessage.error('加载数据源列表失败')
  }
}

// 选择 Notebook
const selectNotebook = (notebook) => {
  currentNotebook.value = notebook
}

// 在 Jupyter Lab 中打开
const openInJupyter = (notebook) => {
  selectNotebook(notebook)
}

// 刷新 Jupyter iframe
const refreshJupyter = () => {
  if (jupyterIframe.value) {
    jupyterIframe.value.src = jupyterIframe.value.src
  }
}

// 在新窗口打开 Jupyter Lab
const openInNewTab = () => {
  if (jupyterUrl.value) {
    window.open(jupyterUrl.value, '_blank')
  }
}

// 显示帮助
const showHelp = () => {
  helpDialogVisible.value = true
}

// Jupyter iframe 加载完成
const onJupyterLoad = () => {
  console.log('Jupyter Lab 加载完成')
}

// 显示新建对话框
const showCreateDialog = () => {
  createForm.value = {
    name: '',
    description: '',
    kernel: 'python3',
    data_sources: []
  }
  createDialogVisible.value = true
}

// 确认新建
const confirmCreate = async () => {
  if (!createForm.value.name) {
    ElMessage.warning('请输入 Notebook 名称')
    return
  }

  creating.value = true
  try {
    // 创建空的 Notebook 结构
    const notebookContent = {
      cells: [
        {
          cell_type: 'markdown',
          metadata: {},
          source: [`# ${createForm.value.name}`]
        },
        {
          cell_type: 'code',
          execution_count: null,
          metadata: {},
          outputs: [],
          source: []
        }
      ],
      metadata: {
        kernelspec: {
          display_name: 'Python 3',
          language: 'python',
          name: 'python3'
        },
        language_info: {
          name: 'python',
          version: '3.9.0'
        }
      },
      nbformat: 4,
      nbformat_minor: 5
    }

    // 创建 File 对象
    const blob = new Blob([JSON.stringify(notebookContent, null, 2)], { type: 'application/json' })
    const file = new File([blob], `${createForm.value.name}.ipynb`, { type: 'application/json' })

    // 上传
    await notebookAPI.uploadNotebook(file, {
      name: createForm.value.name,
      description: createForm.value.description,
      kernel: createForm.value.kernel,
      data_sources: createForm.value.data_sources
    })

    ElMessage.success('创建成功')
    createDialogVisible.value = false
    await loadNotebooks()
  } catch (error) {
    console.error('创建失败:', error)
    ElMessage.error(error.response?.data?.error || '创建失败')
  } finally {
    creating.value = false
  }
}

// 显示上传对话框
const showUploadDialog = () => {
  uploadForm.value = {
    file: null,
    name: '',
    description: '',
    data_sources: []
  }
  uploadDialogVisible.value = true
}

// 文件选择改变
const handleFileChange = (file) => {
  uploadForm.value.file = file.raw
  if (!uploadForm.value.name) {
    uploadForm.value.name = file.name.replace('.ipynb', '')
  }
}

// 确认上传
const confirmUpload = async () => {
  if (!uploadForm.value.file) {
    ElMessage.warning('请选择文件')
    return
  }

  if (!uploadForm.value.name) {
    ElMessage.warning('请输入 Notebook 名称')
    return
  }

  uploading.value = true
  try {
    await notebookAPI.uploadNotebook(uploadForm.value.file, {
      name: uploadForm.value.name,
      description: uploadForm.value.description,
      data_sources: uploadForm.value.data_sources
    })

    ElMessage.success('上传成功')
    uploadDialogVisible.value = false
    await loadNotebooks()
  } catch (error) {
    console.error('上传失败:', error)
    ElMessage.error(error.response?.data?.error || '上传失败')
  } finally {
    uploading.value = false
  }
}

// 显示执行对话框
const showExecuteDialog = (notebook) => {
  executeNotebook.value = notebook
  executeForm.value = {
    parameters: JSON.stringify(notebook.content?.parameters || {}, null, 2),
    data_source_ids: notebook.content?.data_sources || []
  }
  executeDialogVisible.value = true
}

// 确认执行
const confirmExecute = async () => {
  // 验证参数 JSON
  let parameters = {}
  try {
    parameters = JSON.parse(executeForm.value.parameters || '{}')
  } catch (error) {
    ElMessage.error('参数格式不正确，请输入有效的 JSON')
    return
  }

  executing.value = true
  try {
    // 调用统一的 dev_item 执行接口
    const response = await executeDevItem(executeNotebook.value.id, {
      parameters,
      data_source_ids: executeForm.value.data_source_ids
    })

    ElMessage.success(`Notebook 已提交执行，执行 ID: ${response.execution_id}`)
    executeDialogVisible.value = false

    // 跳转到执行监控页面
    router.push({
      path: '/executions',
      query: { execution_id: response.execution_id }
    })
  } catch (error) {
    console.error('执行失败:', error)
    ElMessage.error(error.response?.data?.error || '执行失败')
  } finally {
    executing.value = false
  }
}

// 查看执行历史
const viewHistory = (notebook) => {
  router.push({
    path: '/executions',
    query: { dev_item_id: notebook.id }
  })
}

// 下拉菜单命令处理
const handleCommand = async (command, notebook) => {
  switch (command) {
    case 'download':
      await downloadNotebook(notebook)
      break
    case 'delete':
      await deleteNotebook(notebook)
      break
  }
}

// 下载 Notebook
const downloadNotebook = async (notebook) => {
  try {
    const response = await notebookAPI.downloadNotebook(notebook.id)

    // 创建下载链接（response 已经是 blob，因为 extractData 拦截器会提取 data）
    const url = window.URL.createObjectURL(new Blob([response]))
    const link = document.createElement('a')
    link.href = url
    link.download = `${notebook.name}.ipynb`
    document.body.appendChild(link)
    link.click()
    document.body.removeChild(link)
    window.URL.revokeObjectURL(url)

    ElMessage.success('下载成功')
  } catch (error) {
    console.error('下载失败:', error)
    ElMessage.error('下载失败')
  }
}

// 删除 Notebook
const deleteNotebook = async (notebook) => {
  try {
    await ElMessageBox.confirm(
      `确定要删除 Notebook "${notebook.display_name || notebook.name}" 吗？`,
      '确认删除',
      {
        confirmButtonText: '确定',
        cancelButtonText: '取消',
        type: 'warning'
      }
    )

    await deleteDevItem(notebook.id)
    ElMessage.success('删除成功')

    // 如果删除的是当前选中的 Notebook，清空选中状态
    if (currentNotebook.value && currentNotebook.value.id === notebook.id) {
      currentNotebook.value = null
    }

    await loadNotebooks()
  } catch (error) {
    if (error !== 'cancel') {
      console.error('删除失败:', error)
      ElMessage.error('删除失败')
    }
  }
}

// 格式化时间
const formatTime = (time) => {
  if (!time) return '-'
  return dayjs(time).format('YYYY-MM-DD HH:mm')
}

// 初始化
// 检查虚拟环境状态
const checkVenvStatus = async () => {
  try {
    checkingVenv.value = true
    const res = await getVenvStatus()
    venvInfo.value = res.data || res
    venvReady.value = venvInfo.value.exists
  } catch (error) {
    console.error('检查虚拟环境状态失败:', error)
    ElMessage.error('检查虚拟环境状态失败')
    venvReady.value = false
  } finally {
    checkingVenv.value = false
  }
}

// 初始化虚拟环境
const initVenvEnvironment = async () => {
  try {
    initLoading.value = true
    ElMessage.info('正在初始化开发环境，请稍候...')

    const res = await initVenv()
    venvInfo.value = res.data?.data || res.data
    venvReady.value = true

    ElMessage.success('开发环境初始化成功！')
  } catch (error) {
    console.error('初始化虚拟环境失败:', error)
    ElMessage.error(error.response?.data?.error || '初始化失败')
    venvReady.value = false
  } finally {
    initLoading.value = false
  }
}

onMounted(async () => {
  await checkVenvStatus()
  await loadNotebooks()
  await loadDataSources()
})
</script>

<style scoped>
.notebook-editor {
  display: flex;
  height: 100%;
  background: #f5f5f5;
}

.notebook-sidebar {
  background: white;
  border-right: 1px solid #e0e0e0;
  display: flex;
  flex-direction: column;
}

.sidebar-header {
  padding: 16px;
  border-bottom: 1px solid #e0e0e0;
}

.sidebar-header h3 {
  margin: 0 0 12px 0;
  font-size: 16px;
  font-weight: 600;
}

.sidebar-header .actions {
  display: flex;
  gap: 8px;
}

.search-input {
  margin: 12px 16px;
}

.notebook-list {
  flex: 1;
  overflow-y: auto;
  padding: 8px;
}

.notebook-item {
  padding: 12px;
  margin-bottom: 8px;
  border-radius: 4px;
  border: 1px solid #e0e0e0;
  background: white;
  cursor: pointer;
  transition: all 0.2s;
}

.notebook-item:hover {
  border-color: #409eff;
  box-shadow: 0 2px 4px rgba(64, 158, 255, 0.1);
}

.notebook-item.active {
  border-color: #409eff;
  background: #ecf5ff;
}

.notebook-info {
  margin-bottom: 8px;
}

.notebook-name {
  font-weight: 500;
  margin-bottom: 4px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.notebook-meta {
  font-size: 12px;
  color: #999;
}

.notebook-actions {
  display: flex;
  gap: 4px;
  justify-content: flex-end;
}

.pagination {
  margin-top: 12px;
  justify-content: center;
}

.jupyter-container {
  display: flex;
  flex-direction: column;
  padding: 0;
}

.empty-state {
  display: flex;
  align-items: center;
  justify-content: center;
  height: 100%;
}

.jupyter-wrapper {
  display: flex;
  flex-direction: column;
  height: 100%;
}

.jupyter-toolbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 12px 16px;
  background: white;
  border-bottom: 1px solid #e0e0e0;
}

.current-notebook-name {
  font-weight: 500;
  font-size: 14px;
}

.toolbar-actions {
  display: flex;
  gap: 8px;
}

.jupyter-iframe {
  flex: 1;
  border: none;
  background: white;
}

/* 虚拟环境状态卡片 */
.venv-status-card {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  background: white;
  padding: 40px;
}

.venv-init-card {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  background: white;
  padding: 40px;
}

.venv-init-tips {
  text-align: left;
  max-width: 600px;
  margin: 0 auto;
  line-height: 1.8;
}

.venv-init-tips p {
  margin: 12px 0;
  color: #606266;
}

.venv-init-tips ul {
  margin: 16px 0;
  padding-left: 20px;
  color: #606266;
}

.venv-init-tips ul li {
  margin: 8px 0;
}

.venv-init-tips .time-note {
  margin-top: 16px;
  font-size: 13px;
  color: #909399;
}

.form-tip {
  font-size: 12px;
  color: #999;
  margin-top: 4px;
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
