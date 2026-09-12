import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const wizardStateSource = await readFile(
  new URL('../src/views/TaskWizard/useTaskWizardState.js', import.meta.url),
  'utf8'
)

test('持久化默认目标保持自动 Meta 扫描', () => {
  assert.match(wizardStateSource, /auto_scan_metadata:\s*true/)
  assert.doesNotMatch(wizardStateSource, /auto_scan_metadata:\s*!isContinuousTask\.value/)
})
