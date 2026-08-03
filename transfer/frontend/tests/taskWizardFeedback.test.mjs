import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const step4Source = await readFile(
  new URL('../src/views/TaskWizard/Step4Configure.vue', import.meta.url),
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
