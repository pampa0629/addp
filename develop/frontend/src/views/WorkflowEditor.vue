<template>
  <div class="workflow-editor-page">
    <StatusAnnouncer :label="t('develop.workflow.statusLabel')" :message="workflowAnnouncement" />
    <header class="editor-header">
      <div class="editor-identity">
        <el-tooltip :content="t('develop.workflow.toggleOperatorPanel')">
          <el-button
            circle
            text
            :aria-label="t('develop.workflow.toggleOperatorPanel')"
            :disabled="editorBusy"
            @click="leftPanelCollapsed = !leftPanelCollapsed"
          >
            <el-icon><Grid /></el-icon>
          </el-button>
        </el-tooltip>
        <div class="title-block">
          <div class="title-row">
            <h2>{{ currentTaskName || t('develop.workflow.untitled') }}</h2>
            <el-tag size="small" :type="isDirty ? 'warning' : 'success'" effect="plain">
              {{ isDirty ? t('develop.workflow.unsaved') : t('develop.workflow.saved') }}
            </el-tag>
          </div>
          <div class="engine-row">
            <span class="engine-label">{{ t('develop.workflow.workflowEngine') }}</span>
            <el-select
              ref="engineSelectRef"
              :model-value="workflowEngineId"
              :placeholder="t('develop.workflow.selectEngine')"
              :disabled="switchingEngine || editorBusy"
              class="engine-select"
              @change="requestEngineChange"
            >
              <el-option
                v-if="workflowEngineUnavailable"
                :label="t('develop.workflow.engineUnavailableOption', { id: workflowEngineId })"
                :value="workflowEngineId"
                disabled
              />
              <el-option
                v-for="engine in workflowEngines"
                :key="engine.id"
                :label="engine.name"
                :value="engine.id"
              >
                <div class="engine-option">
                  <span>{{ engine.name }}</span>
                  <el-tag size="small" type="info">{{ getEngineTag(engine) }}</el-tag>
                </div>
              </el-option>
            </el-select>
            <el-tooltip
              v-if="workflowEngineUnavailable"
              :content="t('develop.workflow.engineUnavailableHint')"
              placement="bottom"
            >
              <el-tag class="engine-status-tag" size="small" type="danger" effect="plain">
                {{ t('develop.workflow.engineUnavailable') }}
              </el-tag>
            </el-tooltip>

            <el-button
              v-if="unavailableStorageBindings.length"
              class="storage-binding-status"
              type="danger"
              plain
              size="small"
              :disabled="editorBusy"
              @click="openStorageBindingDialog()"
            >
              <el-icon><WarningFilled /></el-icon>
              {{ t('develop.workflow.storageBindingsUnavailable', { count: unavailableStorageBindings.length }) }}
            </el-button>
            <el-button
              v-else-if="currentTaskId && storageBindingsLoadError"
              class="storage-binding-status"
              type="warning"
              plain
              size="small"
              :disabled="editorBusy"
              @click="loadStorageEngineBindings(currentTaskId)"
            >
              <el-icon><WarningFilled /></el-icon>
              {{ t('develop.workflow.storageBindingsCheckFailed') }}
            </el-button>
            <el-tag
              v-else-if="currentTaskId && storageBindingsLoading"
              class="storage-binding-status"
              size="small"
              type="info"
              effect="plain"
            >
              {{ t('develop.workflow.storageBindingsChecking') }}
            </el-tag>

            <template v-if="needsSparkRuntime()">
              <span class="engine-label">{{ t('develop.workflow.sparkRuntime') }}</span>
              <el-select
                v-model="sparkRuntimeId"
                :placeholder="t('develop.workflow.selectSparkCluster')"
                :disabled="editorBusy || sparkRuntimes.length === 0"
                class="engine-select"
              >
                <el-option
                  v-for="runtime in sparkRuntimes"
                  :key="runtime.id"
                  :label="formatRuntimeLabel(runtime)"
                  :value="runtime.id"
                />
              </el-select>
            </template>
          </div>
        </div>
      </div>

      <div v-if="hasValidationStatus" class="header-validation">
        <el-popover
          v-if="validationIssues.length"
          v-model:visible="validationPopoverVisible"
          placement="bottom"
          :width="420"
          trigger="click"
          popper-class="validation-popover"
        >
          <template #reference>
            <button
              type="button"
              class="validation-summary validation-trigger"
              :class="validationStatusClass"
              :aria-label="t('develop.workflow.viewIssues')"
            >
              <el-icon v-if="validating" class="is-loading"><Loading /></el-icon>
              <el-icon v-else-if="validationErrors.length"><CircleCloseFilled /></el-icon>
              <el-icon v-else><WarningFilled /></el-icon>
              <span class="validation-summary-text">{{ validationSummary }}</span>
              <span class="validation-preview">{{ validationIssues[0].message }}</span>
            </button>
          </template>
          <div class="validation-list">
            <div
              v-for="group in validationIssueGroups"
              :key="group.key"
              class="validation-group"
              role="group"
              :aria-label="group.label"
            >
              <div class="validation-group-title" :title="group.label">{{ group.label }}</div>
              <button
                v-for="issue in group.issues"
                :key="`${issue.severity}:${issue.code}:${issue.path}`"
                type="button"
                class="validation-item"
                :aria-label="issue.message"
                @click="focusValidationIssue(issue)"
              >
                <el-icon v-if="issue.severity === 'error'" class="validation-item-icon is-error">
                  <CircleCloseFilled />
                </el-icon>
                <el-icon v-else class="validation-item-icon is-warning"><WarningFilled /></el-icon>
                <span class="validation-item-content">
                  <span v-if="issue.paramLabel" class="validation-item-param">{{ issue.paramLabel }}</span>
                  <span class="validation-item-message">{{ issue.message }}</span>
                </span>
              </button>
            </div>
          </div>
        </el-popover>

        <div v-else class="validation-summary" :class="validationStatusClass">
          <el-icon v-if="validating" class="is-loading"><Loading /></el-icon>
          <el-icon v-else-if="validationRequestError"><WarningFilled /></el-icon>
          <el-icon v-else><CircleCheckFilled /></el-icon>
          <span>{{ validationSummary }}</span>
        </div>
      </div>

      <div class="primary-actions">
        <el-button type="primary" :disabled="!canSave || editorBusy" :loading="saving" @click="handleSave">
          <el-icon><DocumentAdd /></el-icon>
          {{ t('develop.workflow.save') }}
        </el-button>
        <el-button ref="executeTriggerButtonRef" type="success" :disabled="!canExecute" :loading="executionButtonLoading" @click="handleExecute">
          <el-icon><VideoPlay /></el-icon>
          {{ t('develop.workflow.execute') }}
        </el-button>
        <el-dropdown trigger="click" @command="handleMoreCommand">
          <el-button ref="moreActionsButtonRef" :disabled="editorBusy" :loading="importing">
            {{ t('develop.workflow.more') }}
            <el-icon class="el-icon--right"><MoreFilled /></el-icon>
          </el-button>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item command="saveAs" :disabled="!canSave || editorBusy">
                <el-icon><CopyDocument /></el-icon>{{ t('develop.workflow.saveAs') }}
              </el-dropdown-item>
              <el-dropdown-item command="viewJson" :disabled="!hasValidWorkflow">
                <el-icon><Document /></el-icon>{{ t('develop.workflow.viewJson') }}
              </el-dropdown-item>
              <el-dropdown-item command="import">
                <el-icon><Upload /></el-icon>{{ t('develop.workflow.import') }}
              </el-dropdown-item>
              <el-dropdown-item command="export" :disabled="!hasValidWorkflow">
                <el-icon><Download /></el-icon>{{ t('develop.workflow.export') }}
              </el-dropdown-item>
              <el-dropdown-item divided command="clear" :disabled="!hasValidWorkflow">
                <el-icon><Delete /></el-icon>{{ t('develop.workflow.clear') }}
              </el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
      </div>
    </header>

    <main class="editor-content" :inert="editorBusy" :aria-busy="editorBusy">
      <aside v-if="!leftPanelCollapsed" class="left-panel">
        <div class="panel-header">
          <span class="panel-title">{{ t('develop.workflow.operatorPanel') }}</span>
          <el-button
            circle
            text
            size="small"
            :aria-label="t('develop.workflow.collapseOperatorPanel')"
            @click="leftPanelCollapsed = true"
          >
            <el-icon><ArrowLeft /></el-icon>
          </el-button>
        </div>
        <div class="panel-body operator-panel-body">
          <OperatorPalette
            :workflow-engine-id="workflowEngineId"
            :operators="operators"
            :loading="operatorsLoading"
            :load-error="operatorLoadError"
            @operator-click="handleOperatorClick"
          />
        </div>
      </aside>

      <section class="canvas-panel">
        <WorkflowDAGCanvas
          ref="canvasRef"
          :initial-workflow="workflowData"
          :initial-layout="editorLayout"
          :operators="operators"
          :validation-issues="validationIssues"
          @update:workflow="handleWorkflowUpdate"
          @update:layout="handleLayoutUpdate"
          @node-click="handleNodeClick"
          @node-select="handleNodeSelect"
        />

      </section>

      <aside v-if="selectedNode && !rightPanelCollapsed" class="right-panel">
        <div class="panel-header">
          <div class="panel-title-group">
            <span class="panel-title">{{ selectedNode.displayName || selectedNode.operator }}</span>
            <span
              v-if="hasDistinctText(selectedNode.operator, selectedNode.displayName)"
              class="panel-subtitle"
            >
              {{ selectedNode.operator }}
            </span>
          </div>
          <el-button
            circle
            text
            size="small"
            :aria-label="t('develop.workflow.collapseParamsPanel')"
            @click="rightPanelCollapsed = true"
          >
            <el-icon><ArrowRight /></el-icon>
          </el-button>
        </div>
        <div class="panel-body params-panel-body">
          <OperatorParamsPanel
            ref="paramsPanelRef"
            :node-id="selectedNode.id"
            :operator="selectedNode.operator"
            :public-parameters="selectedNode.publicParameters"
            :initial-params="selectedNode.params"
            :validation-issues="selectedNodeValidationIssues"
            :input-connections="selectedNode.inputConnections"
            :input-connection-options="selectedNode.inputConnectionOptions"
            @update="handleParamsUpdate"
            @update-connection="handleInputConnectionUpdate"
          />
        </div>
      </aside>

      <el-tooltip v-if="selectedNode && rightPanelCollapsed" :content="t('develop.workflow.openParamsPanel')">
        <el-button
          class="open-inspector"
          circle
          :aria-label="t('develop.workflow.openParamsPanel')"
          @click="rightPanelCollapsed = false"
        >
          <el-icon><Setting /></el-icon>
        </el-button>
      </el-tooltip>
    </main>

    <el-dialog
      v-model="engineSwitchDialogVisible"
      class="addp-dialog"
      :title="t('develop.workflow.engineSwitchTitle')"
      width="min(520px, calc(100vw - 24px))"
      :close-on-click-modal="false"
      :close-on-press-escape="!switchingEngine && !saving"
      :show-close="!switchingEngine && !saving"
      @opened="focusEngineSwitchCancel"
      @closed="handleEngineSwitchDialogClosed"
    >
      <el-alert
        :title="t('develop.workflow.engineSwitchMessage', { name: pendingWorkflowEngine?.name || '-' })"
        type="warning"
        :closable="false"
        show-icon
      />
      <template #footer>
        <el-button ref="engineSwitchCancelRef" :disabled="switchingEngine || saving" @click="cancelEngineSwitch">
          {{ t('develop.workflow.cancel') }}
        </el-button>
        <el-button type="danger" plain :loading="switchingEngine" :disabled="saving" @click="clearAndSwitchEngine">
          {{ t('develop.workflow.clearAndSwitch') }}
        </el-button>
        <el-button type="primary" :loading="saving" :disabled="switchingEngine" @click="saveAndSwitchEngine">
          {{ t('develop.workflow.saveAndClear') }}
        </el-button>
      </template>
    </el-dialog>

    <el-dialog
      v-model="storageBindingDialogVisible"
      class="addp-dialog"
      :title="t('develop.workflow.rebindStorageEngineTitle')"
      width="min(620px, calc(100vw - 24px))"
      :close-on-click-modal="!rebindingStorageEngine"
      :close-on-press-escape="!rebindingStorageEngine"
      :show-close="!rebindingStorageEngine"
      @opened="focusStorageBindingTarget"
    >
      <div class="dialog-body-stack">
        <el-alert
          :title="t('develop.workflow.rebindStorageEngineMessage')"
          type="warning"
          :closable="false"
          show-icon
        />
        <el-form class="workflow-dialog-form" label-position="top" :disabled="rebindingStorageEngine">
          <el-form-item :label="t('develop.workflow.sourceStorageEngine')">
            <el-select
              v-if="unavailableStorageBindings.length > 1"
              v-model="rebindSourceEngineId"
              class="storage-engine-select"
              @change="rebindTargetEngineId = null"
            >
              <el-option
                v-for="binding in unavailableStorageBindings"
                :key="binding.engine_id"
                :label="formatStorageBindingLabel(binding)"
                :value="binding.engine_id"
              />
            </el-select>
            <span v-else class="binding-source-value">
              {{ selectedStorageBinding ? formatStorageBindingLabel(selectedStorageBinding) : '-' }}
            </span>
          </el-form-item>
          <el-form-item :label="t('develop.workflow.resourceTypes')">
            <div class="binding-resource-types">
              <el-tag
                v-for="resourceType in selectedStorageBinding?.resource_types || []"
                :key="resourceType"
                size="small"
                type="info"
                effect="plain"
              >
                {{ resourceType }}
              </el-tag>
            </div>
          </el-form-item>
          <el-form-item :label="t('develop.workflow.targetStorageEngine')" required>
            <el-select
              ref="rebindTargetSelectRef"
              v-model="rebindTargetEngineId"
              class="storage-engine-select"
              :placeholder="t('develop.workflow.selectTargetStorageEngine')"
              :disabled="compatibleStorageEngines.length === 0"
            >
              <el-option
                v-for="engine in compatibleStorageEngines"
                :key="engine.id"
                :label="formatStorageEngineLabel(engine)"
                :value="engine.id"
              />
            </el-select>
          </el-form-item>
        </el-form>
        <el-alert
          v-if="selectedStorageBinding && compatibleStorageEngines.length === 0"
          :title="t('develop.workflow.noCompatibleStorageEngine')"
          type="error"
          :closable="false"
          show-icon
        />
      </div>
      <template #footer>
        <el-button :disabled="rebindingStorageEngine" @click="storageBindingDialogVisible = false">
          {{ t('develop.workflow.cancel') }}
        </el-button>
        <el-button
          type="primary"
          :loading="rebindingStorageEngine"
          :disabled="!rebindTargetEngineId || compatibleStorageEngines.length === 0"
          @click="confirmStorageEngineRebind"
        >
          {{ t('develop.workflow.confirmRebindStorageEngine') }}
        </el-button>
      </template>
    </el-dialog>

    <el-dialog
      v-model="saveDialogVisible"
      class="addp-dialog"
      :title="saveDialogTitle"
      width="min(500px, calc(100vw - 24px))"
      :close-on-click-modal="!saving"
      :close-on-press-escape="!saving"
      :show-close="!saving"
      @opened="focusSaveName"
      @closed="handleSaveDialogClosed"
    >
      <el-form class="workflow-dialog-form" :model="saveForm" label-position="top" :disabled="saving">
        <el-form-item :label="t('develop.workflow.workflowName')" required>
          <el-input ref="saveNameInputRef" v-model="saveForm.name" :placeholder="t('develop.workflow.workflowNamePlaceholder')" />
        </el-form-item>
        <el-form-item :label="t('develop.workflow.displayName')">
          <el-input v-model="saveForm.display_name" :placeholder="t('develop.workflow.optional')" />
        </el-form-item>
        <el-form-item :label="t('develop.workflow.description')">
          <el-input
            v-model="saveForm.description"
            type="textarea"
            :rows="3"
            :placeholder="t('develop.workflow.descriptionPlaceholder')"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button :disabled="saving" @click="cancelSaveDialog">{{ t('develop.workflow.cancel') }}</el-button>
        <el-button type="primary" :loading="saving" :disabled="saving" @click="confirmSave">
          {{ t('develop.workflow.save') }}
        </el-button>
      </template>
    </el-dialog>

    <el-dialog
      v-model="executeDialogVisible"
      class="addp-dialog"
      :title="t('develop.workflow.executeDialogTitle')"
      width="min(500px, calc(100vw - 24px))"
      :close-on-click-modal="!executing"
      :close-on-press-escape="!executing"
      :show-close="!executing"
      @opened="focusExecuteInputs"
      @closed="restoreExecuteTriggerFocus"
    >
      <div class="execution-summary">
        <span>{{ t('develop.workflow.taskCount') }}</span>
        <strong>{{ workflowData?.tasks?.length || 0 }}</strong>
      </div>
      <el-form class="workflow-dialog-form" label-position="top" :disabled="executing">
        <el-form-item :label="t('develop.workflow.execParams')">
          <el-input ref="executeInputsRef" v-model="executeInputs" type="textarea" :rows="5" placeholder='{"key": "value"}' />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button :disabled="executing" @click="executeDialogVisible = false">
          {{ t('develop.workflow.cancel') }}
        </el-button>
        <el-button type="primary" :loading="executing" :disabled="executing" @click="confirmExecute">
          {{ t('develop.workflow.execute') }}
        </el-button>
      </template>
    </el-dialog>

    <el-dialog
      v-model="jsonDialogVisible"
      class="addp-dialog"
      :title="t('develop.workflow.jsonDialogTitle')"
      width="min(840px, calc(100vw - 24px))"
      @opened="focusJsonClose"
      @closed="restoreMoreActionsFocus"
    >
      <div class="json-viewer"><pre>{{ workflowJSON }}</pre></div>
      <template #footer>
        <el-button ref="jsonCloseButtonRef" @click="jsonDialogVisible = false">{{ t('develop.workflow.close') }}</el-button>
        <el-button type="primary" @click="copyJSON">{{ t('develop.workflow.copyToClipboard') }}</el-button>
      </template>
    </el-dialog>

    <input ref="fileInputRef" type="file" accept=".json" class="hidden-file-input" @change="handleFileChange" />

    <div class="ai-fab-wrapper">
      <transition name="ai-slide">
        <div v-if="aiDialogOpen" class="ai-inline-panel">
          <div class="ai-panel-header">
            <span class="ai-panel-title">{{ t('develop.workflow.aiTitle') }}</span>
            <el-button
              circle
              text
              size="small"
              :aria-label="t('develop.workflow.close')"
              :disabled="editorBusy"
              @click="aiDialogOpen = false"
            >
              <el-icon><Close /></el-icon>
            </el-button>
          </div>
          <el-input
            ref="aiInputRef"
            v-model="aiQuery"
            type="textarea"
            :rows="4"
            :placeholder="t('develop.workflow.aiPlaceholder')"
            :disabled="editorBusy"
            class="ai-panel-input"
          />
          <div class="ai-panel-footer">
            <el-button type="primary" :loading="generating" :disabled="editorBusy" size="small" @click="generateWorkflow">
              {{ t('develop.workflow.generateWorkflow') }}
            </el-button>
          </div>
        </div>
      </transition>
      <el-tooltip :content="t('develop.workflow.aiTitle')">
        <el-button
          class="ai-fab"
          circle
          type="primary"
          :aria-label="t('develop.workflow.aiTitle')"
          :disabled="editorBusy"
          @click="toggleAiPanel"
        >
          <el-icon><MagicStick /></el-icon>
        </el-button>
      </el-tooltip>
    </div>
  </div>
</template>

<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { onBeforeRouteLeave, useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  ArrowLeft,
  ArrowRight,
  CircleCheckFilled,
  CircleCloseFilled,
  Close,
  CopyDocument,
  Delete,
  Document,
  DocumentAdd,
  Download,
  Grid,
  Loading,
  MagicStick,
  MoreFilled,
  Setting,
  Upload,
  VideoPlay,
  WarningFilled
} from '@element-plus/icons-vue'
import OperatorPalette from '@/components/workflow/OperatorPalette.vue'
import WorkflowDAGCanvas from '@/components/workflow/WorkflowDAGCanvas.vue'
import OperatorParamsPanel from '@/components/workflow/OperatorParamsPanel.vue'
import {
  getDevTask,
  getWorkflowStorageEngineBindings,
  rebindWorkflowStorageEngine
} from '@/api/devTask'
import { listOperatorsByWorkflowEngine, validateWorkflowDefinition } from '@/api/operator'
import { generateWorkflowFromNL } from '@/api/copilot'
import { getWorkflowEngines, getSparkRuntimes } from '@/api/engines'
import { executeWorkflowTask, saveWorkflowTask, updateWorkflowTask } from '@/api/workflow'
import {
  buildWorkflowExportPayload,
  isSparkWorkflowEngine,
  isStandardWorkflowDefinition
} from '@/utils/workflowDevTaskPayload'
import { findInvalidOperatorMetadata } from '@/utils/operatorMetadataContract'
import { resolveWorkflowGenerationResult } from '@/utils/workflowGenerationResult.mjs'
import { getResourceBinding } from '@/utils/workflowResourceBindings'
import { hasDistinctText } from '@/utils/displayText'
import { groupValidationIssues, validationIssueParamName } from '@/utils/workflowValidationIssues'
import { focusElement, openMonitorExecution, StatusAnnouncer } from '@addp/common-frontend'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()

const workflowEngines = ref([])
const workflowEngineId = ref(null)
const selectedEngine = ref(null)
const sparkRuntimes = ref([])
const sparkRuntimeId = ref(null)
const operators = ref([])
const operatorsLoading = ref(false)
const operatorLoadError = ref('')

const canvasRef = ref(null)
const paramsPanelRef = ref(null)
const fileInputRef = ref(null)
const engineSelectRef = ref(null)
const moreActionsButtonRef = ref(null)
const rebindTargetSelectRef = ref(null)
const engineSwitchCancelRef = ref(null)
const saveNameInputRef = ref(null)
const executeInputsRef = ref(null)
const jsonCloseButtonRef = ref(null)
const executeTriggerButtonRef = ref(null)
const workflowData = ref({ tasks: [] })
const editorLayout = ref({})
const selectedNode = ref(null)
const currentTaskId = ref(null)
const currentTaskName = ref('')
const currentTask = ref(null)
const savedStateSignature = ref('')

const leftPanelCollapsed = ref(false)
const rightPanelCollapsed = ref(false)
const saveDialogVisible = ref(false)
const saveAsMode = ref(false)
const pendingAction = ref(null)
const saving = ref(false)
const saveForm = reactive({ name: '', display_name: '', description: '' })
const engineSwitchDialogVisible = ref(false)
const pendingWorkflowEngineId = ref(null)
const switchingEngine = ref(false)
const storageBindings = ref([])
const storageCandidateEngines = ref([])
const storageBindingsLoading = ref(false)
const storageBindingsLoadError = ref('')
const storageBindingDialogVisible = ref(false)
const rebindSourceEngineId = ref(null)
const rebindTargetEngineId = ref(null)
const rebindingStorageEngine = ref(false)
let storageBindingRevision = 0

const executeDialogVisible = ref(false)
const executeInputs = ref('{}')
const preparingExecution = ref(false)
const executing = ref(false)
const jsonDialogVisible = ref(false)
const workflowJSON = ref('')
const importing = ref(false)

const validating = ref(false)
const validationResult = ref(null)
const validationRequestError = ref('')
const validationPopoverVisible = ref(false)
let validationRevision = 0

const aiDialogOpen = ref(false)
const aiQuery = ref('')
const generating = ref(false)
const aiInputRef = ref(null)

const hasValidWorkflow = computed(() => isStandardWorkflowDefinition(workflowData.value))
const isDirty = computed(() => editorStateSignature() !== savedStateSignature.value)
const workflowEngineUnavailable = computed(() => Boolean(
  workflowEngineId.value && !selectedEngine.value
))
const pendingWorkflowEngine = computed(() => (
  workflowEngines.value.find(engine => engine.id === pendingWorkflowEngineId.value) || null
))
const unavailableStorageBindings = computed(() => (
  storageBindings.value.filter(binding => !binding.available)
))
const selectedStorageBinding = computed(() => (
  unavailableStorageBindings.value.find(binding => binding.engine_id === rebindSourceEngineId.value) || null
))
const compatibleStorageEngines = computed(() => {
  const compatibleIds = new Set(selectedStorageBinding.value?.compatible_engine_ids || [])
  return storageCandidateEngines.value.filter(engine => compatibleIds.has(engine.id))
})
const storageBindingsExecutable = computed(() => Boolean(
  !currentTaskId.value || (
    !storageBindingsLoading.value &&
    !storageBindingsLoadError.value &&
    unavailableStorageBindings.value.length === 0
  )
))
const validationErrors = computed(() => validationResult.value?.errors || [])
const validationWarnings = computed(() => validationResult.value?.warnings || [])
const hasValidationStatus = computed(() => Boolean(
  validating.value || validationResult.value || validationRequestError.value
))
const validationIssues = computed(() => [
  ...mapValidationIssues(validationErrors.value, 'error'),
  ...mapValidationIssues(validationWarnings.value, 'warning')
])
const validationIssueGroups = computed(() => groupValidationIssues(validationIssues.value))
const selectedNodeValidationIssues = computed(() => {
  const nodeId = selectedNode.value?.id
  return nodeId ? validationIssues.value.filter(issue => issue.nodeId === nodeId) : []
})
const validationStatusClass = computed(() => {
  if (validationErrors.value.length) return 'is-error'
  if (validationWarnings.value.length) return 'is-warning'
  return validationRequestError.value ? 'is-warning' : 'is-valid'
})
const validationSummary = computed(() => {
  if (validating.value) return t('develop.workflow.validating')
  if (validationErrors.value.length) {
    return t('develop.workflow.validationFailed', { count: validationErrors.value.length })
  }
  if (validationWarnings.value.length) {
    return t('develop.workflow.validationWarnings', { count: validationWarnings.value.length })
  }
  if (validationRequestError.value) return t('develop.workflow.validationUnavailable')
  return t('develop.workflow.validationPassed')
})
const workflowAnnouncement = computed(() => {
  if (validating.value) return validationSummary.value
  if (saving.value) return t('develop.workflow.savingStatus')
  if (executing.value) return t('develop.workflow.submittingExecution')
  if (preparingExecution.value) return t('develop.workflow.preparingExecution')
  return hasValidationStatus.value ? validationSummary.value : ''
})
const editorBusy = computed(() => (
  saving.value ||
  preparingExecution.value ||
  switchingEngine.value ||
  rebindingStorageEngine.value ||
  generating.value ||
  importing.value
))
const executionButtonLoading = computed(() => preparingExecution.value || executing.value)
const canSave = computed(() => Boolean(workflowEngineId.value && hasValidWorkflow.value))
const canExecute = computed(() => Boolean(
  canSave.value &&
  selectedEngine.value &&
  storageBindingsExecutable.value &&
  (!needsSparkRuntime() || sparkRuntimeId.value) &&
  !saving.value &&
  !preparingExecution.value &&
  !executing.value &&
  !validating.value &&
  validationErrors.value.length === 0 &&
  !validationRequestError.value
))
const saveDialogTitle = computed(() => saveAsMode.value
  ? t('develop.workflow.saveAsDialogTitle')
  : t('develop.workflow.saveDialogTitle'))

function handleWorkflowUpdate(workflow) {
  workflowData.value = workflow
  if (workflow.tasks.length === 0) editorLayout.value = {}
  resetValidationState()
}

function handleLayoutUpdate(layout) {
  if (workflowData.value.tasks.length === 0) return
  editorLayout.value = layout
}

function handleNodeClick(node) {
  selectedNode.value = node
  if (node) rightPanelCollapsed.value = false
}

function handleNodeSelect(node) {
  selectedNode.value = node
  if (node) rightPanelCollapsed.value = true
}

function handleParamsUpdate(data) {
  canvasRef.value?.updateNodeParams(data.nodeId, data.params, selectedNode.value?.publicParameters)
}

function handleInputConnectionUpdate(connection) {
  canvasRef.value?.updateInputConnection(connection)
}

function handleOperatorClick(operator) {
  canvasRef.value?.addOperator(operator)
}

async function loadStorageEngineBindings(taskId) {
  if (!taskId) {
    resetStorageBindingState()
    return false
  }
  const revision = ++storageBindingRevision
  storageBindingsLoading.value = true
  storageBindingsLoadError.value = ''
  try {
    const result = await getWorkflowStorageEngineBindings(taskId)
    if (revision !== storageBindingRevision) return false
    storageBindings.value = Array.isArray(result?.items) ? result.items : []
    storageCandidateEngines.value = Array.isArray(result?.candidate_engines)
      ? result.candidate_engines
      : []
    return true
  } catch (error) {
    if (revision !== storageBindingRevision) return false
    storageBindings.value = []
    storageCandidateEngines.value = []
    storageBindingsLoadError.value = error.response?.data?.error || error.message
    return false
  } finally {
    if (revision === storageBindingRevision) storageBindingsLoading.value = false
  }
}

function resetStorageBindingState() {
  storageBindingRevision += 1
  storageBindings.value = []
  storageCandidateEngines.value = []
  storageBindingsLoading.value = false
  storageBindingsLoadError.value = ''
  storageBindingDialogVisible.value = false
  rebindSourceEngineId.value = null
  rebindTargetEngineId.value = null
}

function openStorageBindingDialog(sourceEngineId = null) {
  if (isDirty.value) {
    ElMessage.warning(t('develop.workflow.saveBeforeStorageRebind'))
    return
  }
  const binding = unavailableStorageBindings.value.find(item => item.engine_id === sourceEngineId) ||
    unavailableStorageBindings.value[0]
  if (!binding) return
  rebindSourceEngineId.value = binding.engine_id
  rebindTargetEngineId.value = null
  storageBindingDialogVisible.value = true
}

function focusStorageBindingTarget() {
  focusElement(rebindTargetSelectRef.value)
}

function focusEngineSwitchCancel() {
  focusElement(engineSwitchCancelRef.value)
}

async function confirmStorageEngineRebind() {
  if (rebindingStorageEngine.value || !currentTaskId.value || !selectedStorageBinding.value || !rebindTargetEngineId.value) return
  rebindingStorageEngine.value = true
  try {
    const result = await rebindWorkflowStorageEngine(
      currentTaskId.value,
      selectedStorageBinding.value.engine_id,
      rebindTargetEngineId.value
    )
    const task = result?.task
    if (!task?.content?.workflow_definition) throw new Error(t('develop.workflow.corruptedConfig'))
    currentTask.value = task
    currentTaskName.value = task.name
    workflowData.value = task.content.workflow_definition
    editorLayout.value = task.editor_layout || {}
    selectedNode.value = null
    await nextTick()
    markSaved()
    resetValidationState()
    storageBindingDialogVisible.value = false
    await loadStorageEngineBindings(task.id)
    ElMessage.success(t('develop.workflow.storageRebindSuccess', {
      count: result.replaced_locator_count
    }))
  } catch (error) {
    ElMessage.error(t('develop.workflow.storageRebindFailed') + (error.response?.data?.error || error.message))
  } finally {
    rebindingStorageEngine.value = false
  }
}

function formatStorageBindingLabel(binding) {
  return t('develop.workflow.storageBindingLabel', {
    id: binding.engine_id,
    count: binding.reference_count
  })
}

function formatStorageEngineLabel(engine) {
  return `${engine.name} (${engine.engine_type || '-'})`
}

async function loadWorkflowEngines() {
  try {
    const response = await getWorkflowEngines()
    workflowEngines.value = response.data || response
    if (workflowEngines.value.length === 0) ElMessage.warning(t('develop.workflow.noEngineAvailable'))
  } catch (error) {
    ElMessage.error(t('develop.workflow.loadEngineFailed'))
  }
}

async function loadOperators(engineId) {
  operatorsLoading.value = true
  operatorLoadError.value = ''
  operators.value = []
  if (!workflowEngines.value.some(engine => engine.id === engineId)) {
    operatorsLoading.value = false
    operatorLoadError.value = t('develop.workflow.engineUnavailableHint')
    return false
  }
  try {
    const response = await listOperatorsByWorkflowEngine(engineId)
    if (!Array.isArray(response?.operators)) throw new Error(t('develop.operatorPalette.invalidMetadata'))
    const invalid = findInvalidOperatorMetadata(response.operators)
    if (invalid) throw new Error(t('develop.operatorPalette.invalidOperatorMetadata', { name: invalid.name || '-' }))
    operators.value = response.operators
    return true
  } catch (error) {
    operatorLoadError.value = t('develop.operatorPalette.loadFailed') + (error.response?.data?.error || error.message)
    ElMessage.error(operatorLoadError.value)
    return false
  } finally {
    operatorsLoading.value = false
  }
}

async function loadSparkRuntimes() {
  try {
    const response = await getSparkRuntimes()
    sparkRuntimes.value = response.data || response
  } catch {
    ElMessage.error(t('develop.workflow.loadSparkRuntimeFailed'))
  }
}

async function selectDefaultEngine() {
  if (!workflowEngines.value.length) return
  workflowEngineId.value = workflowEngines.value[0].id
  selectedEngine.value = workflowEngines.value[0]
  await loadOperators(workflowEngineId.value)
  if (needsSparkRuntime()) await ensureSparkRuntime()
}

async function requestEngineChange(engineId) {
  engineSelectRef.value?.blur()
  if (editorBusy.value) return
  if (!engineId || engineId === workflowEngineId.value) return
  if (workflowData.value.tasks.length === 0) {
    await applyEngineSwitch(engineId)
    return
  }
  pendingWorkflowEngineId.value = engineId
  engineSwitchDialogVisible.value = true
}

function cancelEngineSwitch() {
  if (switchingEngine.value || saving.value) return
  engineSwitchDialogVisible.value = false
  pendingWorkflowEngineId.value = null
}

function handleEngineSwitchDialogClosed() {
  if (pendingAction.value !== 'switchEngine') {
    pendingWorkflowEngineId.value = null
    nextTick(() => focusElement(engineSelectRef.value))
  }
}

async function saveAndSwitchEngine() {
  if (saving.value || switchingEngine.value) return
  if (!pendingWorkflowEngineId.value) return

  if (!currentTaskId.value) {
    engineSwitchDialogVisible.value = false
    pendingAction.value = 'switchEngine'
    openSaveDialog(false)
    return
  }

  saving.value = true
  try {
    if (await saveCurrentTask()) {
      await applyEngineSwitch(pendingWorkflowEngineId.value, { saved: true })
    }
  } finally {
    saving.value = false
  }
}

async function clearAndSwitchEngine() {
  if (saving.value || switchingEngine.value) return
  if (pendingWorkflowEngineId.value) {
    await applyEngineSwitch(pendingWorkflowEngineId.value)
  }
}

async function applyEngineSwitch(engineId, { saved = false } = {}) {
  const dialogWasVisible = engineSwitchDialogVisible.value
  switchingEngine.value = true
  engineSwitchDialogVisible.value = false
  canvasRef.value?.clearGraph()
  selectedNode.value = null
  resetValidationState()
  currentTaskId.value = null
  currentTaskName.value = ''
  currentTask.value = null
  resetStorageBindingState()
  workflowEngineId.value = engineId
  selectedEngine.value = workflowEngines.value.find(engine => engine.id === engineId) || null
  sparkRuntimeId.value = null

  try {
    await loadOperators(engineId)
    if (needsSparkRuntime()) await ensureSparkRuntime()
    markSaved()
    await clearTaskRouteQuery()
    const messageKey = saved
      ? 'develop.workflow.saveAndSwitchSuccess'
      : 'develop.workflow.engineSwitchSuccess'
    ElMessage.success(t(messageKey, { name: selectedEngine.value?.name || '-' }))
  } finally {
    if (!dialogWasVisible) pendingWorkflowEngineId.value = null
    switchingEngine.value = false
  }
}

async function clearTaskRouteQuery() {
  const query = { ...route.query }
  const hasTaskQuery = Object.prototype.hasOwnProperty.call(query, 'id') ||
    Object.prototype.hasOwnProperty.call(query, 'taskId')
  if (!hasTaskQuery) return
  delete query.id
  delete query.taskId
  await router.replace({ query })
}

async function setTaskRouteQuery(taskId) {
  if (!taskId) return
  const query = { ...route.query, id: String(taskId) }
  delete query.taskId
  if (String(route.query.id || '') === String(taskId) && !route.query.taskId) return
  await router.replace({ query })
}

async function ensureSparkRuntime() {
  await loadSparkRuntimes()
  if (!sparkRuntimeId.value && sparkRuntimes.value.length) sparkRuntimeId.value = sparkRuntimes.value[0].id
}

function needsSparkRuntime() {
  return isSparkWorkflowEngine(selectedEngine.value)
}

function getEngineTag(engine) {
  return engine?.engine_type || '-'
}

function formatRuntimeLabel(runtime) {
  const connection = runtime.connection_info || {}
  const location = connection.spark_master || [connection.host, connection.port].filter(Boolean).join(':')
  return location ? `${runtime.name} (${location})` : runtime.name
}

async function handleSave() {
  if (editorBusy.value) return
  if (!guardWorkflowSavable()) return
  if (!currentTaskId.value) {
    openSaveDialog(false)
    return
  }
  saving.value = true
  try {
    if (await saveCurrentTask()) await validateAfterSave()
  } finally {
    saving.value = false
  }
}

function openSaveDialog(asCopy) {
  saveAsMode.value = asCopy
  Object.assign(saveForm, {
    name: asCopy && currentTaskName.value
      ? `${currentTaskName.value}${t('develop.workflow.copySuffix')}`
      : currentTaskName.value,
    display_name: currentTask.value?.display_name || '',
    description: currentTask.value?.description || ''
  })
  saveDialogVisible.value = true
}

function cancelSaveDialog() {
  if (saving.value) return
  saveDialogVisible.value = false
  if (pendingAction.value === 'switchEngine') {
    pendingWorkflowEngineId.value = null
  }
  pendingAction.value = null
}

function handleSaveDialogClosed() {
  if (saveAsMode.value && !pendingAction.value) restoreMoreActionsFocus()
}

function focusSaveName() {
  focusElement(saveNameInputRef.value)
}

function focusExecuteInputs() {
  focusElement(executeInputsRef.value)
}

function restoreExecuteTriggerFocus() {
  nextTick(() => focusElement(executeTriggerButtonRef.value))
}

function focusJsonClose() {
  focusElement(jsonCloseButtonRef.value)
}

function restoreMoreActionsFocus() {
  nextTick(() => {
    focusElement(moreActionsButtonRef.value)
  })
}

async function confirmSave() {
  if (saving.value) return
  if (!saveForm.name.trim()) {
    ElMessage.warning(t('develop.workflow.workflowNameRequired'))
    return
  }
  saving.value = true
  try {
    let saved = false
    try {
      const taskData = buildTaskData({
        name: saveForm.name.trim(),
        displayName: saveForm.display_name,
        description: saveForm.description
      })
      const savedSignature = editorStateSignature()
      const task = await saveWorkflowTask(taskData)
      currentTaskId.value = task.id
      currentTaskName.value = task.name
      currentTask.value = task
      saveDialogVisible.value = false
      saveAsMode.value = false
      markSaved(savedSignature)
      await loadStorageEngineBindings(task.id)
      saved = true
    } catch (error) {
      ElMessage.error(t('develop.workflow.saveFailed') + (error.response?.data?.error || error.message))
    }

    if (!saved) return
    const action = pendingAction.value
    pendingAction.value = null
    if (action === 'execute') {
      await setTaskRouteQuery(currentTaskId.value)
      ElMessage.success(t('develop.workflow.saveSuccess'))
      if (!guardStorageBindingsExecutable()) return
      openExecuteDialog()
    } else if (action === 'switchEngine') {
      await applyEngineSwitch(pendingWorkflowEngineId.value, { saved: true })
    } else {
      await setTaskRouteQuery(currentTaskId.value)
      await validateAfterSave()
    }
  } finally {
    saving.value = false
  }
}

async function saveCurrentTask() {
  if (!currentTaskId.value) return false
  try {
    const taskData = buildTaskData({
      name: currentTaskName.value,
      displayName: currentTask.value?.display_name || '',
      description: currentTask.value?.description || ''
    })
    const savedSignature = editorStateSignature()
    const task = await updateWorkflowTask(currentTaskId.value, taskData)
    currentTask.value = task
    currentTaskName.value = task.name
    markSaved(savedSignature)
    await loadStorageEngineBindings(task.id)
    return true
  } catch (error) {
    ElMessage.error(t('develop.workflow.saveFailed') + (error.response?.data?.error || error.message))
    return false
  }
}

function buildTaskData({ name, displayName, description }) {
  return {
    name,
    displayName,
    description,
    workflow: canvasRef.value?.getWorkflow() || workflowData.value,
    editorLayout: editorLayout.value,
    inputs: currentTask.value?.content?.inputs || {},
    workflowEngineId: workflowEngineId.value,
    sparkRuntimeId: sparkRuntimeId.value,
    requiresSparkRuntime: needsSparkRuntime()
  }
}

async function handleExecute() {
  if (preparingExecution.value || saving.value || executing.value) return
  if (!guardWorkflowExecutable()) return
  preparingExecution.value = true
  try {
    if (!await validateCurrentWorkflow()) {
      notifyValidationFailure()
      return
    }
    if (!currentTaskId.value) {
      pendingAction.value = 'execute'
      openSaveDialog(false)
      return
    }
    if (isDirty.value) {
      saving.value = true
      try {
        if (!await saveCurrentTask()) return
      } finally {
        saving.value = false
      }
    }
    if (!guardStorageBindingsExecutable()) return
    openExecuteDialog()
  } finally {
    preparingExecution.value = false
  }
}

function openExecuteDialog() {
  executeInputs.value = '{}'
  executeDialogVisible.value = true
}

async function confirmExecute() {
  if (executing.value) return
  let inputs
  try {
    inputs = JSON.parse(executeInputs.value)
    if (!inputs || typeof inputs !== 'object' || Array.isArray(inputs)) throw new Error()
  } catch {
    ElMessage.warning(t('develop.workflow.invalidJson'))
    return
  }

  executing.value = true
  try {
    const execution = await executeWorkflowTask(currentTaskId.value, inputs)
    executeDialogVisible.value = false
    ElMessage.success(t('develop.workflow.executeSubmitted'))
    await openMonitorExecution(execution.execution_id)
  } catch (error) {
    ElMessage.error(t('develop.workflow.executeFailed') + (error.response?.data?.error || error.message))
  } finally {
    executing.value = false
  }
}

function guardWorkflowReady() {
  if (!workflowEngineId.value) ElMessage.warning(t('develop.workflow.selectEngineFirst'))
  else if (needsSparkRuntime() && !sparkRuntimeId.value) ElMessage.warning(t('develop.workflow.selectSparkRuntimeFirst'))
  else if (!hasValidWorkflow.value) ElMessage.warning(t('develop.workflow.emptyWorkflow'))
  else return true
  return false
}

function guardWorkflowExecutable() {
  if (workflowEngineUnavailable.value) {
    ElMessage.warning(t('develop.workflow.engineUnavailableAction'))
    return false
  }
  if (!guardStorageBindingsExecutable()) return false
  return guardWorkflowReady()
}

function guardStorageBindingsExecutable() {
  if (!currentTaskId.value) return true
  if (storageBindingsLoading.value) {
    ElMessage.warning(t('develop.workflow.storageBindingsChecking'))
    return false
  }
  if (storageBindingsLoadError.value) {
    ElMessage.warning(t('develop.workflow.storageBindingsCheckFailed'))
    return false
  }
  if (unavailableStorageBindings.value.length) {
    ElMessage.warning(t('develop.workflow.storageBindingsUnavailableAction', {
      count: unavailableStorageBindings.value.length
    }))
    return false
  }
  return true
}

function guardWorkflowSavable() {
  if (!workflowEngineId.value) ElMessage.warning(t('develop.workflow.selectEngineFirst'))
  else if (!hasValidWorkflow.value) ElMessage.warning(t('develop.workflow.emptyWorkflow'))
  else return true
  return false
}

async function handleMoreCommand(command) {
  if (editorBusy.value) return
  if (command === 'saveAs') {
    if (guardWorkflowSavable()) openSaveDialog(true)
  } else if (command === 'viewJson') handleViewJSON()
  else if (command === 'import') fileInputRef.value?.click()
  else if (command === 'export') handleExport()
  else if (command === 'clear') await handleClear()
}

async function handleClear() {
  if (editorBusy.value) return
  try {
    await ElMessageBox.confirm(
      t('develop.workflow.clearConfirmMsg'),
      t('develop.workflow.clearConfirmTitle'),
      workflowConfirmOptions({ danger: true })
    )
    canvasRef.value?.clearGraph()
    selectedNode.value = null
    resetValidationState()
    ElMessage.success(t('develop.workflow.clearSuccess'))
  } catch {
    // 用户取消
  }
}

function handleViewJSON() {
  if (!guardWorkflowReady()) return
  workflowJSON.value = JSON.stringify(buildWorkflowExportPayload({
    workflow: canvasRef.value?.getWorkflow() || workflowData.value,
    workflowEngineId: workflowEngineId.value,
    sparkRuntimeId: sparkRuntimeId.value,
    requiresSparkRuntime: needsSparkRuntime()
  }), null, 2)
  jsonDialogVisible.value = true
}

async function copyJSON() {
  try {
    await navigator.clipboard.writeText(workflowJSON.value)
    ElMessage.success(t('develop.workflow.copiedToClipboard'))
  } catch {
    ElMessage.error(t('develop.workflow.copyFailed'))
  }
}

function handleExport() {
  if (!guardWorkflowReady()) return
  const workflow = canvasRef.value?.getWorkflow() || workflowData.value
  const url = URL.createObjectURL(new Blob([JSON.stringify(workflow, null, 2)], { type: 'application/json' }))
  const link = document.createElement('a')
  link.href = url
  link.download = `${currentTaskName.value || 'workflow'}.json`
  link.click()
  URL.revokeObjectURL(url)
  ElMessage.success(t('develop.workflow.exportSuccess'))
}

async function handleFileChange(event) {
  const file = event.target.files[0]
  if (!file) return
  if (editorBusy.value) {
    event.target.value = ''
    return
  }
  importing.value = true
  try {
    const workflow = JSON.parse(await file.text())
    if (!isStandardWorkflowDefinition(workflow)) throw new Error(t('develop.workflow.invalidWorkflowFormat'))
    workflowData.value = workflow
    editorLayout.value = {}
    selectedNode.value = null
    resetValidationState()
    ElMessage.success(t('develop.workflow.importSuccess'))
  } catch (error) {
    ElMessage.error(t('develop.workflow.importFailed') + error.message)
  } finally {
    importing.value = false
    event.target.value = ''
  }
}

function resetValidationState() {
  validationRevision += 1
  validating.value = false
  validationPopoverVisible.value = false
  validationResult.value = null
  validationRequestError.value = ''
}

async function validateCurrentWorkflow() {
  if (!hasValidWorkflow.value || !workflowEngineId.value) return false
  if (workflowEngineUnavailable.value) {
    resetValidationState()
    validationRequestError.value = t('develop.workflow.engineUnavailableAction')
    return false
  }
  const revision = ++validationRevision
  validating.value = true
  validationRequestError.value = ''
  const clientErrors = canvasRef.value?.getClientValidationIssues() || []
  if (clientErrors.length) {
    if (revision !== validationRevision) return false
    validationResult.value = {
      valid: false,
      errors: clientErrors,
      warnings: []
    }
    validating.value = false
    return false
  }
  try {
    const result = await validateWorkflowDefinition(
      workflowEngineId.value,
      canvasRef.value?.getWorkflow() || workflowData.value
    )
    if (revision !== validationRevision) return false
    const errors = deduplicateIssues(result.errors || [])
    validationResult.value = {
      ...result,
      valid: errors.length === 0,
      errors
    }
    return errors.length === 0
  } catch (error) {
    if (revision !== validationRevision) return false
    validationRequestError.value = error.response?.data?.error || error.message
    return false
  } finally {
    if (revision === validationRevision) validating.value = false
  }
}

async function validateAfterSave() {
  if (unavailableStorageBindings.value.length) {
    resetValidationState()
    ElMessage.warning(t('develop.workflow.saveSuccessStorageUnavailable', {
      count: unavailableStorageBindings.value.length
    }))
    return false
  }
  if (storageBindingsLoadError.value) {
    resetValidationState()
    ElMessage.warning(t('develop.workflow.saveSuccessStorageCheckFailed'))
    return false
  }
  if (workflowEngineUnavailable.value) {
    resetValidationState()
    ElMessage.warning(t('develop.workflow.saveSuccessEngineUnavailable'))
    return false
  }
  await validateCurrentWorkflow()
  if (validationErrors.value.length) {
    ElMessage.warning(t('develop.workflow.saveSuccessWithIssues', {
      count: validationErrors.value.length
    }))
  } else if (validationRequestError.value) {
    ElMessage.warning(t('develop.workflow.saveSuccessValidationUnavailable'))
  } else if (validationWarnings.value.length) {
    ElMessage.warning(t('develop.workflow.saveSuccessWithWarnings', {
      count: validationWarnings.value.length
    }))
  } else {
    ElMessage.success(t('develop.workflow.saveSuccess'))
  }
}

function notifyValidationFailure() {
  if (validationErrors.value.length) {
    ElMessage.warning(t('develop.workflow.validationFailed', {
      count: validationErrors.value.length
    }))
  } else if (validationRequestError.value) {
    ElMessage.error(t('develop.workflow.validationUnavailable'))
  }
}

function deduplicateIssues(issues) {
  const seen = new Set()
  return issues.filter(issue => {
    const key = `${issue.code}:${issue.path}`
    if (seen.has(key)) return false
    seen.add(key)
    return true
  })
}

function mapValidationIssues(issues, severity) {
  return issues.map(issue => {
    const match = String(issue.path || '').match(/tasks\[(\d+)\]/)
    const index = match ? Number(match[1]) : -1
    const task = index >= 0 ? workflowData.value.tasks[index] : null
    const paramName = validationIssueParamName(issue)
    return {
      ...issue,
      severity,
      paramName,
      paramLabel: validationIssueParamLabel(task, paramName),
      nodeId: task?.id || null,
      nodeLabel: validationIssueNodeLabel(task)
    }
  })
}

function validationIssueNodeLabel(task) {
  if (!task) return t('develop.workflow.workflowLevelIssue')
  const operator = workflowOperatorDefinition(task.operator)
  const displayName = operator?.display_name || operator?.displayName || task.operator
  return `${displayName} · ${task.id}`
}

function validationIssueParamLabel(task, paramName) {
  if (!task || !paramName) return paramName
  const parameters = workflowOperatorDefinition(task.operator)?.public_parameters || []
  const resourceParameter = parameters.find(parameter => {
    if (parameter.ui_type !== 'resource_tree_picker') return false
    const binding = getResourceBinding(parameter) || {}
    return [
      binding.locator_param,
      binding.parent_locator_param,
      binding.name_param,
      binding.type_param,
      binding.geometry_column_param
    ].includes(paramName)
  })
  if (resourceParameter) return formatValidationParamLabel(resourceParameter, paramName)

  const directParameter = parameters.find(parameter => parameter.name === paramName)
  return directParameter ? formatValidationParamLabel(directParameter, paramName) : paramName
}

function formatValidationParamLabel(parameter, paramName) {
  const displayName = parameter.display_name || parameter.displayName || parameter.name
  return hasDistinctText(displayName, paramName) ? `${displayName} (${paramName})` : paramName
}

function workflowOperatorDefinition(operatorName) {
  return operators.value.find(operator => operator.name === operatorName || operator.id === operatorName)
}

async function focusValidationIssue(issue) {
  validationPopoverVisible.value = false
  if (!issue.nodeId) return
  canvasRef.value?.selectNode(issue.nodeId)
  rightPanelCollapsed.value = false
  await nextTick()
  await paramsPanelRef.value?.focusParam(validationIssueParamName(issue))
}

function editorStateSignature() {
  return JSON.stringify({
    workflow: workflowData.value,
    editorLayout: editorLayout.value,
    workflowEngineId: workflowEngineId.value,
    sparkRuntimeId: sparkRuntimeId.value
  })
}

function markSaved(signature = editorStateSignature()) {
  savedStateSignature.value = signature
}

async function loadTask(taskId) {
  try {
    const task = await getDevTask(taskId)
    currentTaskId.value = task.id
    currentTaskName.value = task.name
    currentTask.value = task

    const config = task.execution_config || {}
    workflowEngineId.value = config.engine_id
    selectedEngine.value = workflowEngines.value.find(engine => engine.id === config.engine_id) || null
    await loadOperators(workflowEngineId.value)
    if (needsSparkRuntime()) {
      await loadSparkRuntimes()
      sparkRuntimeId.value = config.engine_specific?.spark_cluster_id || null
    }

    if (!task.content?.workflow_definition) {
      ElMessage.warning(t('develop.workflow.noWorkflowContent'))
      return
    }
    editorLayout.value = task.editor_layout || {}
    workflowData.value = task.content.workflow_definition
    await nextTick()
    markSaved()
    resetValidationState()
    await loadStorageEngineBindings(task.id)
  } catch (error) {
    ElMessage.error(t('develop.workflow.loadTaskFailed') + (error.response?.data?.error || error.message))
  }
}

function toggleAiPanel() {
  if (editorBusy.value) return
  aiDialogOpen.value = !aiDialogOpen.value
  if (aiDialogOpen.value) nextTick(() => aiInputRef.value?.focus())
}

async function generateWorkflow() {
  if (editorBusy.value) return
  if (!aiQuery.value.trim()) {
    ElMessage.warning(t('develop.workflow.describeWorkflow'))
    return
  }
  if (!workflowEngineId.value) {
    ElMessage.warning(t('develop.workflow.selectEngineFirst'))
    return
  }
  if (workflowEngineUnavailable.value) {
    ElMessage.warning(t('develop.workflow.engineUnavailableAction'))
    return
  }

  generating.value = true
  try {
    const currentWorkflow = canvasRef.value?.getWorkflow() || workflowData.value
    const resources = currentWorkflow.tasks
      .filter(task => task.operator === 'load' && task.params?.locator)
      .map(task => ({ role: task.id, locator: task.params.locator }))
    const result = await generateWorkflowFromNL({
      query: aiQuery.value,
      workflow_engine_id: workflowEngineId.value,
      resources
    })
    const resolved = resolveWorkflowGenerationResult(result)
    if (resolved.clarificationKey) {
      ElMessage.warning(t(resolved.clarificationKey))
      return
    }
    workflowData.value = resolved.workflow
    editorLayout.value = {}
    selectedNode.value = null
    aiDialogOpen.value = false
    resetValidationState()
    ElMessage.success(t('develop.workflow.generateSuccess', { count: resolved.workflow.tasks.length }))
  } catch (error) {
    ElMessage.error(error.response?.data?.detail || error.message || t('develop.workflow.generateFailed'))
  } finally {
    generating.value = false
  }
}

function handleBeforeUnload(event) {
  if (!isDirty.value) return
  event.preventDefault()
  event.returnValue = ''
}

function workflowConfirmOptions({ danger = false } = {}) {
  return {
    type: 'warning',
    customClass: 'addp-message-box',
    confirmButtonText: t('develop.workflow.confirm'),
    cancelButtonText: t('develop.workflow.cancel'),
    ...(danger ? { confirmButtonClass: 'el-button--danger' } : {})
  }
}

onBeforeRouteLeave(async () => {
  if (!isDirty.value) return true
  try {
    await ElMessageBox.confirm(
      t('develop.workflow.leaveConfirm'),
      t('develop.workflow.unsaved'),
      workflowConfirmOptions()
    )
    return true
  } catch {
    return false
  }
})

onMounted(async () => {
  window.addEventListener('beforeunload', handleBeforeUnload)
  await loadWorkflowEngines()
  const taskId = firstQueryValue(route.query.id || route.query.taskId)
  if (taskId) await loadTask(taskId)
  else {
    await selectDefaultEngine()
    markSaved()
  }
})

onBeforeUnmount(() => {
  window.removeEventListener('beforeunload', handleBeforeUnload)
})

function firstQueryValue(value) {
  return Array.isArray(value) ? value[0] || '' : value || ''
}
</script>

<style scoped>
.workflow-editor-page {
  height: 100vh;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  background: var(--addp-bg-secondary);
}

.editor-header {
  min-height: 72px;
  padding: 10px 16px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  flex-shrink: 0;
  background: var(--addp-bg-primary);
  border-bottom: 1px solid var(--addp-border-color);
}

.editor-identity,
.title-row,
.engine-row,
.primary-actions,
.engine-option {
  display: flex;
  align-items: center;
}

.editor-identity {
  min-width: 0;
  gap: 10px;
}

.title-block {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.title-row {
  gap: 8px;
}

.title-row h2 {
  max-width: 320px;
  margin: 0;
  overflow: hidden;
  color: var(--addp-text-primary);
  font-size: 16px;
  font-weight: 600;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.engine-row {
  min-width: 0;
  gap: 8px;
}

.engine-label {
  color: var(--addp-text-tertiary);
  font-size: 12px;
  white-space: nowrap;
}

.engine-select {
  width: 230px;
}

.engine-status-tag {
  flex-shrink: 0;
}

.storage-binding-status {
  flex-shrink: 0;
}

.engine-option {
  justify-content: space-between;
  gap: 12px;
}

.dialog-body-stack {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.workflow-dialog-form :deep(.el-form-item:last-child) {
  margin-bottom: 0;
}

.execution-summary {
  margin-bottom: 16px;
  padding-bottom: 12px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  color: var(--addp-text-secondary);
  border-bottom: 1px solid var(--addp-border-color-light);
}

.execution-summary strong {
  color: var(--addp-text-primary);
  font-size: 16px;
}

.storage-engine-select {
  width: 100%;
}

.binding-source-value {
  color: var(--addp-text-primary);
  overflow-wrap: anywhere;
}

.binding-resource-types {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.primary-actions {
  gap: 8px;
  flex-shrink: 0;
}

.editor-content {
  position: relative;
  min-height: 0;
  flex: 1;
  display: flex;
  overflow: hidden;
}

.left-panel,
.right-panel {
  display: flex;
  flex-direction: column;
  flex-shrink: 0;
  background: var(--addp-bg-primary);
}

.left-panel {
  width: 270px;
  border-right: 1px solid var(--addp-border-color);
}

.right-panel {
  width: 360px;
  border-left: 1px solid var(--addp-border-color);
}

.canvas-panel {
  min-width: 0;
  min-height: 0;
  flex: 1;
  display: flex;
  flex-direction: column;
  background: var(--addp-bg-secondary);
}

.panel-header {
  min-height: 44px;
  padding: 6px 12px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  flex-shrink: 0;
  border-bottom: 1px solid var(--addp-border-color);
}

.panel-title-group {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.panel-title {
  overflow: hidden;
  color: var(--addp-text-primary);
  font-size: 13px;
  font-weight: 600;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.panel-subtitle {
  overflow: hidden;
  color: var(--addp-text-tertiary);
  font-family: 'Monaco', 'Menlo', 'Consolas', monospace;
  font-size: 10px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.panel-body {
  min-height: 0;
  flex: 1;
  overflow: hidden;
}

.operator-panel-body {
  padding: 12px;
}

.params-panel-body {
  overflow: auto;
}

.header-validation {
  min-width: 0;
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
}

.validation-summary {
  min-width: 0;
  max-width: 520px;
  min-height: 32px;
  padding: 6px 10px;
  display: flex;
  align-items: center;
  gap: 6px;
  color: var(--addp-text-secondary);
  font-size: 12px;
  letter-spacing: 0;
  background: var(--addp-bg-secondary);
  border: 1px solid var(--addp-border-color);
  border-radius: 4px;
}

.validation-trigger {
  width: 100%;
  font: inherit;
  text-align: left;
  cursor: pointer;
}

.validation-summary.is-error {
  color: var(--el-color-danger);
  border-color: var(--el-color-danger);
}

.validation-summary.is-valid {
  color: var(--el-color-success);
  border-color: var(--el-color-success);
}

.validation-summary.is-warning {
  color: var(--el-color-warning);
  border-color: var(--el-color-warning);
}

.validation-summary-text {
  flex-shrink: 0;
  font-weight: 600;
}

.validation-preview {
  min-width: 0;
  padding-left: 8px;
  overflow: hidden;
  color: var(--addp-text-primary);
  text-overflow: ellipsis;
  white-space: nowrap;
  border-left: 1px solid var(--addp-border-color);
}

.validation-list {
  max-height: 280px;
  display: flex;
  flex-direction: column;
  overflow: auto;
}

.validation-group + .validation-group {
  margin-top: 8px;
  padding-top: 8px;
  border-top: 1px solid var(--addp-border-color);
}

.validation-group-title {
  padding: 4px;
  overflow: hidden;
  color: var(--addp-text-secondary);
  font-size: 12px;
  font-weight: 600;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.validation-item {
  width: 100%;
  padding: 9px 4px;
  display: flex;
  align-items: flex-start;
  gap: 8px;
  color: var(--addp-text-primary);
  font: inherit;
  text-align: left;
  background: transparent;
  border: 0;
  border-bottom: 1px solid var(--addp-border-color-light);
  cursor: pointer;
}

.validation-item:hover {
  background: var(--addp-bg-secondary);
}

.validation-item-icon {
  margin-top: 2px;
  flex-shrink: 0;
}

.validation-item-icon.is-error {
  color: var(--el-color-danger);
}

.validation-item-icon.is-warning {
  color: var(--el-color-warning);
}

.validation-item-content {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.validation-item-param {
  color: var(--addp-text-secondary);
  font-family: 'Monaco', 'Menlo', 'Consolas', monospace;
  font-size: 11px;
}

.validation-item-message {
  line-height: 1.45;
  overflow-wrap: anywhere;
}

.open-inspector {
  position: absolute;
  top: 54px;
  right: 12px;
  z-index: 20;
}

.json-viewer {
  max-height: 60vh;
  padding: 12px;
  overflow: auto;
  background: var(--addp-bg-secondary);
  border-radius: 4px;
}

.json-viewer pre {
  margin: 0;
  color: var(--addp-text-primary);
  font-family: 'Monaco', 'Menlo', 'Consolas', monospace;
  font-size: 12px;
  line-height: 1.5;
  white-space: pre-wrap;
  word-break: break-word;
}

.hidden-file-input {
  display: none;
}

.ai-fab-wrapper {
  position: fixed;
  right: 22px;
  bottom: 48px;
  z-index: 1000;
  display: flex;
  align-items: flex-end;
  gap: 10px;
}

.ai-fab {
  width: 42px;
  height: 42px;
  flex-shrink: 0;
  box-shadow: var(--addp-shadow-hover);
}

.ai-inline-panel {
  width: 320px;
  padding: 14px;
  display: flex;
  flex-direction: column;
  gap: 10px;
  background: var(--addp-bg-primary);
  border: 1px solid var(--addp-border-color);
  border-radius: 8px;
  box-shadow: var(--addp-shadow-card);
}

.ai-panel-header,
.ai-panel-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.ai-panel-footer {
  justify-content: flex-end;
}

.ai-panel-title {
  color: var(--addp-text-primary);
  font-size: 13px;
  font-weight: 600;
}

.ai-panel-input :deep(.el-textarea__inner) {
  resize: none;
}

.ai-slide-enter-active,
.ai-slide-leave-active {
  transition: opacity 0.18s ease, transform 0.18s ease;
}

.ai-slide-enter-from,
.ai-slide-leave-to {
  opacity: 0;
  transform: translateX(12px);
}

@media (max-width: 1280px) {
  .editor-header {
    align-items: flex-start;
  }

  .engine-select {
    width: 190px;
  }

  .engine-row {
    flex-wrap: wrap;
  }

  .validation-preview {
    max-width: 180px;
  }

  .right-panel {
    position: absolute;
    top: 0;
    right: 0;
    bottom: 0;
    z-index: 30;
    width: min(360px, 88vw);
    box-shadow: var(--addp-shadow-card);
  }
}

@media (max-width: 900px) {
  .editor-header {
    min-height: 104px;
    flex-wrap: wrap;
  }

  .editor-identity {
    width: 100%;
  }

  .engine-row {
    flex-wrap: wrap;
  }

  .primary-actions {
    margin-left: auto;
  }

  .header-validation {
    order: 3;
    width: 100%;
    justify-content: flex-start;
  }

  .validation-summary {
    max-width: 100%;
  }

  .left-panel {
    position: absolute;
    top: 0;
    bottom: 0;
    left: 0;
    z-index: 30;
    width: min(290px, 88vw);
    box-shadow: var(--addp-shadow-card);
  }
}
</style>
