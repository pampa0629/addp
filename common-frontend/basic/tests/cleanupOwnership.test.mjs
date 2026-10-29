import assert from 'node:assert/strict'
import { existsSync, readFileSync, readdirSync } from 'node:fs'
import { resolve, relative } from 'node:path'
import test from 'node:test'
const root = resolve(import.meta.dirname, '../../..')
const source = path => readFileSync(resolve(root, path), 'utf8')
test('Cleanup admission and professional rendering have exactly one shared owner', () => {
  const roots = readdirSync(root, { withFileTypes: true }).filter(item => item.isDirectory()).map(item => resolve(root, item.name, 'frontend/src')).filter(existsSync)
  roots.push(resolve(root, 'common-frontend/basic/src'))
  const files = roots.flatMap(dir => readdirSync(dir, { recursive: true, withFileTypes: true }).filter(item => item.isFile() && item.name.endsWith('.vue')).map(item => resolve(item.parentPath, item.name)))
  for (const [marker, component] of [['cleanup-admission', 'CleanupAdmission'], ['cleanup-module-results', 'CleanupModuleResults']]) {
    const owners = files.filter(path => readFileSync(path, 'utf8').includes(`class="${marker}"`)).map(path => relative(root, path)).sort()
    assert.deepEqual(owners, [`common-frontend/basic/src/components/${component}.vue`])
  }
  assert.match(source('monitor/frontend/src/views/ExecutionList.vue'), /<CleanupAdmission\s/)
  assert.match(source('system/frontend/src/views/CleanupManager.vue'), /<CleanupModuleResults\s/)
  assert.doesNotMatch(source('system/frontend/src/views/CleanupManager.vue'), /JSON\.stringify\(taskDetail|latestResult\.results/)
  for (const locale of ['zh-cn', 'en']) {
    const messages = JSON.parse(source(`common-frontend/basic/src/i18n/${locale}.json`)).common.cleanupObservation
    for (const key of ['closed_unclaimed', 'closed_rejected', 'rejected_unstarted']) assert.ok(messages.states[key] && messages.explanations[key])
    for (const key of ['pending', 'running', 'success', 'failed', 'timeout', 'cancelled']) assert.ok(messages.status[key])
  }
})
