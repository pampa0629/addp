import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import test from 'node:test'

const repositoryRoot = resolve(import.meta.dirname, '../../..')
const source = relativePath => readFileSync(resolve(repositoryRoot, relativePath), 'utf8')

test('PPTX preview reloads only when the ResourceLocator identity changes', () => {
  const pptxPreview = source('common-frontend/basic/src/components/previews/PptxPreview.vue')

  assert.match(pptxPreview, /watch\(\(\) => props\.source\?\.locator, \(\) => resolvePreview\(\), \{ immediate: true \}\)/)
  assert.doesNotMatch(pptxPreview, /watch\(\(\) => props\.source, [\s\S]*deep: true/)
  assert.match(pptxPreview, /\/api\/v1\/manager\/quick-view\/capability\?locator=/)
  assert.match(pptxPreview, /action:\s*'generate_pptx_pdf'/)
  assert.match(pptxPreview, /\['failed', 'stale'\]\.includes\(artifact\.status\) && !retry/)
  assert.doesNotMatch(pptxPreview, /\/api\/v1\/manager\/pptx_pdf\/preview/)
})

test('PDF preview compares stable watch sources instead of a newly allocated array', () => {
  const pdfPreview = source('common-frontend/basic/src/components/previews/PdfPreview.vue')

  assert.match(pdfPreview, /watch\(\s*\[\s*\(\) => props\.data\?\.object\?\.path/)
  assert.doesNotMatch(pdfPreview, /watch\(\s*\(\) => \[/)
})
