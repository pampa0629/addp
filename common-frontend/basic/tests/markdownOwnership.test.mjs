import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import test from 'node:test'

const read = path => readFileSync(resolve(import.meta.dirname, '../../..', path), 'utf8')
test('Agent and file preview delegate Markdown parsing and sanitization to one shared body', () => {
  for (const path of ['agent/frontend/src/components/MessagePartsRenderer.vue', 'common-frontend/basic/src/components/previews/MarkdownPreview.vue']) {
    const source = read(path)
    assert.match(source, /<MarkdownContent\b/)
    assert.doesNotMatch(source, /from ['"](?:marked|dompurify)['"]|v-html|navigator\.clipboard|renderer\.code/)
  }
  const body = read('common-frontend/basic/src/components/MarkdownContent.vue')
  assert.match(body, /DOMPurify\.sanitize/)
  assert.match(body, /marked\.parse/)
  assert.doesNotMatch(body, /from ['"]mermaid['"]|from ['"].*agent\//)
  assert.doesNotMatch(read('common-frontend/basic/src/index.js'), /export.*MarkdownContent/)
})
test('Agent login composes the shared MFA and context flow', () => {
  const source = read('agent/frontend/src/views/Login.vue')
  assert.match(source, /<AuthLoginFlow\b/)
  assert.doesNotMatch(source, /<el-form|<el-input/)
})
