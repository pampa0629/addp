<template>
  <div class="query-workbench">
    <header class="workbench-toolbar">
      <div class="toolbar-primary">
        <el-button
          v-if="isCompact"
          circle
          :aria-label="t('develop.query.dataResources')"
          @click="catalogDrawerVisible = true"
        >
          <el-icon><Menu /></el-icon>
        </el-button>
        <h2>{{ currentTaskName || t('develop.query.title') }}</h2>
        <el-select
          ref="queryEngineSelectRef"
          :model-value="selectedQueryTarget"
          class="engine-select"
          :placeholder="t('develop.query.selectDataSource')"
          :disabled="executing || loadingSampleQuery || switchingQueryTarget || savingForEngineSwitch"
          @change="requestQueryTargetChange"
        >
          <el-option
            v-if="selectedEngineUnavailable"
            :label="t('develop.query.engineUnavailable', { id: selectedEngineId })"
            :value="selectedQueryTarget"
            disabled
          />
          <el-option
            v-for="target in queryTargets"
            :key="target.value"
            :label="target.label"
            :value="target.value"
          >
            <span>{{ target.name }}</span>
            <span class="engine-type">{{ target.typeLabel }}</span>
          </el-option>
        </el-select>
        <el-tag v-if="currentQueryLanguage" size="small" effect="plain">
          {{ currentQueryLanguage.toUpperCase() }}
        </el-tag>
      </div>

      <div class="toolbar-actions">
        <el-tooltip :content="t('develop.query.testConnection')">
          <el-button
            circle
            :loading="testingConnection"
            :disabled="!selectedTarget || switchingQueryTarget || savingForEngineSwitch"
            :aria-label="t('develop.query.testConnection')"
            @click="handleTestConnection"
          >
            <el-icon><Connection /></el-icon>
          </el-button>
        </el-tooltip>
        <el-tooltip :content="t('develop.query.generateQueryTemplate')">
          <el-button
            circle
            :loading="loadingSampleQuery"
            :disabled="!selectedTarget || executing || switchingQueryTarget || savingForEngineSwitch"
            :aria-label="t('develop.query.generateQueryTemplate')"
            @click="generateQueryTemplate"
          >
            <el-icon><DocumentAdd /></el-icon>
          </el-button>
        </el-tooltip>
        <el-tooltip :content="queryParametersSupported ? t('develop.query.queryParameters') : t('develop.query.queryParametersUnsupported')">
          <el-button
            circle
            :disabled="!queryParametersSupported || executing || switchingQueryTarget || savingForEngineSwitch"
            :aria-label="t('develop.query.queryParameters')"
            @click="parameterDrawerVisible = true"
          >
            <el-icon><Key /></el-icon>
          </el-button>
        </el-tooltip>
        <el-tooltip :content="t('develop.query.format')">
          <el-button
            circle
            :disabled="!formatterLanguage || !queryContent || executing || switchingQueryTarget || savingForEngineSwitch"
            :aria-label="t('develop.query.format')"
            @click="formatQuery"
          >
            <el-icon><MagicStick /></el-icon>
          </el-button>
        </el-tooltip>
        <el-button
          :disabled="!selectedTarget || !queryContent.trim() || executing || switchingQueryTarget || savingForEngineSwitch"
          @click="handlePersistQueryTask"
        >
          <el-icon><FolderAdd /></el-icon>
          {{ currentTaskId ? t('develop.query.updateTask') : t('develop.query.saveAsTask') }}
        </el-button>
        <el-button
          type="primary"
          :loading="executing"
          :disabled="loadingSampleQuery || !selectedTarget || !queryContent.trim() || switchingQueryTarget || savingForEngineSwitch"
          @click="executeQuery"
        >
          <el-icon><VideoPlay /></el-icon>
          {{ selectedText ? t('develop.query.executeSelection') : t('develop.query.execute') }}
        </el-button>
      </div>
    </header>

    <main class="workbench-body">
      <aside v-if="!isCompact" class="catalog-panel" :style="{ width: `${catalogWidth}px` }">
        <div class="catalog-heading">
          <span>{{ t('develop.query.dataResources') }}</span>
          <el-tooltip :content="t('develop.query.generateQueryTemplate')">
            <el-button
              circle
              size="small"
              :disabled="!selectedTarget || loadingSampleQuery || executing || switchingQueryTarget || savingForEngineSwitch"
              :aria-label="t('develop.query.generateQueryTemplate')"
              @click="generateQueryTemplate"
            >
              <el-icon><DocumentAdd /></el-icon>
            </el-button>
          </el-tooltip>
        </div>
        <ResourceTreePicker
          v-if="selectedEngineId"
          v-model="catalogSelection"
          class="catalog-tree"
          :engine-id="selectedEngineId"
          :initial-locator="initialCatalogLocator"
          :show-engine-selector="false"
          :show-selection-summary="false"
          :show-count="false"
          :title="''"
          mode="item"
          tree-height="100%"
          @select="rememberCatalogSelection"
          @node-dblclick="insertCatalogItemAtCursor"
        />
        <el-empty v-else :description="t('develop.query.selectDataSourceFirst')" :image-size="64" />
      </aside>

      <div
        v-if="!isCompact"
        class="resize-handle vertical"
        role="separator"
        tabindex="0"
        aria-orientation="vertical"
        @mousedown="startCatalogResize"
        @keydown="handleCatalogResizeKeydown"
      />

      <section class="query-surface">
        <div class="editor-panel" :style="{ height: `${editorHeight}px` }">
          <div class="panel-heading">
            <span><el-icon><Edit /></el-icon>{{ t('develop.query.editorTitle') }}</span>
            <span v-if="isDirty" class="dirty-indicator">{{ t('develop.query.unsaved') }}</span>
          </div>
          <div v-loading="loadingSampleQuery" class="editor-content" :aria-busy="loadingSampleQuery">
            <MonacoEditor
              ref="editorRef"
              v-model="queryContent"
              :language="monacoLanguage"
              :completions="catalogCompletions"
              theme="vs-dark"
              @execute="executeQuery"
              @selection-change="selectedText = $event"
            />
          </div>
        </div>

        <div
          class="resize-handle horizontal"
          role="separator"
          tabindex="0"
          aria-orientation="horizontal"
          @mousedown="startEditorResize"
          @keydown="handleEditorResizeKeydown"
        />

        <div class="result-panel">
          <div class="panel-heading">
            <span><el-icon><List /></el-icon>{{ t('develop.query.resultTitle') }}</span>
            <div class="result-actions">
              <el-radio-group v-if="hasGraphData" v-model="resultViewMode" size="small">
                <el-radio-button value="table">{{ t('develop.query.tableView') }}</el-radio-button>
                <el-radio-button value="graph">{{ t('develop.query.graphView') }}</el-radio-button>
              </el-radio-group>
              <el-tooltip v-if="executionResult && !executing" :content="t('develop.query.clearResult')">
                <el-button circle size="small" :aria-label="t('develop.query.clearResult')" @click="clearResult">
                  <el-icon><Close /></el-icon>
                </el-button>
              </el-tooltip>
            </div>
          </div>
          <div class="result-content">
            <QueryResult
              :result="executionResult"
              :custom-content="resultViewMode === 'graph' && hasGraphData"
            >
              <GraphResultView
                v-if="resultViewMode === 'graph' && hasGraphData"
                class="graph-result-view"
                :graph-data="executionResult.graph_data"
              />
            </QueryResult>
          </div>
        </div>
      </section>
    </main>

    <el-drawer
      v-if="isCompact"
      v-model="catalogDrawerVisible"
      :title="t('develop.query.dataResources')"
      direction="ltr"
      size="min(88vw, 380px)"
    >
      <div class="drawer-catalog-actions">
        <el-button
          type="primary"
          :disabled="!selectedTarget || loadingSampleQuery || executing || switchingQueryTarget || savingForEngineSwitch"
          @click="generateQueryTemplate"
        >
          <el-icon><DocumentAdd /></el-icon>
          {{ t('develop.query.generateQueryTemplate') }}
        </el-button>
      </div>
      <ResourceTreePicker
        v-if="selectedEngineId"
        v-model="catalogSelection"
        :engine-id="selectedEngineId"
        :initial-locator="initialCatalogLocator"
        :show-engine-selector="false"
        :show-selection-summary="false"
        :show-count="false"
        :title="''"
        mode="item"
        tree-height="calc(100vh - 150px)"
        @select="rememberCatalogSelection"
        @node-dblclick="insertCatalogItemAtCursor"
      />
    </el-drawer>

    <el-drawer
      v-model="parameterDrawerVisible"
      :title="t('develop.query.queryParameters')"
      direction="rtl"
      size="min(92vw, 560px)"
      :close-on-click-modal="false"
    >
      <div class="parameter-toolbar">
        <el-button type="primary" plain :disabled="!queryParametersSupported" @click="addQueryParameter">
          <el-icon><Plus /></el-icon>
          {{ t('develop.query.addQueryParameter') }}
        </el-button>
      </div>
      <div class="parameter-list">
        <div v-for="(parameter, index) in queryParameters" :key="parameter.id" class="parameter-item">
          <div class="parameter-item-heading">
            <strong>{{ parameter.name || t('develop.query.unnamedParameter') }}</strong>
            <div class="parameter-item-actions">
              <el-tooltip :content="t('develop.query.insertParameterReference')">
                <el-button
                  circle
                  size="small"
                  :disabled="!parameter.name"
                  :aria-label="t('develop.query.insertParameterReference')"
                  @click="insertQueryParameterReference(parameter)"
                >
                  <el-icon><Position /></el-icon>
                </el-button>
              </el-tooltip>
              <el-tooltip :content="t('develop.query.removeQueryParameter')">
                <el-button
                  circle
                  size="small"
                  type="danger"
                  plain
                  :aria-label="t('develop.query.removeQueryParameter')"
                  @click="removeQueryParameter(index)"
                >
                  <el-icon><Delete /></el-icon>
                </el-button>
              </el-tooltip>
            </div>
          </div>
          <div class="parameter-grid">
            <el-form-item :label="t('develop.query.parameterName')" :error="queryParameterNameError(parameter, index)">
              <el-input v-model="parameter.name" maxlength="64" />
            </el-form-item>
            <el-form-item :label="t('develop.query.parameterType')">
              <el-select v-model="parameter.type" @change="resetQueryParameterDefault(parameter)">
                <el-option
                  v-for="type in queryParameterTypes"
                  :key="type"
                  :label="t(`develop.query.parameterTypes.${type}`)"
                  :value="type"
                />
              </el-select>
            </el-form-item>
            <el-form-item :label="t('develop.query.parameterDefault')">
              <el-switch v-if="parameter.type === 'boolean'" v-model="parameter.default" />
              <el-input-number
                v-else-if="parameter.type === 'integer' || parameter.type === 'number'"
                v-model="parameter.default"
                :precision="parameter.type === 'integer' ? 0 : undefined"
                :step="parameter.type === 'integer' ? 1 : 0.1"
                controls-position="right"
              />
              <el-input v-else v-model="parameter.default" />
            </el-form-item>
            <el-form-item :label="t('develop.query.parameterTitle')">
              <el-input v-model="parameter.title" maxlength="100" />
            </el-form-item>
            <el-form-item class="parameter-description" :label="t('develop.query.parameterDescription')">
              <el-input v-model="parameter.description" type="textarea" :rows="2" maxlength="300" />
            </el-form-item>
          </div>
        </div>
        <el-empty v-if="queryParameters.length === 0" :description="t('develop.query.noQueryParameters')" :image-size="56" />
      </div>
    </el-drawer>

    <el-dialog
      v-model="executionParameterDialogVisible"
      class="addp-dialog"
      :title="t('develop.query.executionParameters')"
      width="min(680px, calc(100vw - 24px))"
      :close-on-click-modal="false"
    >
      <ExecutionParameterForm
        ref="executionParameterFormRef"
        v-model="executionParameterOverrides"
        :contract="queryExecutionContract"
        :disabled="executing"
      />
      <template #footer>
        <el-button :disabled="executing" @click="executionParameterDialogVisible = false">{{ t('develop.query.cancel') }}</el-button>
        <el-button type="primary" :loading="executing" @click="submitQuery(executionParameterOverrides)">
          <el-icon><VideoPlay /></el-icon>
          {{ t('develop.query.execute') }}
        </el-button>
      </template>
    </el-dialog>

    <el-dialog
      v-model="queryEngineSwitchDialogVisible"
      class="addp-dialog"
      :title="t('develop.query.engineSwitchTitle')"
      width="min(520px, calc(100vw - 24px))"
      :close-on-click-modal="false"
      :close-on-press-escape="!switchingQueryTarget && !savingForEngineSwitch"
      :show-close="!switchingQueryTarget && !savingForEngineSwitch"
    >
      <el-alert
        :title="t('develop.query.engineSwitchMessage', { name: pendingQueryTargetInfo?.name || '-' })"
        type="warning"
        :closable="false"
        show-icon
      />
      <template #footer>
        <el-button :disabled="switchingQueryTarget || savingForEngineSwitch" @click="cancelQueryTargetChange">
          {{ t('develop.query.cancel') }}
        </el-button>
        <el-button type="danger" plain :loading="switchingQueryTarget" :disabled="savingForEngineSwitch" @click="clearAndSwitchQueryTarget">
          {{ t('develop.query.clearAndSwitch') }}
        </el-button>
        <el-button type="primary" :loading="savingForEngineSwitch" :disabled="switchingQueryTarget" @click="saveAndSwitchQueryTarget">
          {{ t('develop.query.saveAndClear') }}
        </el-button>
      </template>
    </el-dialog>

    <SaveQueryDialog
      v-model="showSaveDialog"
      :engine-id="selectedEngineId"
      :sql="queryContent"
      @update:model-value="handleSaveDialogVisibility"
      @saved="handleSaveTask"
    />
    <StatusAnnouncer :message="announcement" />
  </div>
</template>

<script setup>
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { onBeforeRouteLeave, onBeforeRouteUpdate, useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  Close,
  Connection,
  DocumentAdd,
  Delete,
  Edit,
  FolderAdd,
  Key,
  List,
  MagicStick,
  Menu,
  Plus,
  Position,
  VideoPlay
} from '@element-plus/icons-vue'
import { format } from 'sql-formatter'
import {
  ResourceTreePicker,
  ExecutionParameterForm,
  StatusAnnouncer,
  parseLocator,
  useResizable
} from '@common-ui'
import { GraphResultView } from '@addp/common-frontend/graph'
import MonacoEditor from '../components/MonacoEditor.vue'
import QueryResult from '../components/QueryResult.vue'
import SaveQueryDialog from '../components/SaveQueryDialog.vue'
import { getSampleQuery, saveQueryTask, testConnection, updateQueryTask } from '../api/query.js'
import { createExecution, getExecution } from '../api/execution.js'
import { listEngines } from '../api/engines.js'
import { getDevTask } from '../api/devTask.js'
import {
  buildDevelopTaskEditorLocation,
  developTaskIDFromRoute
} from '@/utils/developTaskRoute'
import { navigateDevelopTaskEditor } from '@/utils/developNavigation'
import { createLatestRequestCoordinator } from '@common-ui'
import {
  formatterLanguageForQuery,
  buildQueryExecutionContract,
  isTerminalExecutionStatus,
  monacoLanguageForQuery,
  queryParameterReference,
  queryCapabilityForEngine,
  queryResultFromExecution
} from '@/utils/queryWorkbench.mjs'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()

const currentTaskId = ref(null)
const currentTaskName = ref('')
const currentTask = ref(null)
const selectedQueryTarget = ref('')
const engines = ref([])
const queryContent = ref('')
const currentQueryLanguage = ref('')
const executionResult = ref(null)
const executing = ref(false)
const testingConnection = ref(false)
const loadingSampleQuery = ref(false)
const editorRef = ref(null)
const selectedText = ref('')
const showSaveDialog = ref(false)
const queryEngineSelectRef = ref(null)
const queryEngineSwitchDialogVisible = ref(false)
const pendingQueryTarget = ref('')
const switchingQueryTarget = ref(false)
const savingForEngineSwitch = ref(false)
const saveForEngineSwitch = ref(false)
const resultViewMode = ref('table')
const catalogSelection = ref(null)
const targetLocator = ref('')
const initialCatalogLocator = ref('')
const catalogCompletions = ref([])
const catalogDrawerVisible = ref(false)
const parameterDrawerVisible = ref(false)
const queryParameters = ref([])
const executionParameterDialogVisible = ref(false)
const executionParameterOverrides = ref({})
const executionParameterFormRef = ref(null)
const isCompact = ref(false)
const announcement = ref('')
const savedSnapshot = ref('')
const queryTaskRouteReady = ref(false)
const bypassUnsavedRouteConfirm = ref(false)
const sampleRequests = createLatestRequestCoordinator()
let executionRequestSequence = 0
let mediaQuery = null
let compactMediaListener = null
let applyingQueryTaskRoute = false

const {
  size: catalogWidth,
  startResize: startCatalogResize,
  handleResizeKeydown: handleCatalogResizeKeydown
} = useResizable(300, 240, 480, 'horizontal')
const {
  size: editorHeight,
  startResize: startEditorResize,
  handleResizeKeydown: handleEditorResizeKeydown
} = useResizable(390, 220, 720, 'vertical')

const queryTargets = computed(() => engines.value.map(engine => ({
  value: `engine:${engine.id}`,
  name: engine.name,
  label: `${engine.name} (${engine.engine_type})`,
  typeLabel: engine.engine_type,
  engine
})))
const selectedTarget = computed(() => queryTargets.value.find(target => target.value === selectedQueryTarget.value) || null)
const pendingQueryTargetInfo = computed(() => (
  queryTargets.value.find(target => target.value === pendingQueryTarget.value) || null
))
const selectedEngineId = computed(() => {
  if (selectedTarget.value) return selectedTarget.value.engine.id
  const match = String(selectedQueryTarget.value).match(/^engine:(\d+)$/)
  return match ? Number(match[1]) : null
})
const selectedEngineUnavailable = computed(() => Boolean(selectedQueryTarget.value && !selectedTarget.value))
const selectedCapability = computed(() => queryCapabilityForEngine(selectedTarget.value?.engine))
const queryParametersSupported = computed(() => Boolean(
  selectedCapability.value.parameters?.supported &&
  selectedCapability.value.parameters.languages.includes(currentQueryLanguage.value)
))
const queryParameterTypes = computed(() => selectedCapability.value.parameters?.types || [])
const queryExecutionContract = computed(() => buildQueryExecutionContract(queryParameters.value))
const monacoLanguage = computed(() => monacoLanguageForQuery(currentQueryLanguage.value))
const formatterLanguage = computed(() => formatterLanguageForQuery(currentQueryLanguage.value))
const hasGraphData = computed(() => {
  const graph = executionResult.value?.graph_data
  return Boolean(graph?.nodes?.length || graph?.relationships?.length)
})
const currentSnapshot = computed(() => JSON.stringify({
  engine_id: selectedEngineId.value,
  language: currentQueryLanguage.value,
  query: queryContent.value,
  target_locator: catalogSelection.value?.identity?.locator || targetLocator.value || '',
  query_parameters: queryParameters.value.map(queryParameterPayload)
}))
const isDirty = computed(() => queryTaskRouteReady.value && savedSnapshot.value !== currentSnapshot.value)

const markSaved = () => {
  savedSnapshot.value = currentSnapshot.value
}

const loadEngines = async () => {
  try {
    const response = await listEngines()
    engines.value = Array.isArray(response) ? response : []
    if (!selectedQueryTarget.value && queryTargets.value.length) {
      selectedQueryTarget.value = queryTargets.value[0].value
    }
  } catch (error) {
    engines.value = []
    ElMessage.error(t('develop.query.loadEnginesFailed') + (error.response?.data?.error || error.message))
  }
}

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

const loadSampleQuery = async ({ replace = false, locator = '' } = {}) => {
  if (!selectedTarget.value || loadingSampleQuery.value) return
  if ((queryContent.value.trim() || queryParameters.value.length > 0) && !replace) {
    try {
      await ElMessageBox.confirm(
        t('develop.query.replaceSampleConfirm'),
        t('develop.query.replaceSampleTitle'),
        {
          confirmButtonText: t('develop.query.replace'),
          cancelButtonText: t('develop.query.cancel'),
          type: 'warning',
          customClass: 'addp-message-box'
        }
      )
    } catch {
      return
    }
  }
  const request = sampleRequests.begin(`${selectedQueryTarget.value}:${locator}`)
  loadingSampleQuery.value = true
  try {
    const sample = await getSampleQuery(selectedEngineId.value, locator)
    if (!sampleRequests.isCurrent(request, `${selectedQueryTarget.value}:${locator}`)) return
    queryContent.value = sample.query
    queryParameters.value = []
    executionParameterOverrides.value = {}
    if (locator) targetLocator.value = locator
    currentQueryLanguage.value = String(sample.language || selectedCapability.value.defaultLanguage).toLowerCase()
    clearResult()
    announcement.value = t(locator ? 'develop.query.queryTemplateGenerated' : 'develop.query.sampleLoaded')
  } catch (error) {
    if (sampleRequests.isCurrent(request, `${selectedQueryTarget.value}:${locator}`)) {
      ElMessage.error(error.response?.data?.error || error.message)
    }
  } finally {
    if (sampleRequests.isCurrent(request, `${selectedQueryTarget.value}:${locator}`)) {
      loadingSampleQuery.value = false
    }
  }
}

async function requestQueryTargetChange(targetValue) {
  queryEngineSelectRef.value?.blur()
  if (executing.value || loadingSampleQuery.value || switchingQueryTarget.value || savingForEngineSwitch.value) return
  const target = queryTargets.value.find(item => item.value === targetValue)
  if (!target || targetValue === selectedQueryTarget.value) return
  if ((!queryContent.value.trim() && queryParameters.value.length === 0) || !isDirty.value) {
    await applyQueryTargetSwitch(targetValue)
    return
  }
  pendingQueryTarget.value = targetValue
  queryEngineSwitchDialogVisible.value = true
}

function cancelQueryTargetChange() {
  if (switchingQueryTarget.value || savingForEngineSwitch.value) return
  queryEngineSwitchDialogVisible.value = false
  pendingQueryTarget.value = ''
}

async function clearAndSwitchQueryTarget() {
  if (switchingQueryTarget.value || savingForEngineSwitch.value || !pendingQueryTarget.value) return
  await applyQueryTargetSwitch(pendingQueryTarget.value)
}

async function saveAndSwitchQueryTarget() {
  if (switchingQueryTarget.value || savingForEngineSwitch.value || !pendingQueryTarget.value) return
  if (!currentTaskId.value) {
    saveForEngineSwitch.value = true
    queryEngineSwitchDialogVisible.value = false
    showSaveDialog.value = true
    return
  }
  savingForEngineSwitch.value = true
  try {
    if (await persistCurrentQueryTask()) {
      await applyQueryTargetSwitch(pendingQueryTarget.value, { saved: true })
    }
  } finally {
    savingForEngineSwitch.value = false
  }
}

async function applyQueryTargetSwitch(targetValue, { saved = false } = {}) {
  const target = queryTargets.value.find(item => item.value === targetValue)
  if (!target) return
  switchingQueryTarget.value = true
  queryEngineSwitchDialogVisible.value = false
  pendingQueryTarget.value = ''
  executionRequestSequence += 1
  sampleRequests.invalidate()
  selectedQueryTarget.value = targetValue
  catalogSelection.value = null
  targetLocator.value = ''
  initialCatalogLocator.value = ''
  queryContent.value = ''
  queryParameters.value = []
  executionParameterOverrides.value = {}
  parameterDrawerVisible.value = false
  currentQueryLanguage.value = queryCapabilityForEngine(target.engine).defaultLanguage
  clearResult()
  currentTaskId.value = null
  currentTaskName.value = ''
  currentTask.value = null
  try {
    applyingQueryTaskRoute = true
    bypassUnsavedRouteConfirm.value = true
    try {
      await navigateDevelopTaskEditor(router, 'query', '', { history: 'replace' })
    } finally {
      bypassUnsavedRouteConfirm.value = false
    }
    markSaved()
    ElMessage.success(t(
      saved ? 'develop.query.saveAndSwitchSuccess' : 'develop.query.engineSwitchSuccess',
      { name: target.name }
    ))
  } finally {
    applyingQueryTaskRoute = false
    switchingQueryTarget.value = false
  }
}

const queryParameterPayload = parameter => ({
  name: String(parameter?.name || '').trim(),
  type: parameter?.type,
  default: parameter?.default,
  ...(String(parameter?.title || '').trim() ? { title: String(parameter.title).trim() } : {}),
  ...(String(parameter?.description || '').trim() ? { description: String(parameter.description).trim() } : {})
})

const queryParameterNameError = (parameter, index) => {
  const name = String(parameter?.name || '').trim()
  if (!name) return t('develop.query.parameterNameRequired')
  if (!/^[A-Za-z_][A-Za-z0-9_]*$/.test(name)) return t('develop.query.parameterNameInvalid')
  if (queryParameters.value.some((item, itemIndex) => itemIndex !== index && String(item.name || '').trim() === name)) {
    return t('develop.query.parameterNameDuplicate')
  }
  return ''
}

const hasValidQueryParameters = () => queryParameters.value.every((parameter, index) => !queryParameterNameError(parameter, index))

const defaultValueForQueryParameterType = type => {
  if (type === 'boolean') return false
  if (type === 'integer' || type === 'number') return 0
  return ''
}

const addQueryParameter = () => {
  const type = queryParameterTypes.value[0] || 'string'
  queryParameters.value.push({
    id: `${Date.now()}-${queryParameters.value.length}`,
    name: '',
    type,
    default: defaultValueForQueryParameterType(type),
    title: '',
    description: ''
  })
}

const removeQueryParameter = index => {
  queryParameters.value.splice(index, 1)
}

const resetQueryParameterDefault = parameter => {
  parameter.default = defaultValueForQueryParameterType(parameter.type)
}

const insertQueryParameterReference = parameter => {
  const reference = queryParameterReference(currentQueryLanguage.value, parameter.name)
  if (!reference) return
  editorRef.value?.insertText(reference)
  parameterDrawerVisible.value = false
}

const executeQuery = async () => {
  if (loadingSampleQuery.value || executing.value) return
  if (!selectedTarget.value) {
    ElMessage.warning(t('develop.query.selectDataSourceFirst'))
    return
  }
  const selected = editorRef.value?.getSelection()?.trim() || ''
  const query = selected || queryContent.value.trim()
  if (!query) {
    ElMessage.warning(t('develop.query.enterQueryFirst'))
    return
  }

  if (!hasValidQueryParameters()) {
    parameterDrawerVisible.value = true
    ElMessage.warning(t('develop.query.queryParametersInvalid'))
    return
  }
  if (queryParameters.value.length > 0) {
    executionParameterOverrides.value = {}
    executionParameterDialogVisible.value = true
    window.setTimeout(() => executionParameterFormRef.value?.focus(), 0)
    return
  }
  await submitQuery({})
}

const submitQuery = async (parameters = {}) => {
  if (loadingSampleQuery.value || executing.value) return
  const selected = editorRef.value?.getSelection()?.trim() || ''
  const query = selected || queryContent.value.trim()
  if (!selectedTarget.value || !query) return

  const requestSequence = ++executionRequestSequence
  executing.value = true
  executionParameterDialogVisible.value = false
  resultViewMode.value = 'table'
  try {
    const started = await createExecution({
      dev_type: 'query',
      trigger_type: 'manual',
      content: {
        query,
        query_type: currentQueryLanguage.value,
        target_locator: catalogSelection.value?.identity?.locator || targetLocator.value || undefined,
        query_parameters: queryParameters.value.map(queryParameterPayload)
      },
      execution_config: { engine_id: selectedEngineId.value },
      parameters,
      timeout: 120
    })
    if (!started?.execution_id) throw new Error(t('develop.query.executionIdMissing'))
    executionResult.value = {
      status: 'pending', progress: 0, execution_id: started.execution_id,
      rows: [], columns: []
    }
    announcement.value = t('develop.query.executionSubmitted')

    while (requestSequence === executionRequestSequence) {
      const execution = await getExecution(started.execution_id)
      if (requestSequence !== executionRequestSequence) return
      executionResult.value = queryResultFromExecution(execution)
      if (hasGraphData.value) resultViewMode.value = 'graph'
      if (isTerminalExecutionStatus(execution.status)) {
        if (execution.status === 'success') {
          ElMessage.success(t('develop.query.executeSuccess'))
          announcement.value = t('develop.query.executeSuccess')
        } else {
          ElMessage.error(t('develop.query.executeFailed'))
          announcement.value = executionResult.value.error || t('develop.query.executeFailed')
        }
        break
      }
      await new Promise(resolve => window.setTimeout(resolve, 700))
    }
  } catch (error) {
    const responseError = error.response?.data
    const errorMessage = responseError?.details || responseError?.detail || responseError?.error || error.message
    executionResult.value = { success: false, status: 'failed', error: errorMessage, rows: [], columns: [] }
    ElMessage.error(t('develop.query.executeFailed'))
    announcement.value = errorMessage
  } finally {
    if (requestSequence === executionRequestSequence) executing.value = false
  }
}

const formatQuery = () => {
  if (!formatterLanguage.value || !queryContent.value) return
  try {
    queryContent.value = format(queryContent.value, {
      language: formatterLanguage.value,
      indent: '  ',
      keywordCase: 'upper',
      linesBetweenQueries: 2
    })
    ElMessage.success(t('develop.query.formatSuccess'))
  } catch (error) {
    ElMessage.error(t('develop.query.formatFailed') + error.message)
  }
}

const clearResult = () => {
  executionResult.value = null
  resultViewMode.value = 'table'
}

const rememberCatalogSelection = (selection) => {
  const path = selection?.display?.path
  if (!path) return
  if (selection?.identity?.locator) targetLocator.value = selection.identity.locator
  const next = {
    label: selection.display.label || path,
    insertText: path,
    detail: selection.display.type || ''
  }
  catalogCompletions.value = [next, ...catalogCompletions.value.filter(item => item.insertText !== path)].slice(0, 100)
}

const quoteQueryIdentifier = (value, quote) => {
  const text = String(value || '').trim()
  if (!text) return ''
  return `${quote}${text.replaceAll(quote, `${quote}${quote}`)}${quote}`
}

const queryTextForCatalogSelection = (selection) => {
  const locator = selection?.identity?.locator
  if (!locator) return ''
  let parsed
  try {
    parsed = parseLocator(locator)
  } catch {
    return selection.display?.path || ''
  }
  const segments = Array.isArray(parsed.path) ? parsed.path.filter(Boolean) : []
  if (segments.length === 0) return ''
  const engineType = String(selectedTarget.value?.engine?.engine_type || '').toLowerCase()
  if (engineType === 'mongodb' || selection.resource?.type === 'collection') {
    return JSON.stringify(segments.at(-1))
  }
  if (engineType === 'neo4j' || selection.resource?.type === 'graph') {
    return quoteQueryIdentifier(segments.at(-1), '`')
  }
  const quote = engineType === 'mysql' ? '`' : '"'
  return segments.map(segment => quoteQueryIdentifier(segment, quote)).join('.')
}

const insertCatalogItemAtCursor = (selection) => {
  const text = queryTextForCatalogSelection(selection)
  if (!text) return
  rememberCatalogSelection(selection)
  editorRef.value?.insertText(text)
}

const generateQueryTemplate = async () => {
  const locator = catalogSelection.value?.identity?.locator || ''
  if (!locator) {
    ElMessage.warning(t('develop.query.selectResourceForQueryTemplate'))
    return
  }
  catalogDrawerVisible.value = false
  await loadSampleQuery({ locator })
}

const handleSaveTask = async (taskData) => {
  if (!hasValidQueryParameters()) {
    parameterDrawerVisible.value = true
    ElMessage.warning(t('develop.query.queryParametersInvalid'))
    return
  }
  try {
    const task = await saveQueryTask({
      ...taskData,
      query_type: currentQueryLanguage.value,
      target_locator: catalogSelection.value?.identity?.locator || targetLocator.value || '',
      query_parameters: queryParameters.value.map(queryParameterPayload)
    })
    currentTaskId.value = task.id
    currentTaskName.value = task.name
    currentTask.value = task
    markSaved()
    await navigateDevelopTaskEditor(router, 'query', task.id, { history: 'replace' })
    ElMessage.success(t('develop.query.saveTaskSuccess'))
    showSaveDialog.value = false
    if (saveForEngineSwitch.value && pendingQueryTarget.value) {
      const targetValue = pendingQueryTarget.value
      saveForEngineSwitch.value = false
      await applyQueryTargetSwitch(targetValue, { saved: true })
    }
  } catch (error) {
    saveForEngineSwitch.value = false
    pendingQueryTarget.value = ''
    ElMessage.error(t('develop.query.saveTaskFailed') + (error.response?.data?.error || error.message))
  }
}

const handleSaveDialogVisibility = (visible) => {
  showSaveDialog.value = visible
  if (!visible && saveForEngineSwitch.value) {
    saveForEngineSwitch.value = false
    pendingQueryTarget.value = ''
  }
}

const handlePersistQueryTask = async () => {
  if (!hasValidQueryParameters()) {
    parameterDrawerVisible.value = true
    ElMessage.warning(t('develop.query.queryParametersInvalid'))
    return
  }
  if (!currentTaskId.value) {
    showSaveDialog.value = true
    return
  }
  await persistCurrentQueryTask()
}

const persistCurrentQueryTask = async () => {
  if (!currentTaskId.value) return false
  if (!hasValidQueryParameters()) {
    parameterDrawerVisible.value = true
    ElMessage.warning(t('develop.query.queryParametersInvalid'))
    return false
  }
  try {
    const task = currentTask.value || {}
    const updated = await updateQueryTask(currentTaskId.value, {
      name: task.name || currentTaskName.value,
      display_name: task.display_name || currentTaskName.value,
      engine_id: selectedEngineId.value,
      query: queryContent.value,
      query_type: currentQueryLanguage.value,
      target_locator: catalogSelection.value?.identity?.locator || targetLocator.value || '',
      query_parameters: queryParameters.value.map(queryParameterPayload),
      description: task.description,
      tags: task.tags || [],
      timeout: task.timeout
    })
    currentTask.value = updated
    currentTaskName.value = updated.name
    markSaved()
    ElMessage.success(t('develop.query.updateTaskSuccess'))
    return true
  } catch (error) {
    ElMessage.error(t('develop.query.updateTaskFailed') + (error.response?.data?.error || error.message))
    return false
  }
}

const loadTask = async (taskId) => {
  const task = await getDevTask(taskId)
  currentTaskId.value = task.id
  currentTaskName.value = task.name
  currentTask.value = task
  queryContent.value = task.content?.query || ''
  queryParameters.value = (Array.isArray(task.content?.query_parameters) ? task.content.query_parameters : []).map((parameter, index) => ({
    id: `saved-${index}-${parameter.name}`,
    name: parameter.name || '',
    type: parameter.type || 'string',
    default: parameter.default,
    title: parameter.title || '',
    description: parameter.description || ''
  }))
  executionParameterOverrides.value = {}
  currentQueryLanguage.value = String(task.content?.query_type || '').toLowerCase()
  const engineID = task.execution_config?.engine_id
  selectedQueryTarget.value = engineID ? `engine:${engineID}` : ''
  targetLocator.value = task.content?.target_locator || ''
  initialCatalogLocator.value = targetLocator.value
  catalogSelection.value = null
  clearResult()
  if (!queryContent.value) ElMessage.warning(t('develop.query.taskNoSql'))
}

const resetQueryEditorForCreate = async () => {
  currentTaskId.value = null
  currentTaskName.value = ''
  currentTask.value = null
  queryContent.value = ''
  queryParameters.value = []
  executionParameterOverrides.value = {}
  targetLocator.value = ''
  initialCatalogLocator.value = ''
  clearResult()
  if (!selectedTarget.value && queryTargets.value.length) {
    selectedQueryTarget.value = queryTargets.value[0].value
  }
  currentQueryLanguage.value = selectedCapability.value.defaultLanguage
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
    if (taskId) await loadTask(taskId)
    else await resetQueryEditorForCreate()
    markSaved()
  } catch (error) {
    ElMessage.error(t('develop.query.loadTaskFailed') + error.message)
  } finally {
    applyingQueryTaskRoute = false
  }
}

const confirmUnsavedRouteChange = async () => {
  if (bypassUnsavedRouteConfirm.value) return true
  if (!isDirty.value) return true
  try {
    await ElMessageBox.confirm(
      t('develop.query.unsavedConfirm'),
      t('develop.query.unsavedTitle'),
      {
        confirmButtonText: t('develop.query.leave'),
        cancelButtonText: t('develop.query.cancel'),
        type: 'warning',
        customClass: 'addp-message-box'
      }
    )
    return true
  } catch {
    return false
  }
}

const handleBeforeUnload = (event) => {
  if (!isDirty.value) return
  event.preventDefault()
  event.returnValue = ''
}

onBeforeRouteLeave(confirmUnsavedRouteChange)
onBeforeRouteUpdate((to, from) => {
  if (String(to.query.id || '') === String(from.query.id || '')) return true
  return confirmUnsavedRouteChange()
})

onMounted(async () => {
  mediaQuery = window.matchMedia('(max-width: 820px)')
  compactMediaListener = event => { isCompact.value = event.matches }
  isCompact.value = mediaQuery.matches
  mediaQuery.addEventListener('change', compactMediaListener)
  window.addEventListener('beforeunload', handleBeforeUnload)
  await loadEngines()
  await applyQueryTaskRoute()
  queryTaskRouteReady.value = true
  markSaved()
})

watch(() => route.fullPath, () => {
  if (queryTaskRouteReady.value) applyQueryTaskRoute()
})

onBeforeUnmount(() => {
  executionRequestSequence += 1
  sampleRequests.invalidate()
  window.removeEventListener('beforeunload', handleBeforeUnload)
  if (mediaQuery && compactMediaListener) {
    mediaQuery.removeEventListener('change', compactMediaListener)
  }
})
</script>

<style scoped>
.query-workbench {
  height: 100%;
  min-height: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  background: var(--addp-bg-primary);
}

.workbench-toolbar {
  min-height: 58px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 8px 14px;
  border-bottom: 1px solid var(--addp-border-color);
  background: var(--addp-bg-primary);
}

.toolbar-primary,
.toolbar-actions,
.result-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.toolbar-primary h2 {
  max-width: 240px;
  margin: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--addp-text-primary);
  font-size: 17px;
  font-weight: 600;
}

.engine-select {
  width: 270px;
}

.engine-type {
  float: right;
  margin-left: 20px;
  color: var(--addp-text-tertiary);
  font-size: 12px;
}

.workbench-body {
  flex: 1;
  min-height: 0;
  display: flex;
  overflow: hidden;
}

.catalog-panel {
  min-width: 240px;
  max-width: 480px;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  background: var(--addp-bg-secondary);
}

.catalog-heading,
.panel-heading {
  height: 40px;
  flex: 0 0 40px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 0 12px;
  border-bottom: 1px solid var(--addp-border-color);
  color: var(--addp-text-primary);
  font-size: 13px;
  font-weight: 600;
}

.panel-heading > span:first-child {
  display: flex;
  align-items: center;
  gap: 7px;
}

.catalog-tree {
  flex: 1;
  min-height: 0;
}

.catalog-tree :deep(.resource-tree-picker),
.catalog-tree :deep(.resource-tree) {
  height: 100%;
}

.catalog-tree :deep(.resource-tree) {
  border: 0;
  border-radius: 0;
}

.catalog-tree :deep(.el-card__header) {
  display: none;
}

.query-surface {
  flex: 1;
  min-width: 0;
  min-height: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.editor-panel,
.result-panel {
  min-height: 0;
  display: flex;
  flex-direction: column;
}

.editor-panel {
  flex: 0 0 auto;
  max-height: calc(100% - 220px);
}

.result-panel {
  flex: 1;
}

.graph-result-view {
  width: 100%;
  height: 100%;
}

.editor-content,
.result-content {
  flex: 1;
  min-height: 0;
  overflow: hidden;
}

.dirty-indicator {
  color: var(--el-color-warning);
  font-weight: 500;
}

.resize-handle {
  flex: 0 0 auto;
  background: var(--addp-border-color);
  transition: background-color 0.15s ease;
}

.resize-handle:hover,
.resize-handle:focus-visible {
  background: var(--el-color-primary);
  outline: none;
}

.resize-handle.vertical {
  width: 5px;
  cursor: col-resize;
}

.resize-handle.horizontal {
  height: 5px;
  cursor: row-resize;
}

.drawer-catalog-actions {
  display: flex;
  justify-content: flex-end;
  margin-bottom: 8px;
}

.parameter-toolbar {
  display: flex;
  justify-content: flex-end;
  margin-bottom: 12px;
}

.parameter-list {
  display: grid;
  gap: 12px;
}

.parameter-item {
  padding: 12px;
  border: 1px solid var(--addp-border-color);
  border-radius: 6px;
  background: var(--addp-bg-secondary);
}

.parameter-item-heading,
.parameter-item-actions {
  display: flex;
  align-items: center;
}

.parameter-item-heading {
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 10px;
}

.parameter-item-heading strong {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.parameter-item-actions {
  flex: 0 0 auto;
  gap: 6px;
}

.parameter-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
  gap: 0 12px;
}

.parameter-description {
  grid-column: 1 / -1;
}

.parameter-grid :deep(.el-select),
.parameter-grid :deep(.el-input-number) {
  width: 100%;
}

@media (max-width: 1120px) {
  .workbench-toolbar {
    align-items: flex-start;
    flex-wrap: wrap;
  }

  .toolbar-actions {
    margin-left: auto;
  }
}

@media (max-width: 820px) {
  .workbench-toolbar {
    padding: 8px;
  }

  .toolbar-primary,
  .toolbar-actions {
    width: 100%;
  }

  .toolbar-primary h2 {
    display: none;
  }

  .engine-select {
    flex: 1;
    width: auto;
  }

  .toolbar-actions {
    overflow-x: auto;
    padding-bottom: 2px;
  }

  .editor-panel {
    max-height: min(52vh, calc(100% - 180px));
  }

  .parameter-grid {
    grid-template-columns: minmax(0, 1fr);
  }

  .parameter-description {
    grid-column: auto;
  }
}
</style>
