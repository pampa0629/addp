import assert from 'node:assert/strict'
import { existsSync, readFileSync, readdirSync } from 'node:fs'
import { resolve, relative } from 'node:path'
import test from 'node:test'

const root = resolve(import.meta.dirname, '../../..')
const source = path => readFileSync(resolve(root, path), 'utf8')
test('process event display has one shared component and no Transfer text parser', () => {
  const roots = readdirSync(root, { withFileTypes: true }).filter(item => item.isDirectory())
    .map(item => resolve(root, item.name, 'frontend/src')).filter(existsSync)
  roots.push(resolve(root, 'common-frontend/basic/src'))
  const owners = roots.flatMap(dir => readdirSync(dir, { recursive: true, withFileTypes: true })
    .filter(item => item.isFile() && item.name.endsWith('.vue'))
    .map(item => resolve(item.parentPath, item.name)))
    .filter(path => /class="execution-events"/.test(readFileSync(path, 'utf8')))
    .map(path => relative(root, path)).sort()
  assert.deepEqual(owners, ['common-frontend/basic/src/components/ExecutionEvents.vue'])
  for (const path of ['monitor/frontend/src/views/ExecutionList.vue', 'transfer/frontend/src/views/ExecutionDetail.vue']) {
    assert.match(source(path), /<ExecutionEvents\s/)
    assert.doesNotMatch(source(path), /executionAPI\.logs|postProcessSummary|filteredLogs|log-viewer/)
  }
  assert.doesNotMatch(source('transfer/frontend/src/api/tasks.js'), /\/logs/)
})
