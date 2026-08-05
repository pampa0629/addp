<template>
  <div class="query-workbench">
    <header class="workbench-toolbar">
      <div class="toolbar-primary">
        <el-button
          v-if="isCompact"
          circle
          :aria-label="t('develop.query.catalog')"
          @click="catalogDrawerVisible = true"
        >
          <el-icon><Menu /></el-icon>
        </el-button>
        <h2>{{ currentTaskName || t('develop.query.title') }}</h2>
        <el-select
          v-model="selectedQueryTarget"
          class="engine-select"
          :placeholder="t('develop.query.selectDataSource')"
          @change="onQueryTargetChange"
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
            :disabled="!selectedTarget"
            :aria-label="t('develop.query.testConnection')"
            @click="handleTestConnection"
          >
            <el-icon><Connection /></el-icon>
          </el-button>
        </el-tooltip>
        <el-tooltip :content="t('develop.query.loadSample')">
          <el-button
            circle
            :loading="loadingSampleQuery"
            :disabled="!selectedTarget || executing"
            :aria-label="t('develop.query.loadSample')"
            @click="loadSampleQuery({ replace: false })"
          >
            <el-icon><DocumentCopy /></el-icon>
          </el-button>
        </el-tooltip>
        <el-tooltip :content="t('develop.query.format')">
          <el-button
            circle
            :disabled="!formatterLanguage || !queryContent || executing"
            :aria-label="t('develop.query.format')"
            @click="formatQuery"
          >
            <el-icon><MagicStick /></el-icon>
          </el-button>
        </el-tooltip>
        <el-button
          :disabled="!selectedTarget || !queryContent.trim() || executing"
          @click="handlePersistQueryTask"
        >
          <el-icon><FolderAdd /></el-icon>
          {{ currentTaskId ? t('develop.query.updateTask') : t('develop.query.saveAsTask') }}
        </el-button>
        <el-button
          type="primary"
          :loading="executing"
          :disabled="loadingSampleQuery || !selectedTarget || !queryContent.trim()"
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
          <span>{{ t('develop.query.catalog') }}</span>
          <el-tooltip :content="t('develop.query.insertResourcePath')">
            <el-button
              circle
              size="small"
              :disabled="!catalogSelection?.display?.path"
              :aria-label="t('develop.query.insertResourcePath')"
              @click="insertCatalogPath"
            >
              <el-icon><Position /></el-icon>
            </el-button>
          </el-tooltip>
        </div>
        <ResourceTreePicker
          v-if="selectedEngineId"
          v-model="catalogSelection"
          class="catalog-tree"
          :engine-id="selectedEngineId"
          :show-engine-selector="false"
          :show-selection-summary="false"
          :show-count="false"
          :title="''"
          mode="any"
          tree-height="100%"
          @select="rememberCatalogSelection"
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
      :title="t('develop.query.catalog')"
      direction="ltr"
      size="min(88vw, 380px)"
    >
      <div class="drawer-catalog-actions">
        <el-button
          type="primary"
          :disabled="!catalogSelection?.display?.path"
          @click="insertCatalogPath"
        >
          <el-icon><Position /></el-icon>
          {{ t('develop.query.insertResourcePath') }}
        </el-button>
      </div>
      <ResourceTreePicker
        v-if="selectedEngineId"
        v-model="catalogSelection"
        :engine-id="selectedEngineId"
        :show-engine-selector="false"
        :show-selection-summary="false"
        :show-count="false"
        :title="''"
        mode="any"
        tree-height="calc(100vh - 150px)"
        @select="rememberCatalogSelection"
      />
    </el-drawer>

    <SaveQueryDialog
      v-model="showSaveDialog"
      :engine-id="selectedEngineId"
      :sql="queryContent"
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
  DocumentCopy,
  Edit,
  FolderAdd,
  List,
  MagicStick,
  Menu,
  Position,
  VideoPlay
} from '@element-plus/icons-vue'
import { format } from 'sql-formatter'
import {
  ResourceTreePicker,
  StatusAnnouncer,
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
  isTerminalExecutionStatus,
  monacoLanguageForQuery,
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
const appliedQueryTarget = ref('')
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
const resultViewMode = ref('table')
const catalogSelection = ref(null)
const catalogCompletions = ref([])
const catalogDrawerVisible = ref(false)
const isCompact = ref(false)
const announcement = ref('')
const savedSnapshot = ref('')
const queryTaskRouteReady = ref(false)
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
const selectedEngineId = computed(() => {
  if (selectedTarget.value) return selectedTarget.value.engine.id
  const match = String(selectedQueryTarget.value).match(/^engine:(\d+)$/)
  return match ? Number(match[1]) : null
})
const selectedEngineUnavailable = computed(() => Boolean(selectedQueryTarget.value && !selectedTarget.value))
const selectedCapability = computed(() => queryCapabilityForEngine(selectedTarget.value?.engine))
const monacoLanguage = computed(() => monacoLanguageForQuery(currentQueryLanguage.value))
const formatterLanguage = computed(() => formatterLanguageForQuery(currentQueryLanguage.value))
const hasGraphData = computed(() => {
  const graph = executionResult.value?.graph_data
  return Boolean(graph?.nodes?.length || graph?.relationships?.length)
})
const currentSnapshot = computed(() => JSON.stringify({
  engine_id: selectedEngineId.value,
  language: currentQueryLanguage.value,
  query: queryContent.value
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
      appliedQueryTarget.value = selectedQueryTarget.value
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

const loadSampleQuery = async ({ replace = false } = {}) => {
  if (!selectedTarget.value || loadingSampleQuery.value) return
  if (queryContent.value.trim() && !replace) {
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
  const request = sampleRequests.begin(selectedQueryTarget.value)
  loadingSampleQuery.value = true
  try {
    const sample = await getSampleQuery(selectedEngineId.value)
    if (!sampleRequests.isCurrent(request, selectedQueryTarget.value)) return
    queryContent.value = sample.query
    currentQueryLanguage.value = String(sample.language || selectedCapability.value.defaultLanguage).toLowerCase()
    clearResult()
    announcement.value = t('develop.query.sampleLoaded')
  } catch (error) {
    if (sampleRequests.isCurrent(request, selectedQueryTarget.value)) {
      ElMessage.error(error.response?.data?.error || error.message)
    }
  } finally {
    if (sampleRequests.isCurrent(request, selectedQueryTarget.value)) {
      loadingSampleQuery.value = false
    }
  }
}

const onQueryTargetChange = async (targetValue) => {
  const target = queryTargets.value.find(item => item.value === targetValue)
  if (!target) {
    selectedQueryTarget.value = appliedQueryTarget.value
    return
  }
  appliedQueryTarget.value = targetValue
  catalogSelection.value = null
  clearResult()
  const capability = queryCapabilityForEngine(target.engine)
  if (!capability.languages.includes(currentQueryLanguage.value)) {
    currentQueryLanguage.value = capability.defaultLanguage
  }
  if (!queryContent.value.trim()) {
    await loadSampleQuery({ replace: true })
  }
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

  const requestSequence = ++executionRequestSequence
  executing.value = true
  resultViewMode.value = 'table'
  try {
    const started = await createExecution({
      dev_type: 'query',
      trigger_type: 'manual',
      content: { query, query_type: currentQueryLanguage.value },
      execution_config: { engine_id: selectedEngineId.value },
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
  const next = {
    label: selection.display.label || path,
    insertText: path,
    detail: selection.display.type || ''
  }
  catalogCompletions.value = [next, ...catalogCompletions.value.filter(item => item.insertText !== path)].slice(0, 100)
}

const insertCatalogPath = () => {
  const path = catalogSelection.value?.display?.path
  if (!path) return
  editorRef.value?.insertText(path)
  catalogDrawerVisible.value = false
  announcement.value = t('develop.query.resourcePathInserted')
}

const handleSaveTask = async (taskData) => {
  try {
    const task = await saveQueryTask({ ...taskData, query_type: currentQueryLanguage.value })
    currentTaskId.value = task.id
    currentTaskName.value = task.name
    currentTask.value = task
    markSaved()
    await navigateDevelopTaskEditor(router, 'query', task.id, { history: 'replace' })
    ElMessage.success(t('develop.query.saveTaskSuccess'))
    showSaveDialog.value = false
  } catch (error) {
    ElMessage.error(t('develop.query.saveTaskFailed') + (error.response?.data?.error || error.message))
  }
}

const handlePersistQueryTask = async () => {
  if (!currentTaskId.value) {
    showSaveDialog.value = true
    return
  }
  try {
    const task = currentTask.value || {}
    const updated = await updateQueryTask(currentTaskId.value, {
      name: task.name || currentTaskName.value,
      display_name: task.display_name || currentTaskName.value,
      engine_id: selectedEngineId.value,
      query: queryContent.value,
      query_type: currentQueryLanguage.value,
      description: task.description,
      tags: task.tags || [],
      timeout: task.timeout
    })
    currentTask.value = updated
    currentTaskName.value = updated.name
    markSaved()
    ElMessage.success(t('develop.query.updateTaskSuccess'))
  } catch (error) {
    ElMessage.error(t('develop.query.updateTaskFailed') + (error.response?.data?.error || error.message))
  }
}

const loadTask = async (taskId) => {
  const task = await getDevTask(taskId)
  currentTaskId.value = task.id
  currentTaskName.value = task.name
  currentTask.value = task
  queryContent.value = task.content?.query || ''
  currentQueryLanguage.value = String(task.content?.query_type || '').toLowerCase()
  const engineID = task.execution_config?.engine_id
  selectedQueryTarget.value = engineID ? `engine:${engineID}` : ''
  appliedQueryTarget.value = selectedQueryTarget.value
  catalogSelection.value = null
  clearResult()
  if (!queryContent.value) ElMessage.warning(t('develop.query.taskNoSql'))
}

const resetQueryEditorForCreate = async () => {
  currentTaskId.value = null
  currentTaskName.value = ''
  currentTask.value = null
  queryContent.value = ''
  clearResult()
  if (!selectedTarget.value && queryTargets.value.length) {
    selectedQueryTarget.value = queryTargets.value[0].value
    appliedQueryTarget.value = selectedQueryTarget.value
  }
  currentQueryLanguage.value = selectedCapability.value.defaultLanguage
  if (selectedTarget.value) await loadSampleQuery({ replace: true })
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
}
</style>
