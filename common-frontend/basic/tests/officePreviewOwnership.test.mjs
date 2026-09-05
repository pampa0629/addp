import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import test from 'node:test'

const repositoryRoot = resolve(import.meta.dirname, '../../..')
const source = relativePath => readFileSync(resolve(repositoryRoot, relativePath), 'utf8')

test('Manager owns Office file identity and download while the shared renderer owns document interaction', () => {
  const officePreview = source('common-frontend/basic/src/components/previews/OfficePreview.vue')
  const managerPreview = source('manager/frontend/src/components/explorer/PreviewPanel.vue')

  assert.match(managerPreview, /@click="handleDownload"/)
  assert.doesNotMatch(officePreview, /class="office-toolbar"/)
  assert.doesNotMatch(officePreview, /downloadDocument/)
  assert.doesNotMatch(officePreview, /formattedSize/)
  assert.match(officePreview, /download:\s*false/)
  assert.match(officePreview, /value === 'rtf'/)
  assert.match(officePreview, /application\/rtf/)
})
