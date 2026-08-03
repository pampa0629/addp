import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const wizardStateSource = await readFile(
  new URL('../src/views/TaskWizard/useTaskWizardState.js', import.meta.url),
  'utf8'
)

test('continuous 任务保持自动 Meta 扫描开启', () => {
  assert.match(wizardStateSource, /auto_scan_metadata:\s*true/)
  assert.doesNotMatch(wizardStateSource, /auto_scan_metadata:\s*!isContinuousTask\.value/)
})
