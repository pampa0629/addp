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
          <el-button size="small" @click="showUploadDialog">
            <el-icon><Upload /></el-icon> {{ t('develop.notebook.upload') }}
          </el-button>
          <el-button size="small" circle :title="t('develop.notebook.refresh')" @click="loadNotebooks">
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
            <el-tooltip :content="t('develop.notebook.edit')">
              <el-button type="primary" size="small" text @click="selectNotebook(notebook)">
                <el-icon><EditPen /></el-icon>
              </el-button>
            </el-tooltip>

            <el-tooltip :content="t('develop.notebook.execute')">
              <el-button
                type="success"
                size="small"
                text
                :disabled="!isNotebookEngineAvailable(notebook)"
                @click="showExecuteDialog(notebook)"
              >
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
                  <el-dropdown-item command="rebind">
                    <el-icon><Switch /></el-icon> {{ t('develop.notebook.changeEngine') }}
                  </el-dropdown-item>
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

    <el-main class="notebook-detail-container">
      <div v-if="!currentNotebook" class="empty-state">
        <el-empty :description="t('develop.notebook.selectHint')" />
      </div>

      <div v-else class="notebook-detail">
        <div class="detail-toolbar">
          <div class="notebook-title">
            <span class="current-notebook-name">{{ currentNotebook.display_name || currentNotebook.name }}</span>
            <el-tooltip placement="bottom-start">
              <template #content>
                <div class="notebook-runtime-tip">
                  <div>{{ t('develop.notebook.engine') }}: {{ notebookEngineLabel(currentNotebook) }}</div>
                  <div>{{ t('develop.notebook.kernel') }}: {{ currentNotebook.content?.kernel || '-' }}</div>
                </div>
              </template>
              <el-icon class="notebook-runtime-tip-icon" :aria-label="t('develop.notebook.runtimeSummary')"><InfoFilled /></el-icon>
            </el-tooltip>
          </div>
          <div class="toolbar-actions">
            <el-button size="small" :loading="sessionLoading" @click="openNotebookSession(currentNotebook, { force: true })">
              <el-icon><Refresh /></el-icon> {{ t('develop.notebook.reloadEditor') }}
            </el-button>
            <el-button
              type="primary"
              size="small"
              :disabled="!isNotebookEngineAvailable(currentNotebook)"
              @click="showExecuteDialog(currentNotebook)"
            >
              <el-icon><VideoPlay /></el-icon> {{ t('develop.notebook.execute') }}
            </el-button>
            <el-button size="small" @click="showBindingDialog(currentNotebook)">
              <el-icon><Switch /></el-icon> {{ t('develop.notebook.changeEngine') }}
            </el-button>
            <el-button size="small" @click="downloadNotebook(currentNotebook)">
              <el-icon><Download /></el-icon> {{ t('develop.notebook.download') }}
            </el-button>
            <el-button size="small" @click="viewHistory(currentNotebook)">
              <el-icon><Clock /></el-icon> {{ t('develop.notebook.history') }}
            </el-button>
          </div>
        </div>
        <div class="notebook-workspace" v-loading="sessionLoading">
          <iframe
            v-if="notebookSession"
            ref="notebookFrameRef"
            :key="notebookSession.id"
            class="notebook-frame"
            :src="notebookSession.url"
            :title="currentNotebook.display_name || currentNotebook.name"
          />
          <el-result v-else-if="sessionError" icon="error" :title="t('develop.notebook.editorUnavailable')" :sub-title="sessionError">
            <template #extra>
              <el-button type="primary" @click="openNotebookSession(currentNotebook, { force: true })">
                {{ t('develop.notebook.retryEditor') }}
              </el-button>
            </template>
          </el-result>
        </div>
      </div>
    </el-main>

    <el-dialog
      v-model="createDialogVisible"
      class="addp-dialog"
      :title="t('develop.notebook.createDialogTitle')"
      width="min(600px, calc(100vw - 24px))"
      @closed="handleCreateDialogClosed"
    >
      <el-form :model="createForm" label-position="top">
        <el-form-item :label="t('develop.notebook.fieldName')" required>
          <el-input v-model="createForm.name" :placeholder="t('develop.notebook.namePlaceholder')" autofocus />
        </el-form-item>
        <el-form-item :label="t('develop.notebook.engine')" required>
          <el-select v-model="createForm.engine_id" :placeholder="t('develop.notebook.selectEngine')" :loading="enginesLoading" class="full-width" @change="handleCreateEngineChange">
            <el-option v-for="engine in notebookEngines" :key="engine.id" :label="engine.name" :value="engine.id" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('develop.notebook.kernel')" required>
          <el-select v-model="createForm.kernel" :placeholder="t('develop.notebook.selectKernel')" :loading="createKernelsLoading" :disabled="!createForm.engine_id" class="full-width">
            <el-option v-for="kernel in createKernels" :key="kernel.name" :label="kernel.display_name || kernel.name" :value="kernel.name" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('develop.notebook.fieldDescription')">
          <el-input v-model="createForm.description" type="textarea" :rows="3" :placeholder="t('develop.notebook.descriptionPlaceholder')" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createDialogVisible = false">{{ t('develop.notebook.cancel') }}</el-button>
        <el-button type="primary" :loading="creating" :disabled="notebookEngines.length === 0" @click="confirmCreate">{{ t('develop.notebook.confirmCreate') }}</el-button>
      </template>
    </el-dialog>

    <!-- 上传 Notebook 对话框 -->
    <el-dialog
      v-model="uploadDialogVisible"
      class="addp-dialog"
      :title="t('develop.notebook.uploadDialogTitle')"
      width="min(600px, calc(100vw - 24px))"
      @closed="handleUploadDialogClosed"
    >
      <el-form :model="uploadForm" label-position="top">
        <el-form-item :label="t('develop.notebook.engine')" required>
          <el-select
            v-model="uploadForm.engine_id"
            :placeholder="t('develop.notebook.selectEngine')"
            :loading="enginesLoading"
            style="width: 100%"
            @change="handleUploadEngineChange"
          >
            <el-option
              v-for="engine in notebookEngines"
              :key="engine.id"
              :label="engine.name"
              :value="engine.id"
            />
          </el-select>
          <div v-if="!enginesLoading && notebookEngines.length === 0" class="form-status error">
            {{ t('develop.notebook.noEngineAvailable') }}
          </div>
        </el-form-item>

        <el-form-item :label="t('develop.notebook.kernel')" required>
          <el-select
            v-model="uploadForm.kernel"
            :placeholder="t('develop.notebook.selectKernel')"
            :loading="kernelsLoading"
            :disabled="!uploadForm.engine_id"
            style="width: 100%"
          >
            <el-option
              v-for="kernel in kernels"
              :key="kernel.name"
              :label="kernel.display_name || kernel.name"
              :value="kernel.name"
            />
          </el-select>
          <div v-if="uploadForm.engine_id && !kernelsLoading && kernels.length === 0" class="form-status error">
            {{ t('develop.notebook.noKernelAvailable') }}
          </div>
        </el-form-item>

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

      </el-form>

      <template #footer>
        <el-button @click="uploadDialogVisible = false">{{ t('develop.notebook.cancel') }}</el-button>
        <el-button
          type="primary"
          :disabled="notebookEngines.length === 0"
          :loading="uploading"
          @click="confirmUpload"
        >{{ t('develop.notebook.confirmUpload') }}</el-button>
      </template>
    </el-dialog>

    <!-- Notebook 运行时绑定对话框 -->
    <el-dialog
      v-model="bindingDialogVisible"
      class="addp-dialog"
      :title="t('develop.notebook.changeEngineDialogTitle')"
      width="min(520px, calc(100vw - 24px))"
      :close-on-click-modal="!rebinding"
      :close-on-press-escape="!rebinding"
      :show-close="!rebinding"
      @closed="resetBindingDialog"
    >
      <el-form :model="bindingForm" label-position="top">
        <el-form-item :label="t('develop.notebook.currentBinding')">
          <el-input :model-value="notebookRuntimeBindingLabel(bindingNotebook)" disabled />
        </el-form-item>

        <el-form-item :label="t('develop.notebook.targetEngine')" required>
          <el-select
            v-model="bindingForm.engine_id"
            :placeholder="t('develop.notebook.selectEngine')"
            :loading="enginesLoading"
            :disabled="rebinding"
            style="width: 100%"
            @change="handleBindingEngineChange"
          >
            <el-option
              v-for="engine in notebookEngines"
              :key="engine.id"
              :label="engine.name"
              :value="engine.id"
            />
          </el-select>
          <div v-if="!enginesLoading && notebookEngines.length === 0" class="form-status error">
            {{ t('develop.notebook.noEngineAvailable') }}
          </div>
        </el-form-item>

        <el-form-item :label="t('develop.notebook.kernel')" required>
          <el-select
            v-model="bindingForm.kernel"
            :placeholder="t('develop.notebook.selectKernel')"
            :loading="bindingKernelsLoading"
            :disabled="!bindingForm.engine_id || rebinding"
            style="width: 100%"
          >
            <el-option
              v-for="kernel in bindingKernels"
              :key="kernel.name"
              :label="kernel.display_name || kernel.name"
              :value="kernel.name"
            />
          </el-select>
          <div
            v-if="bindingForm.engine_id && !bindingKernelsLoading && bindingKernels.length === 0"
            class="form-status error"
          >
            {{ t('develop.notebook.noKernelAvailable') }}
          </div>
        </el-form-item>

        <el-alert
          :title="t('develop.notebook.runtimeBindingEffect')"
          type="info"
          :closable="false"
          show-icon
        />
      </el-form>

      <template #footer>
        <el-button :disabled="rebinding" @click="bindingDialogVisible = false">
          {{ t('develop.notebook.cancel') }}
        </el-button>
        <el-button
          type="primary"
          :loading="rebinding"
          :disabled="!bindingForm.engine_id || !bindingForm.kernel || bindingKernelsLoading"
          @click="confirmRuntimeBinding"
        >
          {{ t('develop.notebook.confirmChangeEngine') }}
        </el-button>
      </template>
    </el-dialog>

    <!-- 执行 Notebook 对话框 -->
    <el-dialog
      v-model="executeDialogVisible"
      class="addp-dialog"
      :title="t('develop.notebook.executeDialogTitle')"
      width="min(600px, calc(100vw - 24px))"
    >
      <el-form :model="executeForm" label-position="top">
        <el-form-item :label="t('develop.notebook.notebook')">
          <el-input :value="executeNotebook?.display_name || executeNotebook?.name" disabled />
        </el-form-item>

        <el-form-item :label="t('develop.notebook.engine')">
          <el-input :value="notebookEngineLabel(executeNotebook)" disabled />
        </el-form-item>

        <el-form-item :label="t('develop.notebook.fieldParams')">
          <el-input
            v-model="executeForm.parameters"
            type="textarea"
            :rows="5"
            :placeholder="t('develop.notebook.parametersPlaceholder')"
          />
        </el-form-item>

      </el-form>

      <template #footer>
        <el-button @click="executeDialogVisible = false">{{ t('develop.notebook.cancel') }}</el-button>
        <el-button type="primary" @click="confirmExecute" :loading="executing">{{ t('develop.notebook.confirmExecute') }}</el-button>
      </template>
    </el-dialog>

    <div class="notebook-ai-fab-wrapper">
      <transition name="notebook-ai-slide">
        <div v-if="copilotVisible" class="notebook-ai-panel">
          <div class="notebook-ai-panel-header">
            <span class="notebook-ai-panel-title">{{ t('develop.notebook.copilotTitle') }}</span>
            <el-button
              circle
              text
              size="small"
              :aria-label="t('develop.notebook.closeCopilot')"
              :disabled="copilotLoading || copilotInserting"
              @click="copilotVisible = false"
            >
              <el-icon><Close /></el-icon>
            </el-button>
          </div>
          <div class="notebook-copilot-body" v-loading="copilotLoading">
            <el-input
              v-model="copilotQuery"
              type="textarea"
              :rows="4"
              :placeholder="t('develop.notebook.copilotPlaceholder')"
              :disabled="copilotLoading"
            />

            <div v-if="copilotCandidates.length" class="copilot-candidates">
              <div v-for="role in copilotRoles" :key="role" class="copilot-role">
                <div class="copilot-role-title">{{ role }}</div>
                <el-radio-group v-model="copilotSelections[role]" class="copilot-radio-group">
                  <el-radio
                    v-for="candidate in candidatesForRole(role)"
                    :key="candidate.candidate_id"
                    :value="candidate.candidate_id"
                    border
                    class="copilot-candidate"
                  >
                    <span class="copilot-candidate-name">{{ candidate.name }}</span>
                    <span class="copilot-candidate-path">
                      {{ candidate.engine_name }} / {{ candidate.path_names.join(' / ') }}
                    </span>
                    <span v-if="candidate.recommended" class="copilot-recommended">
                      {{ t('develop.notebook.copilotRecommended') }}
                    </span>
                    <span
                      v-if="candidate.recommended && candidate.recommendation_reason"
                      class="copilot-recommendation-reason"
                    >
                      {{ candidate.recommendation_reason }}
                    </span>
                  </el-radio>
                </el-radio-group>
              </div>
            </div>

            <el-alert
              v-if="copilotMessage"
              :title="copilotMessage"
              :type="copilotCandidates.length ? 'info' : 'warning'"
              :closable="false"
              show-icon
            />

            <div v-if="copilotCode" class="copilot-code-section">
              <div class="copilot-code-title">{{ t('develop.notebook.copilotCode') }}</div>
              <el-input v-model="copilotCode" type="textarea" :rows="16" resize="vertical" />
              <p v-if="copilotExplanation" class="copilot-explanation">{{ copilotExplanation }}</p>
            </div>
          </div>

          <div class="copilot-footer">
            <el-button :disabled="copilotLoading || copilotInserting" @click="copilotVisible = false">
              {{ t('develop.notebook.cancel') }}
            </el-button>
            <el-button
              v-if="!copilotCode"
              type="primary"
              :loading="copilotLoading"
              :disabled="!copilotQuery.trim()"
              @click="generateNotebookCell"
            >
              <el-icon><MagicStick /></el-icon>
              {{ copilotCandidates.length ? t('develop.notebook.copilotGenerateCode') : t('develop.notebook.copilotFindData') }}
            </el-button>
            <el-button
              v-else
              type="primary"
              :loading="copilotInserting"
              :disabled="!copilotCode.trim()"
              @click="insertCopilotCell"
            >
              <el-icon><Plus /></el-icon> {{ t('develop.notebook.copilotInsertCell') }}
            </el-button>
          </div>
        </div>
      </transition>
      <el-tooltip
        :content="t('develop.notebook.copilotTitle')"
        placement="left"
        :disabled="copilotVisible"
      >
        <el-button
          class="notebook-ai-fab"
          circle
          type="primary"
          :aria-label="t('develop.notebook.copilotTitle')"
          :disabled="!currentNotebook || !notebookSession || copilotLoading"
          @click="openNotebookCopilot"
        >
          <el-icon><MagicStick /></el-icon>
        </el-button>
      </el-tooltip>
    </div>
  </div>
</template>

<script setup>
import { computed, ref, onMounted, onBeforeUnmount, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Close, Upload, Refresh, Search, EditPen, VideoPlay, Clock, More, Download, Delete, Switch, Plus, MagicStick, InfoFilled } from '@element-plus/icons-vue'
import { notebookAPI } from '@/api/notebook'
import { deleteDevTask, executeDevTask, getDevTask } from '@/api/devTask'
import { openMonitorExecution } from '@addp/common-frontend'
import { useRoute, useRouter } from 'vue-router'
import {
  buildDevelopTaskEditorLocation,
  buildDevelopTaskPageLocation,
  developTaskIDFromRoute
} from '@/utils/developTaskRoute'
import {
  navigateDevelopRoute,
  navigateDevelopTaskEditor
} from '@/utils/developNavigation'
import dayjs from 'dayjs'

const router = useRouter()
const route = useRoute()
const { t } = useI18n()

// 列表相关
const notebooks = ref([])
const currentNotebook = ref(null)
const loading = ref(false)
const currentPage = ref(1)
const pageSize = ref(20)
const total = ref(0)
const searchKeyword = ref('')
const notebookEngines = ref([])
const enginesLoading = ref(false)
const kernels = ref([])
const kernelsLoading = ref(false)
const notebookSession = ref(null)
const sessionLoading = ref(false)
const sessionError = ref('')
const notebookFrameRef = ref(null)
const copilotVisible = ref(false)
const copilotLoading = ref(false)
const copilotInserting = ref(false)
const copilotQuery = ref('')
const copilotCandidates = ref([])
const copilotSelections = ref({})
const copilotCode = ref('')
const copilotExplanation = ref('')
const copilotMessage = ref('')
const copilotRoles = computed(() => [...new Set(copilotCandidates.value.map(candidate => candidate.role))])

const createDialogVisible = ref(false)
const creating = ref(false)
const createKernels = ref([])
const createKernelsLoading = ref(false)
const createForm = ref({ name: '', description: '', engine_id: null, kernel: '' })

// 上传对话框
const uploadDialogVisible = ref(false)
const uploading = ref(false)
const uploadRef = ref(null)
const uploadForm = ref({
  file: null,
  name: '',
  description: '',
  engine_id: null,
  kernel: ''
})

// 运行时绑定对话框
const bindingDialogVisible = ref(false)
const rebinding = ref(false)
const bindingNotebook = ref(null)
const bindingKernels = ref([])
const bindingKernelsLoading = ref(false)
const bindingForm = ref({
  engine_id: null,
  kernel: ''
})
let bindingKernelRequestSequence = 0

// 执行对话框
const executeDialogVisible = ref(false)
const executing = ref(false)
const executeNotebook = ref(null)
const executeForm = ref({
  parameters: '{}'
})

const loadNotebookEngines = async () => {
  enginesLoading.value = true
  try {
    const response = await notebookAPI.listNotebookEngines()
    notebookEngines.value = Array.isArray(response) ? response : []
  } catch (error) {
    console.error('加载 Notebook 引擎失败:', error)
    notebookEngines.value = []
    ElMessage.error(error.response?.data?.error || t('develop.notebook.loadEnginesFailed'))
  } finally {
    enginesLoading.value = false
  }
}

const loadKernels = async (engineId) => {
  kernels.value = []
  uploadForm.value.kernel = ''
  if (!engineId) return

  kernelsLoading.value = true
  try {
    const response = await notebookAPI.listKernels(engineId)
    kernels.value = response.kernels || []
    const preferred = kernels.value.find(kernel => kernel.name === 'python3') || kernels.value[0]
    uploadForm.value.kernel = preferred?.name || ''
  } catch (error) {
    console.error('加载 Kernel 失败:', error)
    ElMessage.error(error.response?.data?.error || t('develop.notebook.loadKernelsFailed'))
  } finally {
    kernelsLoading.value = false
  }
}

const handleUploadEngineChange = (engineId) => loadKernels(engineId)

const loadCreateKernels = async (engineId) => {
  createKernels.value = []
  createForm.value.kernel = ''
  if (!engineId) return
  createKernelsLoading.value = true
  try {
    const response = await notebookAPI.listKernels(engineId)
    createKernels.value = response.kernels || []
    createForm.value.kernel = (createKernels.value.find(kernel => kernel.name === 'python3') || createKernels.value[0])?.name || ''
  } catch (error) {
    ElMessage.error(error.response?.data?.error || t('develop.notebook.loadKernelsFailed'))
  } finally {
    createKernelsLoading.value = false
  }
}

const handleCreateEngineChange = (engineId) => loadCreateKernels(engineId)

const showCreateDialog = async (options = {}) => {
  if (options.updateRoute !== false) {
    await navigateDevelopTaskEditor(router, 'script')
    return
  }
  createForm.value = { name: '', description: '', engine_id: notebookEngines.value[0]?.id || null, kernel: '' }
  createDialogVisible.value = true
  if (createForm.value.engine_id) await loadCreateKernels(createForm.value.engine_id)
}

const handleCreateDialogClosed = async () => {
  if (String(route.query.action || '') === 'create') {
    await navigateDevelopRoute(router, { path: '/notebook' }, { history: 'replace' })
  }
}

const confirmCreate = async () => {
  if (!createForm.value.name.trim()) return ElMessage.warning(t('develop.notebook.nameRequired'))
  if (!createForm.value.engine_id) return ElMessage.warning(t('develop.notebook.engineRequired'))
  if (!createForm.value.kernel) return ElMessage.warning(t('develop.notebook.kernelRequired'))
  creating.value = true
  try {
    const response = await notebookAPI.createNotebook({ ...createForm.value, name: createForm.value.name.trim() })
    createDialogVisible.value = false
    await loadNotebooks()
    const task = response.dev_task
    if (task?.id) {
      await selectNotebook(task, { updateRoute: false })
      await navigateDevelopTaskEditor(router, 'script', task.id, { history: 'replace' })
      await openNotebookSession(task)
    }
    ElMessage.success(t('develop.notebook.createSuccess'))
  } catch (error) {
    ElMessage.error(error.response?.data?.error || t('develop.notebook.createFailed'))
  } finally {
    creating.value = false
  }
}

const loadBindingKernels = async (engineId) => {
  const requestSequence = ++bindingKernelRequestSequence
  bindingKernels.value = []
  bindingForm.value.kernel = ''
  if (!engineId) {
    bindingKernelsLoading.value = false
    return
  }

  bindingKernelsLoading.value = true
  try {
    const response = await notebookAPI.listKernels(engineId)
    if (requestSequence !== bindingKernelRequestSequence) return
    bindingKernels.value = response.kernels || []
  } catch (error) {
    if (requestSequence !== bindingKernelRequestSequence) return
    console.error('加载重绑定 Kernel 失败:', error)
    ElMessage.error(error.response?.data?.error || t('develop.notebook.loadKernelsFailed'))
  } finally {
    if (requestSequence === bindingKernelRequestSequence) {
      bindingKernelsLoading.value = false
    }
  }
}

const handleBindingEngineChange = (engineId) => loadBindingKernels(engineId)

// 加载 Notebook 列表
const loadNotebooks = async () => {
  loading.value = true
  try {
    const params = {
      page: currentPage.value,
      page_size: pageSize.value
    }

    if (searchKeyword.value) {
      params.keyword = searchKeyword.value
    }

    const response = await notebookAPI.listNotebooks(params)
    notebooks.value = response.items || []
    total.value = response.total || 0
  } catch (error) {
    console.error('加载 Notebook 列表失败:', error)
    ElMessage.error(t('develop.notebook.loadListFailed'))
  } finally {
    loading.value = false
  }
}

// 选择 Notebook
const selectNotebook = async (notebook, options = {}) => {
  if (String(currentNotebook.value?.id || '') !== String(notebook.id)) await closeNotebookSession()
  currentNotebook.value = notebook
  if (options.updateRoute !== false) {
    await navigateDevelopTaskEditor(router, 'script', notebook.id)
  }
  await openNotebookSession(notebook)
}

const selectNotebookByID = async (id) => {
  if (!id) return

  const existing = notebooks.value.find(item => String(item.id) === String(id))
  if (existing) {
    await selectNotebook(existing, { updateRoute: false })
    return
  }

  try {
    const task = await getDevTask(id)
    if (task.dev_type !== 'script') {
      ElMessage.warning(t('develop.notebook.taskNotScript'))
      return
    }
    currentNotebook.value = task
    if (!notebooks.value.some(item => String(item.id) === String(task.id))) {
      notebooks.value.unshift(task)
    }
    await openNotebookSession(task)
  } catch (error) {
    console.error('加载脚本任务失败:', error)
    ElMessage.error(error.response?.data?.error || t('develop.notebook.loadTaskFailed'))
  }
}

// 显示上传对话框
const showUploadDialog = async (options = {}) => {
  uploadForm.value = {
    file: null,
    name: '',
    description: '',
    engine_id: notebookEngines.value[0]?.id || null,
    kernel: ''
  }
  kernels.value = []
  uploadDialogVisible.value = true
  if (uploadForm.value.engine_id) {
    await loadKernels(uploadForm.value.engine_id)
  }
}

const handleUploadDialogClosed = async () => {
  uploadRef.value?.clearFiles()
}

// 文件选择改变
const handleFileChange = (file) => {
  uploadForm.value.file = file.raw
  if (!uploadForm.value.name) {
    uploadForm.value.name = file.name.replace('.ipynb', '')
  }

  if (!uploadForm.value.engine_id) {
    ElMessage.warning(t('develop.notebook.engineRequired'))
    return
  }

  if (!uploadForm.value.kernel) {
    ElMessage.warning(t('develop.notebook.kernelRequired'))
    return
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
    const response = await notebookAPI.uploadNotebook(uploadForm.value.file, {
      name: uploadForm.value.name,
      description: uploadForm.value.description,
      engine_id: uploadForm.value.engine_id,
      kernel: uploadForm.value.kernel
    })

    ElMessage.success(t('develop.notebook.uploadSuccess'))
    uploadDialogVisible.value = false
    await loadNotebooks()
    const uploadedTask = response.dev_task
    if (uploadedTask?.id) {
      await selectNotebook(uploadedTask, { updateRoute: false })
      await navigateDevelopTaskEditor(router, 'script', uploadedTask.id, { history: 'replace' })
      await openNotebookSession(uploadedTask)
    }
  } catch (error) {
    console.error('上传失败:', error)
    ElMessage.error(error.response?.data?.error || t('develop.notebook.uploadFailed'))
  } finally {
    uploading.value = false
  }
}

const showBindingDialog = async (notebook) => {
  bindingNotebook.value = notebook
  bindingForm.value = {
    engine_id: null,
    kernel: ''
  }
  bindingKernels.value = []
  bindingDialogVisible.value = true
  if (notebookEngines.value.length === 0 && !enginesLoading.value) {
    await loadNotebookEngines()
  }
}

const resetBindingDialog = () => {
  bindingKernelRequestSequence += 1
  bindingNotebook.value = null
  bindingKernels.value = []
  bindingKernelsLoading.value = false
  bindingForm.value = {
    engine_id: null,
    kernel: ''
  }
}

const replaceNotebookState = (updated) => {
  const index = notebooks.value.findIndex(notebook => String(notebook.id) === String(updated.id))
  if (index >= 0) {
    notebooks.value.splice(index, 1, updated)
  }
  if (String(currentNotebook.value?.id) === String(updated.id)) {
    currentNotebook.value = updated
  }
}

const confirmRuntimeBinding = async () => {
  if (!bindingNotebook.value || !bindingForm.value.engine_id) {
    ElMessage.warning(t('develop.notebook.engineRequired'))
    return
  }
  if (!bindingForm.value.kernel) {
    ElMessage.warning(t('develop.notebook.kernelRequired'))
    return
  }

  rebinding.value = true
  try {
    const updated = await notebookAPI.updateRuntimeBinding(bindingNotebook.value.id, {
      engine_id: bindingForm.value.engine_id,
      kernel: bindingForm.value.kernel
    })
    replaceNotebookState(updated)
    ElMessage.success(t('develop.notebook.runtimeBindingUpdated'))
    bindingDialogVisible.value = false
  } catch (error) {
    console.error('更换 Notebook 引擎失败:', error)
    ElMessage.error(error.response?.data?.error || t('develop.notebook.runtimeBindingUpdateFailed'))
  } finally {
    rebinding.value = false
  }
}

// 显示执行对话框
const showExecuteDialog = (notebook) => {
  if (!isNotebookEngineAvailable(notebook)) {
    ElMessage.warning(t('develop.notebook.boundEngineUnavailable'))
    return
  }
  executeNotebook.value = notebook
  executeForm.value = {
    parameters: JSON.stringify(notebook.content?.parameters || {}, null, 2)
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
    // 调用统一开发任务执行接口
    const response = await executeDevTask(executeNotebook.value.id, {
      parameters
    })

    ElMessage.success(t('develop.notebook.executeSubmitted', { id: response.execution_id }))
    executeDialogVisible.value = false

    await openMonitorExecution(response.execution_id)
  } catch (error) {
    console.error('执行失败:', error)
    ElMessage.error(error.response?.data?.error || t('develop.notebook.executeFailed'))
  } finally {
    executing.value = false
  }
}

// 查看执行历史
const viewHistory = (notebook) => {
  return navigateDevelopRoute(router, {
    path: '/executions',
    query: { source_task_id: notebook.id, dev_type: 'script' }
  })
}

// 下拉菜单命令处理
const handleCommand = async (command, notebook) => {
  switch (command) {
    case 'rebind':
      await showBindingDialog(notebook)
      break
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
        confirmButtonText: t('develop.notebook.deleteConfirmAction'),
        cancelButtonText: t('develop.notebook.cancel'),
        type: 'warning',
        customClass: 'addp-message-box',
        confirmButtonClass: 'el-button--danger'
      }
    )

    await deleteDevTask(notebook.id)
    ElMessage.success(t('develop.notebook.deleteSuccess'))

    // 如果删除的是当前选中的 Notebook，清空选中状态
    if (currentNotebook.value && currentNotebook.value.id === notebook.id) {
      currentNotebook.value = null
      await navigateDevelopRoute(router, { path: '/notebook' }, { history: 'replace' })
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

const formatParameters = (parameters) => JSON.stringify(parameters || {}, null, 2)

const closeNotebookSession = async () => {
  const active = notebookSession.value
  notebookSession.value = null
  sessionError.value = ''
  resetNotebookCopilot()
  if (!active) return
  try {
    await notebookAPI.closeSession(active.task_id, active.id)
  } catch (error) {
    if (error.response?.status !== 404) console.error('关闭 Notebook 会话失败:', error)
  }
}

const resetNotebookCopilot = () => {
  copilotLoading.value = false
  copilotInserting.value = false
  copilotQuery.value = ''
  copilotCandidates.value = []
  copilotSelections.value = {}
  copilotCode.value = ''
  copilotExplanation.value = ''
  copilotMessage.value = ''
}

const openNotebookCopilot = () => {
  if (!notebookSession.value) return
  copilotVisible.value = true
}

const candidatesForRole = role => copilotCandidates.value.filter(candidate => candidate.role === role)

const generateNotebookCell = async () => {
  if (!notebookSession.value || !copilotQuery.value.trim()) return
  const payload = {
    query: copilotQuery.value.trim(),
    kernel: currentNotebook.value?.content?.kernel || 'python3'
  }
  if (copilotCandidates.value.length) {
    const missingRole = copilotRoles.value.find(role => !copilotSelections.value[role])
    if (missingRole) {
      ElMessage.warning(t('develop.notebook.copilotSelectEachRole'))
      return
    }
    payload.selections = copilotRoles.value.map(role => {
      const candidate = copilotCandidates.value.find(item => item.candidate_id === copilotSelections.value[role])
      return {
        candidate_id: candidate.candidate_id,
        role,
        engine_id: candidate.engine_id,
        path: candidate.path
      }
    })
  }

  copilotLoading.value = true
  try {
    const response = await notebookAPI.generateSessionCell(notebookSession.value.id, payload)
    copilotMessage.value = response.message || ''
    if (response.status === 'need_confirmation') {
      copilotCandidates.value = response.candidates || []
      copilotSelections.value = {}
      for (const role of [...new Set(copilotCandidates.value.map(candidate => candidate.role))]) {
        const candidates = copilotCandidates.value.filter(candidate => candidate.role === role)
        const recommended = candidates.find(candidate => candidate.recommended)
        if (recommended) copilotSelections.value[role] = recommended.candidate_id
      }
      return
    }
    if (response.status === 'success') {
      copilotCode.value = response.code || ''
      copilotExplanation.value = response.explanation || ''
      copilotCandidates.value = []
      copilotMessage.value = ''
      return
    }
    copilotCandidates.value = []
  } catch (error) {
    ElMessage.error(error.response?.data?.error || t('develop.notebook.copilotFailed'))
  } finally {
    copilotLoading.value = false
  }
}

const postNotebookBridgeMessage = (message, timeoutMs = 8000) => new Promise((resolve, reject) => {
  const frameWindow = notebookFrameRef.value?.contentWindow
  if (!frameWindow) {
    reject(new Error('Notebook frame is unavailable'))
    return
  }
  const requestId = `${Date.now()}-${Math.random().toString(16).slice(2)}`
  const timer = window.setTimeout(() => {
    window.removeEventListener('message', handleResult)
    reject(new Error('Notebook bridge timed out'))
  }, timeoutMs)
  const handleResult = event => {
    if (event.origin !== window.location.origin || event.source !== frameWindow) return
    if (event.data?.type !== 'addp:notebook:insert-cell:result' || event.data?.requestId !== requestId) return
    window.clearTimeout(timer)
    window.removeEventListener('message', handleResult)
    if (event.data.ok) resolve()
    else reject(new Error(event.data.error || 'Notebook bridge rejected the cell'))
  }
  window.addEventListener('message', handleResult)
  frameWindow.postMessage({ ...message, requestId }, window.location.origin)
})

const insertCopilotCell = async () => {
  if (!notebookSession.value || !copilotCode.value.trim()) return
  copilotInserting.value = true
  try {
    await postNotebookBridgeMessage({
      type: 'addp:notebook:insert-cell',
      sessionId: notebookSession.value.id,
      code: copilotCode.value
    })
    ElMessage.success(t('develop.notebook.copilotInserted'))
    copilotVisible.value = false
  } catch (error) {
    ElMessage.error(t('develop.notebook.copilotInsertFailed'))
  } finally {
    copilotInserting.value = false
  }
}

const openNotebookSession = async (notebook, options = {}) => {
  if (!notebook?.id || sessionLoading.value) return
  if (notebookSession.value?.task_id === notebook.id && !options.force) return
  if (options.force) await closeNotebookSession()
  sessionLoading.value = true
  sessionError.value = ''
  try {
    notebookSession.value = await notebookAPI.createSession(notebook.id)
  } catch (error) {
    notebookSession.value = null
    sessionError.value = error.response?.data?.error || t('develop.notebook.openEditorFailed')
  } finally {
    sessionLoading.value = false
  }
}

const notebookEngineID = (notebook) => Number(notebook?.execution_config?.engine_id || 0)

const findNotebookEngine = (notebook) => {
  const engineId = notebookEngineID(notebook)
  return notebookEngines.value.find(engine => Number(engine.id) === engineId)
}

const isNotebookEngineAvailable = (notebook) => Boolean(findNotebookEngine(notebook))

const notebookEngineLabel = (notebook) => {
  const engine = findNotebookEngine(notebook)
  if (engine) return engine.name
  const engineId = notebookEngineID(notebook)
  return engineId ? t('develop.notebook.unavailableEngine', { id: engineId }) : t('develop.notebook.engineNotBound')
}

const notebookRuntimeBindingLabel = (notebook) => {
  if (!notebook) return '-'
  const kernel = notebook.content?.kernel || '-'
  return `${notebookEngineLabel(notebook)} / ${kernel}`
}

const notebookTaskRouteReady = ref(false)
let applyingNotebookTaskRoute = false

async function applyNotebookTaskRoute() {
  if (applyingNotebookTaskRoute) return
  applyingNotebookTaskRoute = true
  try {
    const taskId = developTaskIDFromRoute(route)
    const action = String(route.query.action || '')
    const canonicalLocation = taskId || action === 'create'
      ? buildDevelopTaskEditorLocation('script', taskId)
      : buildDevelopTaskPageLocation('script')
    if (route.fullPath !== router.resolve(canonicalLocation).fullPath) {
      await navigateDevelopTaskEditor(router, 'script', taskId, { history: 'replace' })
    }

    if (taskId) {
      uploadDialogVisible.value = false
      if (String(currentNotebook.value?.id || '') !== taskId) await selectNotebookByID(taskId)
    } else if (action === 'create') {
      await closeNotebookSession()
      currentNotebook.value = null
      await showCreateDialog({ updateRoute: false })
    } else {
      await closeNotebookSession()
      uploadDialogVisible.value = false
      createDialogVisible.value = false
      currentNotebook.value = null
    }
  } catch (error) {
    ElMessage.error(error.response?.data?.error || t('develop.notebook.loadTaskFailed'))
  } finally {
    applyingNotebookTaskRoute = false
  }
}

onMounted(async () => {
  await Promise.all([loadNotebookEngines(), loadNotebooks()])
  await applyNotebookTaskRoute()
  notebookTaskRouteReady.value = true
})

watch(() => route.fullPath, () => {
  if (notebookTaskRouteReady.value) applyNotebookTaskRoute()
})

onBeforeUnmount(() => {
  closeNotebookSession()
})
</script>

<style scoped>
.notebook-editor {
  display: flex;
  height: 100%;
  min-height: 0;
  background: var(--addp-bg-secondary);
  overflow: hidden;
}

.notebook-sidebar {
  background: var(--addp-bg-primary);
  border-right: 1px solid var(--addp-border-color);
  display: flex;
  flex-direction: column;
  min-height: 0;
  overflow: hidden;
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
  min-height: 0;
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

.notebook-detail-container {
  display: flex;
  flex-direction: column;
  padding: 0;
  min-width: 0;
  min-height: 0;
  overflow: hidden;
}

.empty-state {
  display: flex;
  align-items: center;
  justify-content: center;
  height: 100%;
}

.notebook-detail {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
  overflow: hidden;
}

.detail-toolbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  flex-shrink: 0;
  padding: 12px 16px;
  background: var(--addp-bg-primary);
  border-bottom: 1px solid var(--addp-border-color);
}

.notebook-title {
  display: inline-flex;
  align-items: center;
  min-width: 0;
  gap: 6px;
}

.current-notebook-name {
  font-weight: 500;
  font-size: 14px;
  overflow-wrap: anywhere;
}

.notebook-runtime-tip-icon {
  flex-shrink: 0;
  color: var(--addp-text-tertiary);
  cursor: help;
}

.notebook-runtime-tip {
  line-height: 1.6;
}

.toolbar-actions {
  display: flex;
  gap: 8px;
}

.notebook-workspace {
  flex: 1;
  min-height: 0;
  overflow: hidden;
  background: var(--addp-bg-primary);
}

.notebook-frame {
  display: block;
  width: 100%;
  height: 100%;
  border: 0;
  background: var(--addp-bg-primary);
}

.notebook-ai-fab-wrapper {
  position: fixed;
  right: 22px;
  bottom: 32px;
  z-index: 1000;
  display: flex;
  align-items: flex-end;
  gap: 10px;
}

.notebook-ai-fab {
  width: 42px;
  height: 42px;
  flex-shrink: 0;
  box-shadow: var(--addp-shadow-hover);
}

.notebook-ai-panel {
  width: min(520px, calc(100vw - 92px));
  max-height: min(78vh, 760px);
  padding: 14px;
  display: flex;
  flex-direction: column;
  gap: 10px;
  overflow: hidden;
  background: var(--addp-bg-primary);
  border: 1px solid var(--addp-border-color);
  border-radius: 8px;
  box-shadow: var(--addp-shadow-card);
}

.notebook-ai-panel-header,
.copilot-footer {
  display: flex;
  align-items: center;
  gap: 8px;
}

.notebook-ai-panel-header {
  justify-content: space-between;
}

.copilot-footer {
  justify-content: flex-end;
}

.notebook-ai-panel-title {
  color: var(--addp-text-primary);
  font-weight: 600;
}

.notebook-ai-slide-enter-active,
.notebook-ai-slide-leave-active {
  transition: opacity 0.16s ease, transform 0.16s ease;
}

.notebook-ai-slide-enter-from,
.notebook-ai-slide-leave-to {
  opacity: 0;
  transform: translateY(8px);
}

.notebook-copilot-body,
.copilot-candidates,
.copilot-role,
.copilot-radio-group,
.copilot-code-section {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.notebook-copilot-body {
  min-height: 0;
  overflow: auto;
  padding-right: 2px;
}

.copilot-role-title,
.copilot-code-title {
  font-size: 14px;
  font-weight: 600;
  color: var(--addp-text-primary);
}

.copilot-candidate {
  width: 100%;
  height: auto;
  min-height: 58px;
  margin: 0 !important;
  padding: 10px 12px;
}

.copilot-candidate :deep(.el-radio__label) {
  display: grid;
  min-width: 0;
  gap: 4px;
  white-space: normal;
}

.copilot-candidate-name {
  font-weight: 500;
  overflow-wrap: anywhere;
}

.copilot-candidate-path,
.copilot-explanation {
  margin: 0;
  color: var(--addp-text-secondary);
  font-size: 12px;
  line-height: 1.5;
  overflow-wrap: anywhere;
}

.copilot-recommended {
  color: var(--el-color-primary);
  font-size: 12px;
}

.copilot-recommendation-reason {
  color: var(--el-color-success);
  font-size: 12px;
  line-height: 1.5;
  overflow-wrap: anywhere;
}

.full-width { width: 100%; }

.parameter-section {
  margin-top: 20px;
}

.form-status {
  width: 100%;
  margin-top: 4px;
  font-size: 12px;
  line-height: 1.5;
}

.form-status.error {
  color: var(--el-color-danger);
}

.parameter-section h4 {
  margin: 0 0 8px;
  font-size: 16px;
  font-weight: 600;
}

.parameter-section pre {
  margin: 0;
  padding: 12px;
  background: var(--addp-bg-secondary);
  border: 1px solid var(--addp-border-color);
  border-radius: 4px;
  color: var(--addp-text-primary);
  font-family: monospace;
  font-size: 13px;
  line-height: 1.5;
  overflow-x: auto;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}

@media (max-width: 900px) {
  .notebook-editor {
    flex-direction: column;
    overflow-y: auto;
  }

  .notebook-sidebar {
    width: 100% !important;
    max-height: 45vh;
    border-right: 0;
    border-bottom: 1px solid var(--addp-border-color);
  }

  .detail-toolbar {
    align-items: flex-start;
    gap: 12px;
    flex-direction: column;
  }

  .toolbar-actions {
    flex-wrap: wrap;
  }
}
</style>
