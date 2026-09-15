import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'

const read = path => readFileSync(new URL('../../../' + path, import.meta.url), 'utf8')
test('unsaved navigation has one shared owner across the three editors and Console', () => {
  for (const path of ['orchestrator/frontend/src/views/OrchestrationForm.vue',
    'develop/frontend/src/views/QueryEditor.vue', 'develop/frontend/src/views/WorkflowEditor.vue']) {
    const source = read(path)
    assert.match(source, /useUnsavedChangesGuard\(/, path)
    assert.doesNotMatch(source, /onBeforeRouteLeave|onBeforeRouteUpdate|beforeunload|confirmUnsavedRouteChange/, path)
  }
  assert.match(read('console/frontend/src/views/Portal.vue'), /useConsoleUnsavedChangesGuard\(/)
  const source = read('common-frontend/basic/src/composables/useUnsavedChangesGuard.js')
  assert.match(source, /event.source !== iframe.contentWindow/)
  assert.match(source, /event.origin !== new URL\(iframe.src\).origin/)
})
