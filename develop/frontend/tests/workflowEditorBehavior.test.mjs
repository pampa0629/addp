import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { resolve } from 'node:path'

const editor = await readFile(resolve('src/views/WorkflowEditor.vue'), 'utf8')
const paramsPanel = await readFile(resolve('src/components/workflow/OperatorParamsPanel.vue'), 'utf8')
const operatorPalette = await readFile(resolve('src/components/workflow/OperatorPalette.vue'), 'utf8')
const executeDialogSource = editor.slice(
  editor.indexOf('<el-dialog\n      v-model="executeDialogVisible"'),
  editor.indexOf('<el-dialog v-model="jsonDialogVisible"')
)
const saveDialogSource = editor.slice(
  editor.indexOf('<el-dialog\n      v-model="saveDialogVisible"'),
  editor.indexOf('<el-dialog\n      v-model="executeDialogVisible"')
)

assert.match(editor, /:model-value="workflowEngineId"/)
assert.match(editor, /class="editor-content" :inert="editorBusy" :aria-busy="editorBusy"/)
assert.match(editor, /:loading="executionButtonLoading"/)
assert.match(executeDialogSource, /:close-on-click-modal="!executing"/)
assert.match(executeDialogSource, /:close-on-press-escape="!executing"/)
assert.match(executeDialogSource, /:show-close="!executing"/)
assert.match(executeDialogSource, /<el-form label-width="100px" :disabled="executing">/)
assert.match(executeDialogSource, /<el-button :disabled="executing" @click="executeDialogVisible = false">/)
assert.match(executeDialogSource, /:loading="executing" :disabled="executing" @click="confirmExecute"/)
assert.match(saveDialogSource, /:close-on-click-modal="!saving"/)
assert.match(saveDialogSource, /:close-on-press-escape="!saving"/)
assert.match(saveDialogSource, /:show-close="!saving"/)
assert.match(saveDialogSource, /<el-form :model="saveForm" label-width="100px" :disabled="saving">/)
assert.match(saveDialogSource, /<el-button :disabled="saving" @click="cancelSaveDialog">/)
assert.match(saveDialogSource, /:loading="saving" :disabled="saving" @click="confirmSave"/)
assert.match(editor, /@change="requestEngineChange"/)
assert.match(editor, /v-model="engineSwitchDialogVisible"/)
assert.match(editor, /develop\.workflow\.saveAndClear/)
assert.match(editor, /develop\.workflow\.clearAndSwitch/)
assert.match(editor, /pendingAction\.value = 'switchEngine'/)
assert.match(editor, /await saveCurrentTask\(\)/)
assert.match(editor, /canvasRef\.value\?\.clearGraph\(\)/)
assert.match(editor, /currentTaskId\.value = null/)
assert.match(editor, /await router\.replace\(\{ query \}\)/)
assert.doesNotMatch(editor, /:disabled="engineLocked"/)
assert.doesNotMatch(editor, /const engineLocked = computed/)
assert.match(editor, /await updateWorkflowTask\(currentTaskId\.value/)
assert.match(editor, /if \(workflow\.tasks\.length === 0\) editorLayout\.value = \{\}/)
assert.match(editor, /if \(workflowData\.value\.tasks\.length === 0\) return/)
assert.match(editor, /await executeWorkflowTask\(currentTaskId\.value, inputs\)/)
assert.match(editor, /validateWorkflowDefinition\(/)
assert.match(editor, /getClientValidationIssues\(\)/)
assert.match(editor, /class="header-validation"/)
assert.match(editor, /v-if="hasValidationStatus"/)
assert.match(editor, /v-model:visible="validationPopoverVisible"/)
assert.match(editor, /v-for="group in validationIssueGroups"/)
assert.match(editor, /class="validation-group-title"/)
assert.match(editor, /class="validation-item-param"/)
assert.match(editor, /ref="paramsPanelRef"/)
assert.match(editor, /:validation-issues="selectedNodeValidationIssues"/)
assert.match(editor, /paramsPanelRef\.value\?\.focusParam\(validationIssueParamName\(issue\)\)/)
assert.match(editor, /const canExecute = computed/)
const canExecuteSource = editor.match(/const canExecute = computed\([\s\S]*?\n\)\)/)?.[0] || ''
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
assert.match(confirmExecuteSource, /if \(executing\.value\) return/)
assert.match(confirmExecuteSource, /await executeWorkflowTask\(currentTaskId\.value, inputs\)/)
assert.match(confirmSaveSource, /setTaskRouteQuery\(currentTaskId\.value\)/)
assert.match(editor, /async function setTaskRouteQuery\(taskId\)/)
assert.match(editor, /function cancelSaveDialog\(\) \{\s+if \(saving\.value\) return/)
assert.match(editor, /async function handleFileChange\(event\) \{[\s\S]*importing\.value = true[\s\S]*importing\.value = false/)
assert.match(editor, /async function generateWorkflow\(\) \{\s+if \(editorBusy\.value\) return/)
assert.match(editor, /class="ai-fab"[\s\S]*:aria-label="t\('develop\.workflow\.aiTitle'\)"[\s\S]*:disabled="editorBusy"/)
assert.doesNotMatch(editor, /createTemporaryWorkflowTask|tempWorkflowPrefix/)

assert.match(paramsPanel, /defineEmits\(\['update'\]\)/)
assert.match(paramsPanel, /watch\(formData/)
assert.match(paramsPanel, /:data-param-name="param\.name"/)
assert.match(paramsPanel, /:error="validationMessageFor\(param\.name\)"/)
assert.match(paramsPanel, /resourceValidationMessages\(param\)/)
assert.match(paramsPanel, /defineExpose\(\{ focusParam \}\)/)
assert.doesNotMatch(paramsPanel, /saveParams|develop\.operatorParams\.saveParams/)
assert.match(operatorPalette, /hasDistinctText\(operator\.description, operator\.displayName, operator\.name\)/)

console.log('workflowEditorBehavior tests passed')
