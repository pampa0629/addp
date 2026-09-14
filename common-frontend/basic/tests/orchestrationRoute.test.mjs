import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { buildOrchestrationListRoute, resolveOrchestrationTaskFilter } from '../src/utils/orchestrationRoute.js'

test('builds and restores the complete task reference without ambiguous aliases', () => {
  const route = buildOrchestrationListRoute({ module: 'quality', task_type: 'data_validation', task_id: 17 })
  assert.equal(route, '/orchestrator/orchestrations?module=quality&task_type=data_validation&task_id=17')
  assert.deepEqual(resolveOrchestrationTaskFilter(Object.fromEntries(new URL(route, 'https://example.test').searchParams)), {
    module: 'quality', task_type: 'data_validation', task_id: '17'
  })
  assert.equal(buildOrchestrationListRoute(), '/orchestrator/orchestrations')
})

test('rejects partial, repeated, invalid and unsafe task identities', () => {
  for (const task of [
    { module: 'quality' },
    ...['0', '-1', '01', '1.5', '1e2', '9007199254740992', ['1']].map(task_id => ({ module: 'quality', task_type: 'publish', task_id })),
    { module: ['quality'], task_type: 'publish', task_id: '1' }
  ]) assert.throws(() => buildOrchestrationListRoute(task), TypeError)
})

test('related workflow selection checks the full identity, not numeric ID alone', async () => {
  const { matchesOrchestrationTask } = await import('../src/utils/orchestrationRoute.js')
  const task = { module: 'quality', task_type: 'data_validation', task_id: 1 }
  assert.equal(matchesOrchestrationTask({ steps: [{ provider: 'quality', task_type: 'data_validation', task_id: 1 }] }, task), true)
  assert.equal(matchesOrchestrationTask({ steps: [{ provider: 'quality', task_type: 'check', task_id: 1 }] }, task), false)
  assert.equal(matchesOrchestrationTask({ steps: [{ provider: 'develop', task_type: 'data_validation', task_id: 1 }] }, task), false)
})

test('Orchestrator and related workflows have one execution confirmation owner', () => {
  for (const path of ['../../../orchestrator/frontend/src/views/OrchestrationList.vue', '../src/components/RelatedOrchestrationsDialog.vue']) {
    const source = readFileSync(new URL(path, import.meta.url), 'utf8')
    assert.match(source, /OrchestrationExecuteButton/)
    assert.doesNotMatch(source, /async function handleExecute/)
  }
})
