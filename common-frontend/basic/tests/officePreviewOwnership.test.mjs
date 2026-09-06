import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import test from 'node:test'

const repositoryRoot = resolve(import.meta.dirname, '../../..')
const source = relativePath => readFileSync(resolve(repositoryRoot, relativePath), 'utf8')

test('Manager owns Office file identity and download while the shared renderer owns document interaction', () => {
  const officePreview = source('common-frontend/basic/src/components/previews/OfficePreview.vue')
  const officeRenderer = source('common-frontend/basic/src/lib/office/renderOfficeDocument.js')
  const managerPreview = source('manager/frontend/src/components/explorer/PreviewPanel.vue')

  assert.match(managerPreview, /@click="handleDownload"/)
  assert.match(officePreview, /class="office-toolbar"/)
  assert.doesNotMatch(officePreview, /downloadDocument/)
  assert.doesNotMatch(officePreview, /formattedSize/)
  assert.match(officePreview, /import\('\.\.\/\.\.\/lib\/office\/renderOfficeDocument'\)/)
  assert.doesNotMatch(officePreview, /@open-file-viewer\/core/)
  assert.match(officeRenderer, /from '\.\/legacyWord'/)
  assert.match(officeRenderer, /from '\.\/rtf'/)
  assert.match(officeRenderer, /from 'mammoth'/)
  assert.match(officeRenderer, /from 'dompurify'/)
})

test('Manager does not declare or alias an external Office viewer runtime', () => {
  const packageJson = source('manager/frontend/package.json')
  const viteConfig = source('manager/frontend/vite.config.js')

  assert.doesNotMatch(packageJson, /@open-file-viewer\/core/)
  assert.doesNotMatch(viteConfig, /@open-file-viewer\/core/)
  assert.match(packageJson, /"mammoth":/)
})
