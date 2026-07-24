import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { resolve } from 'node:path'

const editor = await readFile(resolve('src/views/WorkflowEditor.vue'), 'utf8')
const paramsPanel = await readFile(resolve('src/components/workflow/OperatorParamsPanel.vue'), 'utf8')
const operatorPalette = await readFile(resolve('src/components/workflow/OperatorPalette.vue'), 'utf8')

assert.match(editor, /:model-value="workflowEngineId"/)
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
assert.match(editor, /v-model:visible="validationPopoverVisible"/)
assert.match(editor, /const canExecute = computed/)
assert.match(editor, /validationResult\.value\?\.valid === true/)
assert.doesNotMatch(editor, /class="validation-bar"/)
const handleSaveSource = editor.match(/async function handleSave\(\) \{[\s\S]*?\n\}/)?.[0] || ''
const confirmSaveSource = editor.match(/async function confirmSave\(\) \{[\s\S]*?\n\}/)?.[0] || ''
const saveAndSwitchSource = editor.match(/async function saveAndSwitchEngine\(\) \{[\s\S]*?\n\}/)?.[0] || ''
assert.doesNotMatch(handleSaveSource, /validateCurrentWorkflow/)
assert.doesNotMatch(confirmSaveSource, /validateCurrentWorkflow/)
assert.doesNotMatch(saveAndSwitchSource, /validateCurrentWorkflow/)
assert.doesNotMatch(editor, /createTemporaryWorkflowTask|tempWorkflowPrefix/)

assert.match(paramsPanel, /defineEmits\(\['update'\]\)/)
assert.match(paramsPanel, /watch\(formData/)
assert.doesNotMatch(paramsPanel, /saveParams|develop\.operatorParams\.saveParams/)
assert.match(operatorPalette, /hasDistinctText\(operator\.description, operator\.displayName, operator\.name\)/)

console.log('workflowEditorBehavior tests passed')
