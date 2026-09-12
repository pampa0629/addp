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
const step1Source = await readFile(
  new URL('../src/views/TaskWizard/Step1SelectSource.vue', import.meta.url),
  'utf8'
)
const step2Source = await readFile(
  new URL('../src/views/TaskWizard/Step2SelectTarget.vue', import.meta.url),
  'utf8'
)
const relationalSQLBuilderSource = await readFile(
  new URL('../src/views/TaskWizard/RelationalSQLQueryBuilder.vue', import.meta.url),
  'utf8'
)
const mongoStructureBuilderSource = await readFile(
  new URL('../src/views/TaskWizard/MongoStructureQueryBuilder.vue', import.meta.url),
  'utf8'
)
const zhCN = JSON.parse(await readFile(new URL('../src/i18n/zh-cn.json', import.meta.url), 'utf8'))
const en = JSON.parse(await readFile(new URL('../src/i18n/en.json', import.meta.url), 'utf8'))

test('查询语言由源引擎能力决定且关系型源使用轻量 SQL 构造器', () => {
  assert.match(step1Source, /queryLanguageOptions = computed\(\(\) => selectedSourceQueryCapability/)
  assert.match(step1Source, /v-for="language in queryLanguageOptions"/)
  assert.match(step1Source, /<RelationalSQLQueryBuilder/)
  assert.match(step1Source, /:parameter-types="selectedSourceQueryCapability\?\.parameterTypes"/)
  assert.match(step1Source, /data-testid="task-query-language-fixed"/)
  assert.match(step1Source, /data-testid="task-query-language-select"/)
  assert.doesNotMatch(step1Source, /<el-option label="MQL" value="mql"/)
  assert.doesNotMatch(step1Source, /<el-option label="SQL" value="sql"/)
})

test('关系型 SQL 构造器不提供高级 SQL 编辑路径', () => {
  assert.match(relationalSQLBuilderSource, /v-if="unsupportedReason"/)
  assert.match(relationalSQLBuilderSource, /:model-value="modelValue"[\s\S]*?readonly/)
  assert.doesNotMatch(relationalSQLBuilderSource, /el-radio-group/)
  assert.doesNotMatch(relationalSQLBuilderSource, /advancedMode|emitAdvancedQuery|parseAdvancedParameters/)
  assert.match(relationalSQLBuilderSource, /fieldKind\(filter\.field\) === 'exact-number'/)
  assert.equal(zhCN.transfer.taskWizard.sqlBuilder.readOnlySql, '只读 SQL 和参数')
  assert.equal(en.transfer.taskWizard.sqlBuilder.readOnlySql, 'Read-only SQL and parameters')
})

test('MongoDB MQL 构造器不提供高级编辑路径且失配查询只读', () => {
  assert.match(mongoStructureBuilderSource, /v-if="unsupportedReason"/)
  assert.match(mongoStructureBuilderSource, /:model-value="modelValue" type="textarea" :rows="10" readonly/)
  assert.match(mongoStructureBuilderSource, /parseMongoStructureQuery\(statement, \{[^]*?sourceFields: props\.sourceFields/)
  assert.doesNotMatch(mongoStructureBuilderSource, /advancedMode|changeMode|emitRawStatement/)
  assert.match(step1Source, /v-if="!isRelationalSqlSource && !isMongoMqlSource"/)
  assert.equal(zhCN.transfer.taskWizard.mongoBuilder.readOnlyMql, '只读 MQL')
  assert.equal(en.transfer.taskWizard.mongoBuilder.readOnlyMql, 'Read-only MQL')
})

test('目标覆盖位于默认目标之后且不依赖源查询开关', () => {
  const targetTableIndex = step2Source.indexOf('targetTableLabel')
  const overrideIndex = step2Source.indexOf('orchestrationExecutionLabel')
  assert.ok(targetTableIndex >= 0 && overrideIndex > targetTableIndex)
  assert.match(step2Source, /targetOverrideEligible/)
  assert.doesNotMatch(step2Source, /sourceQueryEnabled[\s\S]*targetOverride/)
  assert.doesNotMatch(step2Source, /targetBinding|runtimeTarget/)
  assert.match(zhCN.transfer.taskWizard.targetOverrideHint, /默认目标/)
  assert.match(en.transfer.taskWizard.targetOverrideHint, /default target/)
})

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

test('声明 decimal 写入限制的目标表可以分析源数据并应用推荐', () => {
  assert.match(step3Source, /canRecommendDecimalDefinitions[\s\S]*?recommendDecimalDefinitions/)
  assert.match(step3Source, /fieldDefinitionRecommendationAPI\.create/)
  assert.match(step3Source, /target_engine_id: props\.wizardState\.targetEngineID\.value/)
  assert.match(zhCN.transfer.taskWizard.decimalRecommendationApplied, /扫描/)
  assert.match(en.transfer.taskWizard.decimalRecommendationApplied, /Scanned/)
})
