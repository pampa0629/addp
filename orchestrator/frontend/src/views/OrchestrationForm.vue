<template>
  <div class="orchestration-form" :aria-busy="saving">
    <StatusAnnouncer :label="t('orchestrator.orchestrationForm.statusLabel')" :message="formAnnouncement" />
    <div class="header">
      <div class="header-title">
        <div class="title-row">
          <h2 class="orchestration-name">{{ isEdit ? form.name : t('orchestrator.orchestrationForm.createTitle') }}</h2>
          <el-tooltip v-if="isEdit" :content="t('orchestrator.orchestrationForm.editMetadata')">
            <el-button
              text circle size="small"
              :aria-label="t('orchestrator.orchestrationForm.editMetadata')"
              :disabled="!loaded || !editorReady || saving || executing"
              @click="openMetadataDialog"
            ><el-icon><Edit /></el-icon></el-button>
          </el-tooltip>
        </div>
        <div class="header-summary">
          <ScheduleDisplay :cron="form.schedule" :empty-text="t('schedule.manualTrigger')" />
          <el-tag v-if="form.schedule" size="small" :type="form.enabled ? 'success' : 'info'">
            {{ form.enabled ? t('orchestrator.orchestrationForm.enabledLabel') : t('orchestrator.orchestrationForm.disabledLabel') }}
          </el-tag>
          <span v-if="hasUnsavedChanges" class="unsaved-status">{{ t('orchestrator.orchestrationForm.unsavedChanges') }}</span>
        </div>
      </div>
      <div class="header-actions">
        <el-tooltip v-if="authStore.hasPermission('orchestrator.workflow.execute')" :content="activeExecution ? t('orchestrator.liveExecution.alreadyRunning') : t('orchestrator.orchestrationForm.saveBeforeExecute')" :disabled="!executionDisabled">
          <span><OrchestrationExecuteButton
            :key="route.params.id"
            :orchestration="{ ...form, id: route.params.id }"
            :execute="orchestrationAPI.execute"
            :disabled="executionDisabled"
            @busy="executing = $event"
            @executed="handleExecuted"
          /></span>
        </el-tooltip>
        <el-button @click="handleOpenScheduleDialog">
          <el-icon><Clock /></el-icon>
          {{ t('orchestrator.orchestrationForm.scheduleBtn') }}
        </el-button>
        <el-button @click="handleViewJSON">
          <el-icon><Document /></el-icon>
          {{ t('orchestrator.orchestrationForm.viewJsonBtn') }}
        </el-button>
        <el-button @click="handleCancel">
          <el-icon><Close /></el-icon>
          {{ t('orchestrator.orchestrationForm.cancelBtn') }}
        </el-button>
        <el-button type="primary" @click="handleSave" :loading="saving" :disabled="!loaded || !editorReady || executing">
          <el-icon><Check /></el-icon>
          {{ t('orchestrator.orchestrationForm.saveBtn') }}
        </el-button>
      </div>
    </div>

    <section v-if="execution" class="execution-summary" :aria-label="t('orchestrator.liveExecution.title')">
      <div class="execution-summary-row">
        <strong>{{ t('orchestrator.liveExecution.title') }}</strong>
        <el-tag role="status" :type="executionStatusType(execution.status)">{{ t(`orchestrator.liveExecution.status.${execution.status}`) }}</el-tag>
        <span>{{ execution.execution_id }}</span>
        <span>{{ t('orchestrator.liveExecution.completed', { completed: completedSteps, total: executionSteps.length }) }}</span>
        <el-button size="small" @click="openMonitorExecution(execution.execution_id)">{{ t('orchestrator.liveExecution.viewMonitor') }}</el-button>
        <el-button v-if="!activeExecution" size="small" text @click="clearExecution">{{ t('orchestrator.liveExecution.dismiss') }}</el-button>
      </div>
      <p v-if="execution.error_details?.message" class="execution-error">{{ execution.error_details.message }}</p>
      <p v-if="!executionMatchesDefinition">{{ t('orchestrator.liveExecution.definitionChanged') }}</p>
      <div v-if="executionRefreshFailed" class="execution-refresh-error" role="alert">
        {{ t('orchestrator.liveExecution.refreshFailed') }}
        <el-button size="small" :loading="executionRefreshing" @click="refreshExecution">{{ t('orchestrator.liveExecution.retry') }}</el-button>
      </div>
    </section>

    <!-- 三栏布局 -->
    <div class="three-column-layout">
      <!-- 左侧任务库 -->
      <div id="task-library-panel" class="left-panel" :style="{ width: `${leftPanelWidth}px` }">
        <TaskPanel @add-task="handleAddTask" />
      </div>

      <div
        class="panel-splitter"
        role="separator"
        aria-orientation="vertical"
        aria-controls="task-library-panel"
        :aria-label="t('orchestrator.orchestrationForm.resizeTaskPanel')"
        :aria-valuemin="leftPanelMinWidth"
        :aria-valuemax="leftPanelMaxWidth"
        :aria-valuenow="Math.round(leftPanelWidth)"
        tabindex="0"
        @mousedown="startLeftPanelResize"
        @keydown="handleLeftPanelResizeKeydown"
      ></div>

      <!-- 中央 DAG 画布 -->
      <div class="center-panel">
        <DAGEditor
          v-if="loaded"
          :key="route.params.id || 'new'"
          ref="dagEditor"
          :initial-steps="form.steps"
          :initial-layout="form.editor_layout"
          :execution-states="executionMatchesDefinition ? executionStates : {}"
          @update:steps="handleStepsUpdate"
          @update:layout="handleLayoutUpdate"
          @ready="handleEditorReady"
        />
      </div>
    </div>

    <el-dialog
      v-model="metadataDialogVisible"
      class="addp-dialog"
      :title="t('orchestrator.orchestrationForm.saveDialogTitle')"
      width="min(520px, calc(100vw - 24px))"
      :close-on-click-modal="false"
      @opened="focusMetadataName"
    >
      <el-form :model="metadataDraft" label-position="top">
        <el-form-item :label="t('orchestrator.orchestrationForm.nameLabel')" required>
          <el-input
            ref="metadataNameInputRef"
            v-model="metadataDraft.name"
            :placeholder="t('orchestrator.orchestrationForm.namePlaceholder')"
          />
        </el-form-item>
        <el-form-item :label="t('orchestrator.orchestrationForm.descriptionLabel')">
          <el-input
            v-model="metadataDraft.description"
            type="textarea"
            :rows="3"
            :placeholder="t('orchestrator.orchestrationForm.descriptionPlaceholder')"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="metadataDialogVisible = false">
          {{ t('orchestrator.orchestrationForm.saveDialogCancel') }}
        </el-button>
        <el-button type="primary" @click="confirmMetadataDialog">
          {{ t('orchestrator.orchestrationForm.saveDialogConfirm') }}
        </el-button>
      </template>
    </el-dialog>

    <el-dialog
      v-model="scheduleDialogVisible"
      class="addp-dialog"
      :title="t('orchestrator.orchestrationForm.scheduleDialogTitle')"
      width="min(720px, calc(100vw - 24px))"
      :close-on-click-modal="false"
      @opened="focusScheduleEnabled"
    >
      <el-form :model="scheduleDraft" label-position="top">
        <el-form-item :label="t('orchestrator.orchestrationForm.scheduleEnabledLabel')">
          <el-switch ref="scheduleEnabledSwitchRef" v-model="scheduleDraft.enabled" />
        </el-form-item>
        <el-form-item>
          <ScheduleConfig
            v-model="scheduleDraft.schedule"
            :allow-custom-cron="true"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="scheduleDialogVisible = false">
          {{ t('orchestrator.orchestrationForm.scheduleDialogCancel') }}
        </el-button>
        <el-button type="primary" @click="confirmScheduleDialog">
          {{ t('orchestrator.orchestrationForm.scheduleDialogConfirm') }}
        </el-button>
      </template>
    </el-dialog>

    <!-- JSON 预览对话框 -->
    <el-dialog
      v-model="jsonDialogVisible"
      class="addp-dialog"
      :title="t('orchestrator.orchestrationForm.jsonDialogTitle')"
      width="min(840px, calc(100vw - 24px))"
      :close-on-click-modal="false"
      @opened="focusJsonClose"
    >
      <div class="json-preview">
        <pre class="json-content">{{ formattedJSON }}</pre>
      </div>
      <template #footer>
        <el-button ref="jsonCloseButtonRef" @click="jsonDialogVisible = false">
          {{ t('orchestrator.orchestrationForm.jsonDialogClose') }}
        </el-button>
        <el-button @click="downloadJSON">{{ t('orchestrator.orchestrationForm.downloadJsonBtn') }}</el-button>
        <el-button type="primary" @click="copyJSON">{{ t('orchestrator.orchestrationForm.copyJsonBtn') }}</el-button>
      </template>
    </el-dialog>

  </div>
</template>

<script setup>
import { ref, reactive, watch, computed } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { Check, Clock, Close, Document, Edit } from '@element-plus/icons-vue'
import { useAuthStore } from '../store/auth'
import { useUnsavedChangesGuard, openMonitorExecution } from '@common-ui'
import { useExecutionTracking } from '../composables/useExecutionTracking'
import { executionStepStates, executionStatusType } from '../utils/executionPresentation'
import DAGEditor from '../components/DAGEditor.vue'
import TaskPanel from '../components/TaskPanel.vue'
import orchestrationAPI from '../api/orchestration'
import { buildOrchestrationPayload } from '../utils/orchestrationPayload'
import { focusElement, ScheduleConfig, ScheduleDisplay, StatusAnnouncer, useResizable, useConsolePageDescriptor, OrchestrationExecuteButton } from '@common-ui'
import { navigateOrchestratorRoute, orchestrationListLocation } from '@/utils/moduleNavigation'

const { t } = useI18n()
const authStore = useAuthStore()
const editorReady = ref(false)
const router = useRouter()
const route = useRoute()
const dagEditor = ref(null)
const metadataNameInputRef = ref(null)
const scheduleEnabledSwitchRef = ref(null)
const jsonCloseButtonRef = ref(null)

const isEdit = ref(false)
const saving = ref(false)
const executing = ref(false)
const executionSteps = ref([])
const executedDefinition = ref('')
const {
  execution, active: activeExecution, refreshFailed: executionRefreshFailed,
  refreshing: executionRefreshing, start: trackExecution, clear: clearExecution, refresh: refreshExecution
} = useExecutionTracking(orchestrationAPI)
const executionStates = computed(() => executionStepStates(execution.value, executionSteps.value))
const completedSteps = computed(() => Object.values(executionStates.value).filter(step => ['success', 'failed', 'timeout', 'cancelled'].includes(step.status)).length)
const executionMatchesDefinition = computed(() => executedDefinition.value === definitionSignature())
function handleExecuted(result) {
  executedDefinition.value = savedDefinition.value
  executionSteps.value = JSON.parse(savedDefinition.value).steps
  trackExecution(result)
}
const loaded = ref(false)
const savedDefinition = ref('')
const savedPositions = ref('')
const layoutBaselinePending = ref(true)
let loadGeneration = 0
const jsonDialogVisible = ref(false)
const metadataDialogVisible = ref(false)
const scheduleDialogVisible = ref(false)
const {
  size: leftPanelWidth,
  minSize: leftPanelMinWidth,
  maxSize: leftPanelMaxWidth,
  startResize: startLeftPanelResize,
  handleResizeKeydown: handleLeftPanelResizeKeydown
} = useResizable(360, 240, 560, 'horizontal')

const form = reactive({
  name: '',
  description: '',
  enabled: false,
  schedule: '',
  steps: [],
  editor_layout: {}
})
useConsolePageDescriptor(router, 'orchestrator', {
  title: computed(() => t('orchestrator.orchestrationForm.recentVisitTitle')),
  subject: computed(() => form.name),
  ready: computed(() => isEdit.value && Boolean(form.name))
})

const metadataDraft = reactive({
  name: '',
  description: ''
})

const scheduleDraft = reactive({
  enabled: false,
  schedule: ''
})

function definitionSignature() {
  const { editor_layout, ...definition } = buildOrchestrationPayload(form)
  return JSON.stringify(definition)
}
const hasDefinitionChanges = computed(() => loaded.value && definitionSignature() !== savedDefinition.value)
function positionsSignature() {
  return JSON.stringify(Object.entries(form.editor_layout?.nodes || {}).sort(([a], [b]) => a.localeCompare(b)))
}
const hasUnsavedChanges = computed(() => loaded.value && (hasDefinitionChanges.value ||
  (metadataDialogVisible.value && (metadataDraft.name !== form.name || metadataDraft.description !== form.description)) ||
  (scheduleDialogVisible.value && (scheduleDraft.enabled !== form.enabled || scheduleDraft.schedule !== form.schedule)) ||
  (!layoutBaselinePending.value && positionsSignature() !== savedPositions.value)))
useUnsavedChangesGuard({
  router,
  isDirty: () => hasUnsavedChanges.value,
  shouldConfirmUpdate: (to, from) => to.params.id !== from.params.id
})
function handleEditorReady(ready) {
  editorReady.value = ready
  if (ready && layoutBaselinePending.value) {
    savedPositions.value = positionsSignature()
    layoutBaselinePending.value = false
  }
}
const executionDisabled = computed(() => !loaded.value || !editorReady.value || !isEdit.value || saving.value || activeExecution.value || hasDefinitionChanges.value)

// 格式化 JSON 用于展示
const formattedJSON = computed(() => {
  return JSON.stringify(form, null, 2)
})
const formAnnouncement = computed(() => saving.value
  ? t('orchestrator.orchestrationForm.savingStatus')
  : '')

watch(() => route.params.id, async id => {
  clearExecution()
  executing.value = false
  const generation = ++loadGeneration
  loaded.value = false
  editorReady.value = false
  layoutBaselinePending.value = true
  isEdit.value = Boolean(id && id !== 'new')
  if (isEdit.value) {
    await loadOrchestration(id, generation)
  } else {
    Object.assign(form, buildOrchestrationPayload())
    savedDefinition.value = definitionSignature()
    loaded.value = true
  }
}, { immediate: true })

async function loadOrchestration(id, generation) {
  try {
    const data = await orchestrationAPI.get(id)
    if (generation !== loadGeneration) return
    Object.assign(form, buildOrchestrationPayload(data))
    savedDefinition.value = definitionSignature()
    loaded.value = true
  } catch (error) {
    if (generation !== loadGeneration) return
    ElMessage.error(t('orchestrator.orchestrationForm.loadFailed'))
  }
}

function handleStepsUpdate(steps) {
  form.steps = steps
}

function handleLayoutUpdate(layout) {
  form.editor_layout = layout
}

function handleAddTask(taskData) {
  dagEditor.value?.addTask(taskData)
}

function hasValue(value) {
  return value !== null && value !== undefined && String(value).trim() !== ''
}

function hasTaskReference(step) {
  return hasValue(step.provider) && hasValue(step.task_type) && hasValue(step.task_id)
}

async function handleSave() {
  if (!loaded.value || !editorReady.value || saving.value || executing.value) return
  const latestSteps = dagEditor.value?.getSteps ? dagEditor.value.getSteps() : form.steps
  form.steps = latestSteps || []
  form.editor_layout = dagEditor.value?.getLayout ? dagEditor.value.getLayout() : form.editor_layout

  if (!form.steps || form.steps.length === 0) {
    ElMessage.warning(t('orchestrator.orchestrationForm.stepsRequired'))
    return
  }

  if (form.steps.some(step => !hasTaskReference(step))) {
    ElMessage.warning(t('orchestrator.orchestrationForm.stepTaskReferenceRequired'))
    return
  }

  if (isEdit.value) {
    await persistForm()
    return
  }
  openMetadataDialog()
}

function openMetadataDialog() {
  metadataDraft.name = form.name
  metadataDraft.description = form.description
  metadataDialogVisible.value = true
}

async function confirmMetadataDialog() {
  if (!hasValue(metadataDraft.name)) {
    ElMessage.warning(t('orchestrator.orchestrationForm.nameRequired'))
    return
  }

  form.name = metadataDraft.name.trim()
  form.description = metadataDraft.description
  metadataDialogVisible.value = false
  if (isEdit.value) await handleSave()
  else await persistForm()
}

function handleOpenScheduleDialog() {
  scheduleDraft.enabled = form.enabled
  scheduleDraft.schedule = form.schedule
  scheduleDialogVisible.value = true
}

function confirmScheduleDialog() {
  form.enabled = scheduleDraft.enabled
  form.schedule = scheduleDraft.schedule
  scheduleDialogVisible.value = false
  ElMessage.success(t('orchestrator.orchestrationForm.scheduleSaved'))
}

async function persistForm() {
  saving.value = true
  try {
    const payload = JSON.parse(JSON.stringify(buildOrchestrationPayload(form)))
    const savedSignature = definitionSignature()
    const savedPositionSignature = positionsSignature()
    if (isEdit.value) {
      await orchestrationAPI.update(route.params.id, payload)
      ElMessage.success(t('orchestrator.orchestrationForm.updateSuccess'))
    } else {
      await orchestrationAPI.create(payload)
      ElMessage.success(t('orchestrator.orchestrationForm.createSuccess'))
    }
    savedDefinition.value = savedSignature
    savedPositions.value = savedPositionSignature
    if (!isEdit.value) await navigateOrchestratorRoute(router, orchestrationListLocation(route.query), { history: 'replace' })
  } catch (error) {
    const detail = error.response?.data?.error
    ElMessage.error(typeof detail === 'string' && detail.trim()
      ? detail
      : t(isEdit.value ? 'orchestrator.orchestrationForm.updateFailed' : 'orchestrator.orchestrationForm.createFailed'))
  } finally {
    saving.value = false
  }
}

function handleCancel() {
  navigateOrchestratorRoute(router, orchestrationListLocation(route.query), { history: 'replace' })
}

function handleViewJSON() {
  jsonDialogVisible.value = true
}

function focusMetadataName() {
  focusElement(metadataNameInputRef.value)
}

function focusScheduleEnabled() {
  focusElement(scheduleEnabledSwitchRef.value)
}

function focusJsonClose() {
  focusElement(jsonCloseButtonRef.value)
}

async function copyJSON() {
  try {
    await navigator.clipboard.writeText(formattedJSON.value)
    ElMessage.success(t('orchestrator.orchestrationForm.jsonCopied'))
  } catch (error) {
    ElMessage.error(t('orchestrator.orchestrationForm.copyFailed'))
  }
}

function downloadJSON() {
  const blob = new Blob([formattedJSON.value], { type: 'application/json' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `orchestration-${form.name || 'unnamed'}.json`
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  URL.revokeObjectURL(url)
  ElMessage.success(t('orchestrator.orchestrationForm.jsonDownloaded'))
}
</script>

<style scoped>
.orchestration-form {
  height: 100%;
  display: flex;
  flex-direction: column;
  padding: 20px;
  overflow: hidden;
}

.header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  margin-bottom: 20px;
  flex-shrink: 0;
  gap: 16px;
  flex-wrap: wrap;
}

.header h2 {
  margin: 0;
  color: var(--addp-text-primary);
}

.header-title {
  display: flex;
  flex-direction: column;
  gap: 8px;
  min-width: 0;
}

.title-row {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.title-row .el-button {
  flex-shrink: 0;
}

.orchestration-name {
  font-size: 18px;
  font-weight: 600;
  overflow-wrap: anywhere;
  color: var(--addp-text-primary);
}

.unsaved-status {
  color: var(--el-color-warning);
  font-size: 12px;
}

.header-summary {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}

.header-actions :deep(.el-button + .el-button) { margin-left: 0; }

.header-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  justify-content: flex-end;
}

.execution-summary {
  flex-shrink: 0;
  margin-bottom: 12px;
  padding: 12px;
  border: 1px solid var(--addp-border-color);
  border-radius: 6px;
  color: var(--addp-text-primary);
  font-size: 13px;
}
.execution-summary-row { display: flex; align-items: center; flex-wrap: wrap; gap: 10px; }
.execution-summary-row > span { overflow-wrap: anywhere; }
.execution-summary p { margin: 8px 0 0; }
.execution-error { color: var(--el-color-danger); overflow-wrap: anywhere; }
.execution-refresh-error { margin-top: 8px; color: var(--el-color-warning); }

.three-column-layout {
  flex: 1;
  display: flex;
  gap: 0;
  min-height: 0;
  overflow: hidden;
}

.left-panel {
  flex-shrink: 0;
  min-width: 240px;
  max-width: 560px;
  height: 100%;
  min-height: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.left-panel :deep(.task-panel) {
  flex: 1;
  min-height: 0;
}

.panel-splitter {
  width: 12px;
  flex-shrink: 0;
  cursor: col-resize;
  position: relative;
  background: transparent;
}

.panel-splitter::before {
  content: '';
  position: absolute;
  top: 0;
  bottom: 0;
  left: 5px;
  width: 2px;
  border-radius: 999px;
  background: var(--addp-border-color);
  opacity: 0.9;
}

.panel-splitter:hover,
.panel-splitter:focus-visible {
  background: color-mix(in srgb, var(--el-color-primary) 10%, transparent);
}

.panel-splitter:hover::before,
.panel-splitter:focus-visible::before {
  background: var(--el-color-primary);
}

.panel-splitter:focus-visible {
  outline: 2px solid var(--el-color-primary);
  outline-offset: -2px;
}

.center-panel {
  flex: 1;
  min-width: 0;
  background: var(--addp-bg-secondary) !important;
  overflow: hidden;
}

.json-preview {
  display: flex;
  flex-direction: column;
  height: clamp(260px, 55vh, 520px);
}

.json-content {
  flex: 1;
  overflow: auto;
  background: var(--addp-bg-tertiary);
  padding: 16px;
  border-radius: 4px;
  font-family: 'Consolas', 'Monaco', 'Courier New', monospace;
  font-size: 13px;
  line-height: 1.6;
  color: var(--addp-text-primary);
  margin: 0;
  white-space: pre-wrap;
  word-break: break-all;
}
</style>
