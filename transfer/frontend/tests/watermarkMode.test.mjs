import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const stateSource = await readFile(new URL('../src/views/TaskWizard/useTaskWizardState.js', import.meta.url), 'utf8')
const step4Source = await readFile(new URL('../src/views/TaskWizard/Step4Configure.vue', import.meta.url), 'utf8')
const step5Source = await readFile(new URL('../src/views/TaskWizard/Step5Review.vue', import.meta.url), 'utf8')
const assistantSource = await readFile(new URL('../src/components/TransferAIAssistant.vue', import.meta.url), 'utf8')
const zhCN = JSON.parse(await readFile(new URL('../src/i18n/zh-cn.json', import.meta.url), 'utf8'))
const en = JSON.parse(await readFile(new URL('../src/i18n/en.json', import.meta.url), 'utf8'))

test('仅新增和新增更新是两个显式可选的 watermark 同步范围', () => {
  assert.match(step4Source, /<el-radio value="insert_only"/)
  assert.match(step4Source, /<el-radio value="incremental"/)
  assert.match(assistantSource, /<el-radio value="insert_only"/)
  assert.equal(zhCN.transfer.taskWizard.insertOnlyIncrementalLoad, '只同步新增')
  assert.equal(en.transfer.taskWizard.insertOnlyIncrementalLoad, 'Synchronize Inserts Only')
})

test('仅新增使用空 tie breaker 并从单字段映射派生目标键', () => {
  assert.match(stateSource, /isInsertOnlyIncremental[\s\S]*?\[watermarkField\.value\]/)
  assert.match(stateSource, /tie_breaker: isInsertOnlyIncremental\.value \? \[\]/)
  assert.match(stateSource, /normalizedFieldNames\(load\.change_detection\?\.tie_breaker\)\.length === 0 \? 'insert_only'/)
  assert.match(step4Source, /v-if="!isInsertOnlyMode"[\s\S]*?tieBreakerLabel/)
  assert.match(step4Source, /watermarkTargetKeys\.value\.join/)
})

test('复核页明确显示仅新增不支持修改和物理删除', () => {
  assert.match(step5Source, /insertOnlyUpdateUnsupported/)
  assert.match(step5Source, /incrementalDeleteUnsupported/)
  assert.match(zhCN.transfer.taskWizard.insertOnlyIncrementalNotice, /已有记录的修改和物理删除都不会同步/)
  assert.match(en.transfer.taskWizard.insertOnlyIncrementalNotice, /Updates to existing records and physical deletes are not synchronized/)
})
