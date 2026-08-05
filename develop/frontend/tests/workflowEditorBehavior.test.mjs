import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { resolve } from 'node:path'
import enMessages from '../src/i18n/en.json' with { type: 'json' }
import zhCnMessages from '../src/i18n/zh-cn.json' with { type: 'json' }
import {
  sortEntriesByOrder,
  summarizeExecutionResource
} from '../../../common-frontend/basic/src/utils/executionParameterPresentation.js'

const editor = await readFile(resolve('src/views/WorkflowEditor.vue'), 'utf8')
const paramsPanel = await readFile(resolve('src/components/workflow/OperatorParamsPanel.vue'), 'utf8')
const operatorPalette = await readFile(resolve('src/components/workflow/OperatorPalette.vue'), 'utf8')
const sharedTheme = await readFile(resolve('../../common-frontend/basic/src/styles/theme.css'), 'utf8')
const statusAnnouncer = await readFile(resolve('../../common-frontend/basic/src/components/StatusAnnouncer.vue'), 'utf8')
const executionParameterForm = await readFile(resolve('../../common-frontend/basic/src/components/ExecutionParameterForm.vue'), 'utf8')
const executionParameterPresentation = await readFile(resolve('../../common-frontend/basic/src/utils/executionParameterPresentation.js'), 'utf8')
const commonZhCnMessages = JSON.parse(await readFile(resolve('../../common-frontend/basic/src/i18n/zh-cn.json'), 'utf8'))
const commonEnMessages = JSON.parse(await readFile(resolve('../../common-frontend/basic/src/i18n/en.json'), 'utf8'))
const operatorItemSource = operatorPalette.slice(
  operatorPalette.indexOf('<div\n              v-for="operator in category.operators"'),
  operatorPalette.indexOf('</el-collapse-item>')
)
const operatorItemOpening = operatorItemSource.slice(0, operatorItemSource.indexOf('>') + 1)
const executeDialogSource = editor.slice(
  editor.indexOf('<el-dialog\n      v-model="executeDialogVisible"'),
  editor.indexOf('<el-dialog\n      v-model="jsonDialogVisible"')
)
const saveDialogSource = editor.slice(
  editor.indexOf('<el-dialog\n      v-model="saveDialogVisible"'),
  editor.indexOf('<el-dialog\n      v-model="executeDialogVisible"')
)
const engineSwitchDialogSource = editor.slice(
  editor.indexOf('<el-dialog\n      v-model="engineSwitchDialogVisible"'),
  editor.indexOf('<el-dialog\n      v-model="storageBindingDialogVisible"')
)
const resourcePickerDialogSource = paramsPanel.slice(
  paramsPanel.indexOf('<el-dialog\n      v-model="resourcePickerDialogVisible"'),
  paramsPanel.indexOf('</el-dialog>')
)

assert.match(editor, /:model-value="workflowEngineId"/)
assert.match(editor, /ref="engineSelectRef"/)
assert.match(editor, /v-if="workflowEngineUnavailable"[\s\S]*engineUnavailableOption/)
assert.match(editor, /engineSelectRef\.value\?\.blur\(\)/)
assert.match(editor, /class="editor-content" :inert="editorBusy" :aria-busy="editorBusy"/)
assert.match(editor, /<StatusAnnouncer :label="t\('develop\.workflow\.statusLabel'\)" :message="workflowAnnouncement"/)
assert.match(editor, /const workflowAnnouncement = computed/)
assert.match(editor, /develop\.workflow\.savingStatus/)
assert.match(editor, /develop\.workflow\.preparingExecution/)
assert.match(editor, /develop\.workflow\.submittingExecution/)
assert.match(statusAnnouncer, /role="status"/)
assert.match(statusAnnouncer, /aria-live="polite"/)
assert.match(statusAnnouncer, /aria-atomic="true"/)
assert.match(statusAnnouncer, /:aria-label="label \|\| undefined"/)
assert.match(sharedTheme, /\.addp-visually-hidden \{[\s\S]*clip: rect/)
assert.match(editor, /:loading="executionButtonLoading"/)
assert.match(executeDialogSource, /:close-on-click-modal="!executing"/)
assert.match(executeDialogSource, /:close-on-press-escape="!executing"/)
assert.match(executeDialogSource, /:show-close="!executing"/)
assert.match(executeDialogSource, /<ExecutionParameterForm[\s\S]*v-model="executionParameters"[\s\S]*:contract="executionContract"[\s\S]*:disabled="executing"/)
assert.doesNotMatch(executeDialogSource, /type="textarea"/)
assert.match(executeDialogSource, /@opened="focusExecutionParameters"/)
assert.match(executeDialogSource, /<el-button ref="executeCancelButtonRef" :disabled="executing" @click="executeDialogVisible = false">/)
assert.match(executeDialogSource, /:loading="executing"[\s\S]*:disabled="executing \|\| executionContractLoading \|\| !executionContract"[\s\S]*@click="confirmExecute"/)
assert.match(saveDialogSource, /:close-on-click-modal="!saving"/)
assert.match(saveDialogSource, /:close-on-press-escape="!saving"/)
assert.match(saveDialogSource, /:show-close="!saving"/)
assert.match(saveDialogSource, /<el-form class="workflow-dialog-form" :model="saveForm" label-position="top" :disabled="saving">/)
assert.match(saveDialogSource, /<el-button :disabled="saving" @click="cancelSaveDialog">/)
assert.match(saveDialogSource, /:loading="saving" :disabled="saving" @click="confirmSave"/)
assert.doesNotMatch(editor, /width="500px"/)
assert.equal((editor.match(/class="addp-dialog"/g) || []).length, 5)
assert.match(saveDialogSource, /label-position="top"/)
assert.match(executeDialogSource, /class="execution-summary"/)
assert.match(executeDialogSource, /develop\.workflow\.operatorCount/)
assert.doesNotMatch(executeDialogSource, /develop\.workflow\.taskCount/)
assert.doesNotMatch(executeDialogSource, /<el-input :model-value="workflowData\?\.tasks\?\.length \|\| 0" disabled/)
assert.match(executionParameterPresentation, /parseLocatorSafe/)
assert.match(executionParameterPresentation, /formatLocatorDisplayPath/)
assert.match(executionParameterForm, /resourceSummary/)
assert.match(executionParameterForm, /sortEntriesByOrder/)
assert.match(executionParameterForm, /disabled: \{ type: Boolean, default: false \}/)
assert.match(executionParameterForm, /defineExpose\(\{ focus \}\)/)
assert.doesNotMatch(executionParameterForm, /executionParameters\.default/)
assert.doesNotMatch(executionParameterForm, /executionParameters\.fixed/)
assert.equal(commonZhCnMessages.common.executionParameters.workflowConfiguration, '工作流配置')
assert.equal(commonZhCnMessages.common.executionParameters.executionOverride, '执行时指定')
assert.equal(commonEnMessages.common.executionParameters.workflowConfiguration, 'Workflow configuration')
assert.equal(commonEnMessages.common.executionParameters.executionOverride, 'Execution override')
assert.equal(commonZhCnMessages.common.executionParameters.geometryDetected, '已从资源元数据自动识别')
assert.equal(commonEnMessages.common.executionParameters.geometryDetected, 'Detected automatically from resource metadata')
assert.deepEqual(
  sortEntriesByOrder(
    { save: {}, load: {}, buffer: {} },
    { save: { order: 2 }, load: { order: 0 }, buffer: { order: 1 } }
  ).map(([name]) => name),
  ['load', 'buffer', 'save']
)
assert.deepEqual(
  summarizeExecutionResource(
    { ui: { resource_binding: { mode: 'existing' } } },
    { locator: 'addp://engine/2/path/public/farmland?type=table&item_id=51572' },
    { 2: { name: '业务 PostgreSQL', engine_type: 'postgresql' } }
  ),
  { status: 'resolved', engineId: 2, engineName: '业务 PostgreSQL', name: 'public.farmland', type: 'table' }
)
assert.deepEqual(
  summarizeExecutionResource(
    { ui: { resource_binding: { mode: 'target', type_values: { schema: 'table' } } } },
    { parent_locator: 'addp://engine/2/path/public?type=schema&node_id=265', name: 'farmland_buffer' },
    { 2: { name: '业务 PostgreSQL', engine_type: 'postgresql' } }
  ),
  { status: 'resolved', engineId: 2, engineName: '业务 PostgreSQL', name: 'public.farmland_buffer', type: 'table' }
)
assert.deepEqual(
  summarizeExecutionResource(
    { ui: { resource_binding: { mode: 'target', type_values: { bucket: 'file' } } } },
    { parent_locator: 'addp://engine/3/path/results/analysis?type=bucket', name: 'buffer.gpkg' },
    { 3: { name: '成果 MinIO', engine_type: 'minio' } }
  ),
  { status: 'resolved', engineId: 3, engineName: '成果 MinIO', name: 'results/analysis/buffer.gpkg', type: 'file' }
)
assert.deepEqual(
  summarizeExecutionResource(
    { ui: { resource_binding: { mode: 'existing' } } },
    { locator: 'addp://invalid/internal-value' }
  ),
  { status: 'configured', engineId: 0, engineName: '', name: '', type: '' }
)
assert.match(executionParameterForm, /geometryColumnOptions/)
assert.match(executionParameterForm, /listResourceTreeEngines/)
assert.doesNotMatch(executionParameterForm, /resource-child[\s\S]{0,400}<SchemaExecutionInput[\s\S]{0,300}geometry_column/)
assert.ok(engineSwitchDialogSource.indexOf('@click="clearAndSwitchEngine"') < engineSwitchDialogSource.indexOf('@click="saveAndSwitchEngine"'))
assert.match(resourcePickerDialogSource, /class="addp-dialog workflow-resource-picker-dialog"/)
assert.match(resourcePickerDialogSource, /resourcePickerDialogTitle/)
assert.match(resourcePickerDialogSource, /tree-height="clamp\(240px, 44vh, 420px\)"/)
assert.match(sharedTheme, /\.addp-dialog \{[\s\S]*--el-dialog-margin-top:[\s\S]*max-width: calc\(100vw - 24px\)/)
assert.match(sharedTheme, /\.addp-dialog \.el-dialog__body \{[\s\S]*max-height:[\s\S]*overflow: auto/)
assert.match(sharedTheme, /\.addp-dialog \.el-dialog__footer \{[\s\S]*flex-wrap: wrap/)
assert.match(sharedTheme, /\.addp-message-box \{[\s\S]*width: min\(420px, calc\(100vw - 24px\)\)/)
assert.match(sharedTheme, /\.addp-message-box \.el-message-box__title \{[\s\S]*font-weight: 600/)
assert.match(sharedTheme, /\.addp-message-box \.el-message-box__btns \{[\s\S]*flex-wrap: wrap/)
assert.match(editor, /@change="requestEngineChange"/)
assert.match(editor, /v-model="engineSwitchDialogVisible"/)
assert.match(editor, /develop\.workflow\.saveAndClear/)
assert.match(editor, /develop\.workflow\.clearAndSwitch/)
assert.match(editor, /workflowConfirmOptions\(\{ danger: true \}\)/)
assert.match(editor, /customClass: 'addp-message-box'/)
assert.match(editor, /confirmButtonText: t\('develop\.workflow\.confirm'\)/)
assert.match(editor, /cancelButtonText: t\('develop\.workflow\.cancel'\)/)
assert.match(editor, /confirmButtonClass: 'el-button--danger'/)
assert.match(editor, /pendingAction\.value = 'switchEngine'/)
assert.match(editor, /await saveCurrentTask\(\)/)
assert.match(editor, /canvasRef\.value\?\.clearGraph\(\)/)
assert.match(editor, /currentTaskId\.value = null/)
assert.match(editor, /navigateDevelopTaskEditor\(router, 'workflow', '', \{ history: 'replace' \}\)/)
assert.doesNotMatch(editor, /route\.query\.taskId/)
assert.doesNotMatch(editor, /:disabled="engineLocked"/)
assert.doesNotMatch(editor, /const engineLocked = computed/)
assert.match(editor, /await updateWorkflowTask\(currentTaskId\.value/)
assert.match(editor, /if \(workflow\.tasks\.length === 0\) editorLayout\.value = \{\}/)
assert.match(editor, /if \(workflowData\.value\.tasks\.length === 0\) return/)
assert.match(editor, /await executeWorkflowTask\(currentTaskId\.value, executionParameters\.value\)/)
assert.match(editor, /validateWorkflowDefinition\(/)
assert.match(editor, /getClientValidationIssues\(\)/)
assert.match(editor, /class="header-validation"/)
assert.match(editor, /storageBindingsUnavailable/)
assert.match(editor, /v-model="storageBindingDialogVisible"/)
assert.match(editor, /compatibleStorageEngines/)
assert.match(editor, /if \(isDirty\.value\) \{[\s\S]*saveBeforeStorageRebind/)
assert.match(editor, /await rebindWorkflowStorageEngine\(/)
assert.match(editor, /await loadStorageEngineBindings\(task\.id\)/)
assert.match(editor, /v-if="hasValidationStatus"/)
assert.match(editor, /v-model:visible="validationPopoverVisible"/)
assert.match(editor, /v-for="group in validationIssueGroups"/)
assert.match(editor, /class="validation-group-title"/)
assert.match(editor, /class="validation-item-param"/)
assert.match(editor, /ref="paramsPanelRef"/)
assert.match(editor, /:validation-issues="selectedNodeValidationIssues"/)
assert.match(editor, /paramsPanelRef\.value\?\.focusParam\(validationIssueParamName\(issue\)\)/)
assert.match(editor, /const canExecute = computed/)
const canSaveSource = editor.slice(
  editor.indexOf('const canSave = computed'),
  editor.indexOf('const canExecute = computed')
)
const canExecuteSource = editor.match(/const canExecute = computed\([\s\S]*?\n\)\)/)?.[0] || ''
assert.doesNotMatch(canSaveSource, /selectedEngine|workflowEngineUnavailable/)
assert.match(canExecuteSource, /selectedEngine\.value/)
assert.match(canExecuteSource, /storageBindingsExecutable\.value/)
assert.doesNotMatch(canExecuteSource, /validationResult/)
assert.match(canExecuteSource, /validationErrors\.value\.length === 0/)
assert.doesNotMatch(editor, /class="validation-bar"/)
assert.doesNotMatch(editor, /scheduleValidation|validationTimer/)
const handleSaveSource = editor.match(/async function handleSave\(\) \{[\s\S]*?\n\}/)?.[0] || ''
const confirmSaveSource = editor.match(/async function confirmSave\(\) \{[\s\S]*?\n\}/)?.[0] || ''
const saveAndSwitchSource = editor.match(/async function saveAndSwitchEngine\(\) \{[\s\S]*?\n\}/)?.[0] || ''
const saveCurrentTaskSource = editor.match(/async function saveCurrentTask\(\) \{[\s\S]*?\n\}/)?.[0] || ''
const validateCurrentWorkflowSource = editor.match(/async function validateCurrentWorkflow\(\) \{[\s\S]*?\n\}/)?.[0] || ''
const handleExecuteSource = editor.slice(
  editor.indexOf('async function handleExecute()'),
  editor.indexOf('function openExecuteDialog()')
)
const confirmExecuteSource = editor.slice(
  editor.indexOf('async function confirmExecute()'),
  editor.indexOf('function guardWorkflowReady()')
)
assert.match(handleSaveSource, /validateAfterSave/)
assert.match(handleSaveSource, /saving\.value = true/)
assert.match(confirmSaveSource, /validateAfterSave/)
assert.doesNotMatch(saveAndSwitchSource, /validateCurrentWorkflow/)
assert.doesNotMatch(saveCurrentTaskSource, /ElMessage\.success/)
assert.doesNotMatch(saveCurrentTaskSource, /saving\.value/)
assert.doesNotMatch(validateCurrentWorkflowSource, /ElMessage/)
assert.match(editor, /const preparingExecution = ref\(false\)/)
assert.match(editor, /const editorBusy = computed\(\(\) => \([\s\S]*saving\.value[\s\S]*preparingExecution\.value[\s\S]*switchingEngine\.value[\s\S]*generating\.value[\s\S]*importing\.value/)
assert.match(editor, /const executionButtonLoading = computed\(\(\) => preparingExecution\.value \|\| executing\.value\)/)
assert.match(handleExecuteSource, /preparingExecution\.value = true/)
assert.match(handleExecuteSource, /finally \{\s+preparingExecution\.value = false/)
assert.match(handleExecuteSource, /await validateCurrentWorkflow\(\)[\s\S]*await saveCurrentTask\(\)[\s\S]*openExecuteDialog\(\)/)
assert.match(handleExecuteSource, /guardStorageBindingsExecutable\(\)/)
assert.match(editor, /const task = await getDevTask\(currentTaskId\.value\)/)
assert.doesNotMatch(editor, /getProviderDevTask/)
assert.match(editor, /executionContract\.value = task\.execution_contract/)
assert.match(editor, /executionParameters\.value = \{\}/)
assert.match(confirmExecuteSource, /if \(executing\.value\) return/)
assert.match(confirmExecuteSource, /await executeWorkflowTask\(currentTaskId\.value, executionParameters\.value\)/)
assert.match(confirmSaveSource, /setTaskRouteQuery\(currentTaskId\.value\)/)
assert.match(editor, /async function setTaskRouteQuery\(taskId\)/)
assert.match(editor, /developTaskIDFromRoute\(route\)/)
assert.match(editor, /await applyWorkflowTaskRoute\(\{ initializeCreate: true \}\)/)
const workflowMountedSource = editor.match(/onMounted\(async \(\) => \{[\s\S]*?workflowTaskRouteReady\.value = true\s*\}\)/)?.[0] || ''
assert.doesNotMatch(workflowMountedSource, /developTaskIDFromRoute\(route\)/)
assert.match(editor, /function cancelSaveDialog\(\) \{\s+if \(saving\.value\) return/)
assert.match(editor, /async function handleFileChange\(event\) \{[\s\S]*importing\.value = true[\s\S]*importing\.value = false/)
assert.match(editor, /async function generateWorkflow\(\) \{\s+if \(editorBusy\.value\) return/)
assert.match(editor, /function guardWorkflowExecutable\(\) \{[\s\S]*workflowEngineUnavailable\.value[\s\S]*engineUnavailableAction/)
const loadOperatorsSource = editor.slice(
  editor.indexOf('async function loadOperators(engineId)'),
  editor.indexOf('async function loadSparkRuntimes()')
)
const unavailableOperatorSource = loadOperatorsSource.slice(
  0,
  loadOperatorsSource.indexOf('try {')
)
assert.ok(loadOperatorsSource.indexOf('workflowEngines.value.some') < loadOperatorsSource.indexOf('listOperatorsByWorkflowEngine'))
assert.match(unavailableOperatorSource, /operatorLoadError\.value = t\('develop\.workflow\.engineUnavailableHint'\)/)
assert.doesNotMatch(unavailableOperatorSource, /ElMessage/)
assert.match(editor, /class="ai-fab"[\s\S]*:aria-label="t\('develop\.workflow\.aiTitle'\)"[\s\S]*:disabled="editorBusy"/)
assert.match(editor, /:aria-label="t\('develop\.workflow\.toggleOperatorPanel'\)"/)
assert.match(editor, /:aria-label="t\('develop\.workflow\.collapseOperatorPanel'\)"/)
assert.match(editor, /:aria-label="t\('develop\.workflow\.collapseParamsPanel'\)"/)
assert.match(editor, /class="open-inspector"[\s\S]*:aria-label="t\('develop\.workflow\.openParamsPanel'\)"/)
assert.doesNotMatch(editor, /createTemporaryWorkflowTask|tempWorkflowPrefix/)

assert.match(paramsPanel, /defineEmits\(\['update', 'update-connection'\]\)/)
assert.match(paramsPanel, /watch\(formData/)
assert.match(paramsPanel, /inputParameters/)
assert.match(paramsPanel, /@change="value => changeInputConnection\(param\.name, value\)"/)
assert.match(paramsPanel, /emit\('update-connection'/)
assert.match(paramsPanel, /`\$\{option\.sourceLabel\} \(\$\{option\.sourceId\}\)`/)
assert.match(editor, /@update-connection="handleInputConnectionUpdate"/)
assert.match(paramsPanel, /:data-param-name="param\.name"/)
assert.match(paramsPanel, /:error="validationMessageFor\(param\.name\)"/)
assert.match(paramsPanel, /resourceValidationMessages\(param\)/)
assert.match(paramsPanel, /defineExpose\(\{ focusParam \}\)/)
assert.doesNotMatch(paramsPanel, /saveParams|develop\.operatorParams\.saveParams/)
assert.match(operatorPalette, /hasDistinctText\(operator\.description, operator\.displayName, operator\.name\)/)
assert.match(operatorPalette, /<el-tooltip effect="light" placement="right" :show-after="300">/)
assert.match(operatorPalette, /<button[\s\S]*class="info-button"[\s\S]*:aria-label="t\('develop\.operatorPalette\.helpLabel', \{ name: operator\.displayName \}\)"[\s\S]*@click\.stop/)
assert.match(operatorPalette, /\.info-button:focus-visible/)
assert.doesNotMatch(operatorItemOpening, /@click=/)
assert.match(operatorItemSource, /<button[\s\S]*class="operator-add-button"[\s\S]*:aria-label="t\('develop\.operatorPalette\.addLabel', \{ name: operator\.displayName \}\)"[\s\S]*@click="handleOperatorClick\(operator\)"/)
assert.match(operatorPalette, /\.operator-add-button:focus-visible/)
assert.equal(zhCnMessages.develop.operatorPalette.helpLabel, '查看“{name}”算子说明')
assert.equal(enMessages.develop.operatorPalette.helpLabel, 'View details for the {name} operator')
assert.equal(zhCnMessages.develop.operatorPalette.addLabel, '添加“{name}”算子')
assert.equal(enMessages.develop.operatorPalette.addLabel, 'Add the {name} operator')
assert.equal(zhCnMessages.develop.operatorParams.resourceDialogTitle, '选择{name}')
assert.equal(enMessages.develop.operatorParams.resourceDialogTitle, 'Select {name}')
assert.equal(zhCnMessages.develop.workflow.confirm, '确定')
assert.equal(enMessages.develop.workflow.confirm, 'Confirm')
assert.equal(zhCnMessages.develop.workflow.savingStatus, '正在保存工作流')
assert.equal(enMessages.develop.workflow.savingStatus, 'Saving workflow')
assert.doesNotMatch(operatorPalette, /operator-code/)
assert.match(operatorPalette, /v-if="!loading && !loadError && filteredCategories\.length === 0"/)

console.log('workflowEditorBehavior tests passed')
