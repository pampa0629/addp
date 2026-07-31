<template>
  <div class="orchestration-form" :aria-busy="saving">
    <StatusAnnouncer :label="t('orchestrator.orchestrationForm.statusLabel')" :message="formAnnouncement" />
    <div class="header">
      <div class="header-title">
        <h2>{{ isEdit ? t('orchestrator.orchestrationForm.editTitle') : t('orchestrator.orchestrationForm.createTitle') }}</h2>
        <div class="header-summary">
          <ScheduleDisplay :cron="form.schedule" :empty-text="t('schedule.manualTrigger')" />
          <el-tag size="small" :type="form.enabled ? 'success' : 'info'">
            {{ form.enabled ? t('orchestrator.orchestrationForm.enabledLabel') : t('orchestrator.orchestrationForm.disabledLabel') }}
          </el-tag>
        </div>
      </div>
      <div class="header-actions">
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
        <el-button type="primary" @click="handleSave" :loading="saving">
          <el-icon><Check /></el-icon>
          {{ t('orchestrator.orchestrationForm.saveBtn') }}
        </el-button>
      </div>
    </div>

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
          ref="dagEditor"
          :initial-steps="form.steps"
          :initial-layout="form.editor_layout"
          @update:steps="handleStepsUpdate"
          @update:layout="handleLayoutUpdate"
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
        <el-form-item :label="t('orchestrator.orchestrationForm.enabledLabel')">
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
import { ref, reactive, onMounted, computed } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { Check, Clock, Close, Document } from '@element-plus/icons-vue'
import DAGEditor from '../components/DAGEditor.vue'
import TaskPanel from '../components/TaskPanel.vue'
import orchestrationAPI from '../api/orchestration'
import { buildOrchestrationPayload } from '../utils/orchestrationPayload'
import { focusElement, ScheduleConfig, ScheduleDisplay, StatusAnnouncer, useResizable } from '@common-ui'

const { t } = useI18n()
const router = useRouter()
const route = useRoute()
const dagEditor = ref(null)
const metadataNameInputRef = ref(null)
const scheduleEnabledSwitchRef = ref(null)
const jsonCloseButtonRef = ref(null)

const isEdit = ref(false)
const saving = ref(false)
const jsonDialogVisible = ref(false)
const metadataDialogVisible = ref(false)
const scheduleDialogVisible = ref(false)
const {
  size: leftPanelWidth,
  minSize: leftPanelMinWidth,
  maxSize: leftPanelMaxWidth,
  startResize: startLeftPanelResize,
  handleResizeKeydown: handleLeftPanelResizeKeydown
} = useResizable(320, 240, 560, 'horizontal')

const form = reactive({
  name: '',
  description: '',
  enabled: false,
  schedule: '',
  steps: [],
  editor_layout: {}
})

const metadataDraft = reactive({
  name: '',
  description: ''
})

const scheduleDraft = reactive({
  enabled: false,
  schedule: ''
})

// 格式化 JSON 用于展示
const formattedJSON = computed(() => {
  return JSON.stringify(form, null, 2)
})
const formAnnouncement = computed(() => saving.value
  ? t('orchestrator.orchestrationForm.savingStatus')
  : '')

onMounted(async () => {
  const id = route.params.id
  if (id && id !== 'new') {
    isEdit.value = true
    await loadOrchestration(id)
  }
})

async function loadOrchestration(id) {
  try {
    const data = await orchestrationAPI.get(id)
    Object.assign(form, buildOrchestrationPayload(data))
  } catch (error) {
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
  await persistForm()
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
    const payload = buildOrchestrationPayload(form)
    if (isEdit.value) {
      await orchestrationAPI.update(route.params.id, payload)
      ElMessage.success(t('orchestrator.orchestrationForm.updateSuccess'))
    } else {
      await orchestrationAPI.create(payload)
      ElMessage.success(t('orchestrator.orchestrationForm.createSuccess'))
    }
    router.push('/orchestrations')
  } catch (error) {
    ElMessage.error(isEdit.value ? t('orchestrator.orchestrationForm.updateFailed') : t('orchestrator.orchestrationForm.createFailed'))
  } finally {
    saving.value = false
  }
}

function handleCancel() {
  router.back()
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

.header-summary {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}

.header-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  justify-content: flex-end;
}

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
