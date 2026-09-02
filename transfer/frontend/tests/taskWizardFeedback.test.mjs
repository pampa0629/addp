import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const step4Source = await readFile(
  new URL('../src/views/TaskWizard/Step4Configure.vue', import.meta.url),
  'utf8'
)
const step3Source = await readFile(
  new URL('../src/views/TaskWizard/Step3FieldMapping.vue', import.meta.url),
  'utf8'
)
const zhCN = JSON.parse(await readFile(new URL('../src/i18n/zh-cn.json', import.meta.url), 'utf8'))
const en = JSON.parse(await readFile(new URL('../src/i18n/en.json', import.meta.url), 'utf8'))

test('数据库 CDC 不可用时通过问号按钮展示原因', () => {
  assert.match(
    step4Source,
    /<el-popover[\s\S]*?v-if="databaseCDCUnavailableReasons\.length"[\s\S]*?trigger="click"/
  )
  assert.match(step4Source, /:icon="QuestionFilled"/)
  assert.doesNotMatch(step4Source, /class="cdc-unavailable-alert"/)
  assert.equal(zhCN.transfer.taskWizard.databaseCDCUnavailableTitle, '持续同步不可用')
  assert.equal(en.transfer.taskWizard.databaseCDCUnavailableTitle, 'Continuous sync unavailable')
  assert.equal(zhCN.transfer.taskWizard.databaseCDCUnavailableHelp, '查看持续同步不可用原因')
  assert.equal(en.transfer.taskWizard.databaseCDCUnavailableHelp, 'View why continuous sync is unavailable')
})

test('字段映射分别处理源字段变化和已有目标字段选择', () => {
  assert.match(
    step3Source,
    /sourceFieldCol[\s\S]*?v-model="row\.source_field"[\s\S]*?@change="handleMappingChange\(\$index\)"/
  )
  assert.match(
    step3Source,
    /targetFieldCol[\s\S]*?v-model="row\.target_field"[\s\S]*?@change="handleTargetFieldChange\(\$index\)"/
  )
})

test('结构化 MongoDB 查询固定源字段并只开放目标映射', () => {
  assert.match(step3Source, /const isStructuredMongoQuery = computed/)
  assert.match(step3Source, /v-if="isStructuredMongoQuery" class="structured-source-field"/)
  assert.match(step3Source, /v-if="!isStructuredMongoQuery" class="mapping-controls"/)
  assert.match(step3Source, /v-if="!isStructuredMongoQuery" :label="t\('transfer\.taskWizard\.actionsCol'\)"/)
  assert.match(zhCN.transfer.taskWizard.structuredMongoMappingDesc, /未选择的 MongoDB 字段不会出现在映射中/)
  assert.match(en.transfer.taskWizard.structuredMongoMappingDesc, /unselected MongoDB fields do not appear/)
})

test('Kafka 字段建议必须经确认后合并', () => {
  assert.match(step3Source, /v-if="wizardState\.isKafkaContinuousTask\.value"[\s\S]*?@click="loadTopicFieldRecommendations"/)
  assert.match(step3Source, /getManagerPreview\(props\.wizardState\.sourceLocator\.value, 50\)/)
  assert.match(step3Source, /topicSampleDialogVisible[\s\S]*?confirmTopicFieldRecommendations/)
  assert.match(step3Source, /applyTopicFieldRecommendations\(topicRecommendations\.value\)/)
  assert.match(zhCN.transfer.taskWizard.topicSampleNotice, /样本不是 Topic Schema/)
  assert.match(en.transfer.taskWizard.topicSampleNotice, /Samples are not a Topic schema/)
})

test('decimal 精度和小数位在各自输入框提供说明和错误', () => {
  assert.match(step3Source, /precisionHelp[\s\S]*?decimalPrecisionPlaceholder/)
  assert.match(step3Source, /precisionIssue\(\$index\)[\s\S]*?decimalIssueMessage/)
  assert.match(step3Source, /scaleHelp[\s\S]*?decimalScalePlaceholder/)
  assert.match(step3Source, /scaleIssue\(\$index\)[\s\S]*?decimalIssueMessage/)
  assert.match(zhCN.transfer.taskWizard.precisionHelp, /DECIMAL\(20,10\)/)
  assert.match(en.transfer.taskWizard.precisionHelp, /DECIMAL\(20,10\)/)
})

test('新建 MySQL 目标表可以分析源数据并应用 decimal 推荐', () => {
  assert.match(step3Source, /canRecommendDecimalDefinitions[\s\S]*?recommendDecimalDefinitions/)
  assert.match(step3Source, /fieldDefinitionRecommendationAPI\.create/)
  assert.match(zhCN.transfer.taskWizard.decimalRecommendationApplied, /扫描/)
  assert.match(en.transfer.taskWizard.decimalRecommendationApplied, /Scanned/)
})
