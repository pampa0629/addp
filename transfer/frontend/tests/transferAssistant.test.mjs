import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { inferTransferSyncMode } from '../src/utils/transferCopilot.mjs'

const assistant = readFileSync(new URL('../src/components/TransferAIAssistant.vue', import.meta.url), 'utf8')
const taskList = readFileSync(new URL('../src/views/TaskList.vue', import.meta.url), 'utf8')
const taskWizard = readFileSync(new URL('../src/views/TaskWizard/TaskWizard.vue', import.meta.url), 'utf8')
const zhCN = JSON.parse(readFileSync(new URL('../src/i18n/zh-cn.json', import.meta.url), 'utf8'))

test('传输任务创建助手只挂载在任务列表页', () => {
  assert.match(taskList, /<TransferAIAssistant @task-created="loadPageData"/)
  assert.doesNotMatch(taskWizard, /TransferAIAssistant/)
  assert.equal(zhCN.transfer.taskAssistant.title, '传输任务创建助手')
})

test('唯一源候选也进入显式确认阶段', () => {
  assert.match(assistant, /candidates\.value = sourceEngineIDs\.size/)
  assert.match(assistant, /stage\.value = 'source'/)
  assert.doesNotMatch(assistant, /shouldAutoConfirmSource/)
  assert.match(assistant, /sourceConfirmTitle/)
  assert.match(assistant, /inferSourceEnginesFromPrompt/)
  assert.match(assistant, /source_engine_id: Number\(selectedSource\.value\.engine_id\)/)
})

test('确认数据源后使用 Meta 权威字段而不是 Copilot 候选简化字段', () => {
  assert.match(assistant, /resolveAuthoritativeSourceFields/)
  assert.match(assistant, /getItemFieldsByID\(itemID\)/)
})

test('目标和字段在助手内独立确认后才创建任务', () => {
  assert.match(assistant, /<ResourceTreePicker[\s\S]*targetEngineId/)
  assert.match(assistant, /targetConfirmTitle/)
  assert.match(assistant, /fieldsConfirmTitle/)
  assert.match(assistant, /fieldDefinitionRecommendationAPI\.create/)
  assert.match(assistant, /wizardState\.applyRecommendedDecimalDefinitions/)
  assert.match(assistant, /taskAPI\.create\(task\)/)
})

test('目标数据库位置选择器占满助手表单宽度', () => {
  assert.match(assistant, /<ResourceTreePicker\s+[\s\S]*?v-model="targetParentSelection"\s+class="full-width"/)
})

test('同步方式意图会进入 Transfer 向导状态而不是只进入标题', () => {
  assert.equal(inferTransferSyncMode('从 pg 到 mysql，实时同步 farmland'), 'cdc')
  assert.equal(inferTransferSyncMode('从 pg 到 mysql，增量同步 farmland'), 'incremental')
  assert.equal(inferTransferSyncMode('从 pg 到 mysql，全量同步 farmland'), 'snapshot')
  assert.equal(inferTransferSyncMode('从 pg 到 mysql，同步 farmland'), 'snapshot')
  assert.equal(inferTransferSyncMode('持续消费 orders', {
    sourceEngineType: 'kafka',
    sourceLocator: 'addp://engine/8/path/orders?type=topic'
  }), 'kafka')
  assert.match(assistant, /capabilitiesAPI\.get\(\)/)
  assert.match(assistant, /wizardState\.setLoadMode/)
  assert.match(assistant, /watermarkField/)
  assert.match(assistant, /continuousKeyFields/)
  assert.match(assistant, /syncModeSummary/)
  assert.ok(assistant.indexOf('wizardState.autoGenerateFieldMappings()') < assistant.lastIndexOf('applySyncMode()'))
})

test('助手界面不展示内部资源定位符', () => {
  assert.doesNotMatch(assistant, /\{\{\s*candidate\.locator\s*\}\}/)
  assert.doesNotMatch(assistant, /addp:\/\//)
  assert.doesNotMatch(assistant, /JSON\.stringify\(wizardState\.taskConfig\.value/)
  assert.match(assistant, /fieldsConfirmed/)
})

test('推理场景未配置时给出配置管理提示', () => {
  assert.match(assistant, /transfer_inference_scenario_not_configured/)
  assert.match(assistant, /inferenceNotConfigured/)
  assert.match(zhCN.transfer.taskAssistant.inferenceNotConfigured, /配置管理/)
})
