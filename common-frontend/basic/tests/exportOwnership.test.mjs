import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const commonIndex = readFileSync(new URL('../src/index.js', import.meta.url), 'utf8')
const managerPreview = readFileSync(
  new URL('../../../manager/frontend/src/components/explorer/PreviewPanel.vue', import.meta.url),
  'utf8'
)
const developEditor = readFileSync(
  new URL('../../../develop/frontend/src/views/QueryEditor.vue', import.meta.url),
  'utf8'
)

test('export dialog and session workflow have one common-frontend owner', () => {
  assert.match(commonIndex, /ExportDialog/)
  assert.match(commonIndex, /exportSession/)
  assert.match(managerPreview, /ExportDialog/)
  assert.match(managerPreview, /waitForExportSession/)
  assert.match(developEditor, /ExportDialog/)
  assert.match(developEditor, /waitForExportSession/)
  assert.doesNotMatch(managerPreview, /const waitForExportReady/)
  assert.doesNotMatch(developEditor, /target_parent_locator/)
})
