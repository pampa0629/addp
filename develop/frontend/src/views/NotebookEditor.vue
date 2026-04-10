<template>
  <div class="notebook-editor">
    <!-- 左侧 Notebook 列表 -->
    <el-aside width="320px" class="notebook-sidebar">
      <div class="sidebar-header">
        <h3>{{ t('develop.notebook.listTitle') }}</h3>
        <div class="actions">
          <el-button type="primary" size="small" @click="showCreateDialog">
            <el-icon><Plus /></el-icon> {{ t('develop.notebook.create') }}
          </el-button>
          <el-button type="success" size="small" @click="showUploadDialog">
            <el-icon><Upload /></el-icon> {{ t('develop.notebook.upload') }}
          </el-button>
          <el-button size="small" @click="loadNotebooks">
            <el-icon><Refresh /></el-icon>
          </el-button>
        </div>
      </div>

      <el-input
        v-model="searchKeyword"
        :placeholder="t('develop.notebook.searchPlaceholder')"
        clearable
        class="search-input"
        @input="loadNotebooks"
      >
        <template #prefix>
          <el-icon><Search /></el-icon>
        </template>
      </el-input>

      <div class="notebook-list" v-loading="loading">
        <el-empty v-if="!loading && notebooks.length === 0" :description="t('develop.notebook.empty')" />

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
            <el-tooltip :content="t('develop.notebook.openInJupyter')">
              <el-button type="primary" size="small" text @click="openInJupyter(notebook)">
                <el-icon><Document /></el-icon>
              </el-button>
            </el-tooltip>

            <el-tooltip :content="t('develop.notebook.execute')">
              <el-button type="success" size="small" text @click="showExecuteDialog(notebook)">
                <el-icon><VideoPlay /></el-icon>
              </el-button>
            </el-tooltip>

            <el-tooltip :content="t('develop.notebook.history')">
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
                    <el-icon><Download /></el-icon> {{ t('develop.notebook.download') }}
                  </el-dropdown-item>
                  <el-dropdown-item command="delete" divided>
                    <el-icon><Delete /></el-icon> {{ t('develop.notebook.delete') }}
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
        <el-empty :description="t('develop.notebook.selectHint')" />
      </div>

      <div v-else class="jupyter-wrapper">
        <div class="jupyter-toolbar">
          <span class="current-notebook-name">{{ currentNotebook.display_name || currentNotebook.name }}</span>
          <div class="toolbar-actions">
            <el-button size="small" @click="refreshJupyter">
              <el-icon><Refresh /></el-icon> {{ t('develop.notebook.refresh') }}
            </el-button>
            <el-button size="small" @click="openInNewTab">
              <el-icon><TopRight /></el-icon> {{ t('develop.notebook.openNewTab') }}
            </el-button>
            <el-button size="small" @click="showHelp">
              <el-icon><QuestionFilled /></el-icon> {{ t('develop.notebook.help') }}
            </el-button>
          </div>
        </div>

        <!-- 虚拟环境初始化卡片 -->
        <div v-if="checkingVenv" class="venv-status-card" v-loading="true" :element-loading-text="t('develop.notebook.checkingVenv')">
          <el-empty :description="t('develop.notebook.checkingVenvStatus')" />
        </div>

        <div v-else-if="!venvReady" class="venv-init-card">
          <el-result icon="warning" :title="t('develop.notebook.venvRequired')">
            <template #sub-title>
              <div class="venv-init-tips">
                <p>{{ t('develop.notebook.venvInitHint') }}</p>
                <p>{{ t('develop.notebook.venvInitBenefits') }}</p>
                <ul>
                  <li>{{ t('develop.notebook.venvBenefit1') }}</li>
                  <li>{{ t('develop.notebook.venvBenefit2') }}</li>
                  <li>{{ t('develop.notebook.venvBenefit3') }}</li>
                  <li>{{ t('develop.notebook.venvBenefit4') }}</li>
                </ul>
                <p class="time-note">{{ t('develop.notebook.venvInitTime') }}</p>
              </div>
            </template>
            <template #extra>
              <el-button
                type="primary"
                size="large"
                @click="initVenvEnvironment"
                :loading="initLoading"
              >
                {{ initLoading ? t('develop.notebook.initializing') : t('develop.notebook.initNow') }}
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
      :title="t('develop.notebook.createDialogTitle')"
      width="600px"
    >
      <el-form :model="createForm" label-width="100px">
        <el-form-item :label="t('develop.notebook.fieldName')" required>
          <el-input v-model="createForm.name" :placeholder="t('develop.notebook.namePlaceholder')" />
        </el-form-item>

        <el-form-item :label="t('develop.notebook.fieldDescription')">
          <el-input
            v-model="createForm.description"
            type="textarea"
            :rows="3"
            :placeholder="t('develop.notebook.descriptionPlaceholder')"
          />
        </el-form-item>

        <el-form-item label="Kernel">
          <el-select v-model="createForm.kernel" :placeholder="t('develop.notebook.selectKernel')">
            <el-option label="Python 3" value="python3" />
          </el-select>
        </el-form-item>

        <el-form-item :label="t('develop.notebook.fieldDataSource')">
          <el-select
            v-model="createForm.data_sources"
            multiple
            :placeholder="t('develop.notebook.selectDataSource')"
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
        <el-button @click="createDialogVisible = false">{{ t('develop.notebook.cancel') }}</el-button>
        <el-button type="primary" @click="confirmCreate" :loading="creating">{{ t('develop.notebook.confirm') }}</el-button>
      </template>
    </el-dialog>

    <!-- 上传 Notebook 对话框 -->
    <el-dialog
      v-model="uploadDialogVisible"
      :title="t('develop.notebook.uploadDialogTitle')"
      width="600px"
    >
      <el-form :model="uploadForm" label-width="100px">
        <el-form-item :label="t('develop.notebook.fieldFile')" required>
          <el-upload
            ref="uploadRef"
            :auto-upload="false"
            :limit="1"
            accept=".ipynb"
            :on-change="handleFileChange"
          >
            <template #trigger>
              <el-button type="primary">{{ t('develop.notebook.selectFile') }}</el-button>
            </template>
            <template #tip>
              <div class="el-upload__tip">{{ t('develop.notebook.uploadTip') }}</div>
            </template>
          </el-upload>
        </el-form-item>

        <el-form-item :label="t('develop.notebook.fieldName')" required>
          <el-input v-model="uploadForm.name" :placeholder="t('develop.notebook.namePlaceholder')" />
        </el-form-item>

        <el-form-item :label="t('develop.notebook.fieldDescription')">
          <el-input
            v-model="uploadForm.description"
            type="textarea"
            :rows="3"
            :placeholder="t('develop.notebook.descriptionPlaceholder')"
          />
        </el-form-item>

        <el-form-item :label="t('develop.notebook.fieldDataSource')">
          <el-select
            v-model="uploadForm.data_sources"
            multiple
            :placeholder="t('develop.notebook.selectDataSource')"
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
        <el-button @click="uploadDialogVisible = false">{{ t('develop.notebook.cancel') }}</el-button>
        <el-button type="primary" @click="confirmUpload" :loading="uploading">{{ t('develop.notebook.confirmUpload') }}</el-button>
      </template>
    </el-dialog>

    <!-- 执行 Notebook 对话框 -->
    <el-dialog
      v-model="executeDialogVisible"
      :title="t('develop.notebook.executeDialogTitle')"
      width="600px"
    >
      <el-form :model="executeForm" label-width="100px">
        <el-form-item label="Notebook">
          <el-input :value="executeNotebook?.display_name || executeNotebook?.name" disabled />
        </el-form-item>

        <el-form-item :label="t('develop.notebook.fieldParams')">
          <el-input
            v-model="executeForm.parameters"
            type="textarea"
            :rows="5"
            placeholder="输入 JSON 格式的参数，例如：&#10;{&#10;  &quot;city_name&quot;: &quot;北京&quot;,&#10;  &quot;buffer_distance&quot;: 1000&#10;}"
          />
        </el-form-item>

        <el-form-item :label="t('develop.notebook.fieldDataSource')">
          <el-select
            v-model="executeForm.data_source_ids"
            multiple
            :placeholder="t('develop.notebook.selectDataSourceOptional')"
            style="width: 100%"
          >
            <el-option
              v-for="ds in dataSources"
              :key="ds.id"
              :label="ds.display_name || ds.name"
              :value="ds.id"
            />
          </el-select>
          <div class="form-tip">{{ t('develop.notebook.dataSourceInjectHint') }}</div>
        </el-form-item>
      </el-form>

      <template #footer>
        <el-button @click="executeDialogVisible = false">{{ t('develop.notebook.cancel') }}</el-button>
        <el-button type="primary" @click="confirmExecute" :loading="executing">{{ t('develop.notebook.confirmExecute') }}</el-button>
      </template>
    </el-dialog>

    <!-- 帮助对话框 -->
    <el-dialog v-model="helpDialogVisible" :title="t('develop.notebook.helpDialogTitle')" width="600px">
      <div class="help-content">
        <h3>🚀 {{ t('develop.notebook.helpQuickStart') }}</h3>
        <p>{{ t('develop.notebook.helpQuickStartDesc') }}</p>

        <h3>📝 {{ t('develop.notebook.helpFeatures') }}</h3>
        <ul>
          <li><strong>{{ t('develop.notebook.helpFeatureCode') }}</strong>: {{ t('develop.notebook.helpFeatureCodeDesc') }}</li>
          <li><strong>{{ t('develop.notebook.helpFeatureData') }}</strong>: {{ t('develop.notebook.helpFeatureDataDesc') }}</li>
          <li><strong>{{ t('develop.notebook.helpFeatureViz') }}</strong>: {{ t('develop.notebook.helpFeatureVizDesc') }}</li>
          <li><strong>{{ t('develop.notebook.helpFeatureGeo') }}</strong>: {{ t('develop.notebook.helpFeatureGeoDesc') }}</li>
        </ul>

        <h3>⌨️ {{ t('develop.notebook.helpShortcuts') }}</h3>
        <ul>
          <li><code>Shift + Enter</code>: {{ t('develop.notebook.shortcutShiftEnter') }}</li>
          <li><code>Ctrl + Enter</code>: {{ t('develop.notebook.shortcutCtrlEnter') }}</li>
          <li><code>A</code>: {{ t('develop.notebook.shortcutA') }}</li>
          <li><code>B</code>: {{ t('develop.notebook.shortcutB') }}</li>
          <li><code>DD</code>: {{ t('develop.notebook.shortcutDD') }}</li>
          <li><code>M</code>: {{ t('develop.notebook.shortcutM') }}</li>
          <li><code>Y</code>: {{ t('develop.notebook.shortcutY') }}</li>
        </ul>

        <h3>📦 {{ t('develop.notebook.helpPreinstalled') }}</h3>
        <ul>
          <li>{{ t('develop.notebook.helpPreinstalledData') }}: pandas, numpy, scipy</li>
          <li>{{ t('develop.notebook.helpPreinstalledViz') }}: matplotlib, seaborn, plotly</li>
          <li>{{ t('develop.notebook.helpPreinstalledGeo') }}: geopandas, shapely, fiona</li>
          <li>{{ t('develop.notebook.helpPreinstalledDb') }}: psycopg2, sqlalchemy</li>
        </ul>

        <h3>💡 {{ t('develop.notebook.helpTips') }}</h3>
        <ul>
          <li>{{ t('develop.notebook.helpTip1') }}</li>
          <li>{{ t('develop.notebook.helpTip2') }}</li>
          <li>{{ t('develop.notebook.helpTip3') }}</li>
        </ul>
      </div>
      <template #footer>
        <el-button type="primary" @click="helpDialogVisible = false">
          {{ t('develop.notebook.gotIt') }}
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted, computed } from 'vue'
import { useI18n } from 'vue-i18n'
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
const { t } = useI18n()

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
  if (!minioPath) {
    // 没有 notebook 文件时，返回基础 URL，并指定租户的 kernel
    const kernelName = venvInfo.value.kernel_name
    return `${jupyterBaseUrl.value}?kernel=${kernelName}`
  }

  // 构造 Jupyter Lab URL
  // 使用 /lab/tree/ 路径打开指定文件，并通过 kernel 参数指定租户的 Kernel
  // 注意: 由于配置了 TenantKernelSpecManager，Jupyter 只会显示租户的 Kernel
  const kernelName = venvInfo.value.kernel_name
  return `${jupyterBaseUrl.value}/lab/tree/${minioPath}?kernel=${kernelName}`
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
    ElMessage.error(t('develop.notebook.loadListFailed'))
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
    ElMessage.error(t('develop.notebook.loadDataSourceFailed'))
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
    ElMessage.warning(t('develop.notebook.nameRequired'))
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

    ElMessage.success(t('develop.notebook.createSuccess'))
    createDialogVisible.value = false
    await loadNotebooks()
  } catch (error) {
    console.error('创建失败:', error)
    ElMessage.error(error.response?.data?.error || t('develop.notebook.createFailed'))
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
    ElMessage.warning(t('develop.notebook.selectFileRequired'))
    return
  }

  if (!uploadForm.value.name) {
    ElMessage.warning(t('develop.notebook.nameRequired'))
    return
  }

  uploading.value = true
  try {
    await notebookAPI.uploadNotebook(uploadForm.value.file, {
      name: uploadForm.value.name,
      description: uploadForm.value.description,
      data_sources: uploadForm.value.data_sources
    })

    ElMessage.success(t('develop.notebook.uploadSuccess'))
    uploadDialogVisible.value = false
    await loadNotebooks()
  } catch (error) {
    console.error('上传失败:', error)
    ElMessage.error(error.response?.data?.error || t('develop.notebook.uploadFailed'))
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
    ElMessage.error(t('develop.notebook.invalidParams'))
    return
  }

  executing.value = true
  try {
    // 调用统一的 dev_item 执行接口
    const response = await executeDevItem(executeNotebook.value.id, {
      parameters,
      data_source_ids: executeForm.value.data_source_ids
    })

    ElMessage.success(t('develop.notebook.executeSubmitted', { id: response.execution_id }))
    executeDialogVisible.value = false

    // 跳转到执行监控页面
    router.push({
      path: '/executions',
      query: { execution_id: response.execution_id }
    })
  } catch (error) {
    console.error('执行失败:', error)
    ElMessage.error(error.response?.data?.error || t('develop.notebook.executeFailed'))
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

    ElMessage.success(t('develop.notebook.downloadSuccess'))
  } catch (error) {
    console.error('下载失败:', error)
    ElMessage.error(t('develop.notebook.downloadFailed'))
  }
}

// 删除 Notebook
const deleteNotebook = async (notebook) => {
  try {
    await ElMessageBox.confirm(
      t('develop.notebook.deleteConfirmMsg', { name: notebook.display_name || notebook.name }),
      t('develop.notebook.deleteConfirmTitle'),
      {
        confirmButtonText: t('develop.notebook.confirm'),
        cancelButtonText: t('develop.notebook.cancel'),
        type: 'warning'
      }
    )

    await deleteDevItem(notebook.id)
    ElMessage.success(t('develop.notebook.deleteSuccess'))

    // 如果删除的是当前选中的 Notebook，清空选中状态
    if (currentNotebook.value && currentNotebook.value.id === notebook.id) {
      currentNotebook.value = null
    }

    await loadNotebooks()
  } catch (error) {
    if (error !== 'cancel') {
      console.error('删除失败:', error)
      ElMessage.error(t('develop.notebook.deleteFailed'))
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
    ElMessage.error(t('develop.notebook.checkVenvFailed'))
    venvReady.value = false
  } finally {
    checkingVenv.value = false
  }
}

// 初始化虚拟环境
const initVenvEnvironment = async () => {
  try {
    initLoading.value = true
    ElMessage.info(t('develop.notebook.initializingVenv'))

    const res = await initVenv()
    venvInfo.value = res.data?.data || res.data
    venvReady.value = true

    ElMessage.success(t('develop.notebook.venvInitSuccess'))
  } catch (error) {
    console.error('初始化虚拟环境失败:', error)
    ElMessage.error(error.response?.data?.error || t('develop.notebook.venvInitFailed'))
    venvReady.value = false
  } finally {
    initLoading.value = false
  }
}

onMounted(async () => {
  await checkVenvStatus()

  // 自动初始化虚拟环境（如果不存在）
  if (!venvReady.value) {
    await initVenvEnvironment()
  }

  await loadNotebooks()
  await loadDataSources()
})
</script>

<style scoped>
.notebook-editor {
  display: flex;
  height: 100%;
  background: var(--addp-bg-secondary);
}

.notebook-sidebar {
  background: var(--addp-bg-primary);
  border-right: 1px solid var(--addp-border-color);
  display: flex;
  flex-direction: column;
}

.sidebar-header {
  padding: 16px;
  border-bottom: 1px solid var(--addp-border-color);
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
  border: 1px solid var(--addp-border-color);
  background: var(--addp-bg-primary);
  cursor: pointer;
  transition: all 0.2s;
}

.notebook-item:hover {
  border-color: var(--el-color-primary);
  box-shadow: 0 2px 4px rgba(64, 158, 255, 0.1);
}

.notebook-item.active {
  border-color: var(--el-color-primary);
  background: var(--addp-bg-secondary);
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
  color: var(--addp-text-tertiary);
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
  background: var(--addp-bg-primary);
  border-bottom: 1px solid var(--addp-border-color);
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
  background: var(--addp-bg-primary);
}

/* 虚拟环境状态卡片 */
.venv-status-card {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--addp-bg-primary);
  padding: 40px;
}

.venv-init-card {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--addp-bg-primary);
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
  color: var(--addp-text-secondary);
}

.venv-init-tips ul {
  margin: 16px 0;
  padding-left: 20px;
  color: var(--addp-text-secondary);
}

.venv-init-tips ul li {
  margin: 8px 0;
}

.venv-init-tips .time-note {
  margin-top: 16px;
  font-size: 13px;
  color: var(--addp-text-tertiary);
}

.form-tip {
  font-size: 12px;
  color: var(--addp-text-tertiary);
  margin-top: 4px;
}

/* 帮助对话框样式 */
.help-content {
  line-height: 1.8;
}

.help-content h3 {
  margin-top: 16px;
  margin-bottom: 8px;
  color: var(--addp-text-primary);
  font-size: 16px;
}

.help-content h3:first-child {
  margin-top: 0;
}

.help-content p {
  margin: 8px 0;
  color: var(--addp-text-secondary);
}

.help-content ul {
  margin: 8px 0;
  padding-left: 24px;
  color: var(--addp-text-secondary);
}

.help-content li {
  margin: 4px 0;
}

.help-content code {
  background: var(--addp-bg-secondary);
  padding: 2px 6px;
  border-radius: 3px;
  font-family: 'Monaco', 'Courier New', monospace;
  font-size: 13px;
  color: #e83e8c;
}
</style>
