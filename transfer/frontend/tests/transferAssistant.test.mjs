import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

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
  assert.match(assistant, /candidates\.value = result\.data_source_candidates/)
  assert.match(assistant, /stage\.value = 'source'/)
  assert.doesNotMatch(assistant, /shouldAutoConfirmSource/)
  assert.match(assistant, /sourceConfirmTitle/)
})

test('目标和字段在助手内独立确认后才创建任务', () => {
  assert.match(assistant, /<ResourceTreePicker[\s\S]*targetEngineId/)
  assert.match(assistant, /targetConfirmTitle/)
  assert.match(assistant, /fieldsConfirmTitle/)
  assert.match(assistant, /fieldDefinitionRecommendationAPI\.create/)
  assert.match(assistant, /wizardState\.applyRecommendedDecimalDefinitions/)
  assert.match(assistant, /taskAPI\.create\(task\)/)
})

test('助手界面不展示内部资源定位符', () => {
  assert.doesNotMatch(assistant, /\{\{\s*candidate\.locator\s*\}\}/)
  assert.doesNotMatch(assistant, /addp:\/\//)
})
