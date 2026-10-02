import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { isStepObservationStopped } from '../src/utils/executionStepObservation.mjs'

test('terminal parents preserve unfinished steps as historical observations', () => {
  for (const parent of ['failed', 'timeout', 'cancelled', 'success']) {
    assert.equal(isStepObservationStopped(parent, 'running'), true)
    assert.equal(isStepObservationStopped(parent, 'pending'), true)
    assert.equal(isStepObservationStopped(parent, 'success'), false)
    assert.equal(isStepObservationStopped(parent, 'failed'), false)
    assert.equal(isStepObservationStopped(parent, undefined), false)
  }
})

test('active or unknown parents do not invent a stopped observation', () => {
  for (const parent of ['pending', 'running', undefined, 'unknown']) {
    assert.equal(isStepObservationStopped(parent, 'running'), false)
    assert.equal(isStepObservationStopped(parent, 'pending'), false)
  }
})

test('Monitor and Orchestrator use the same observation decision and bilingual labels', () => {
  const source = path => readFileSync(new URL(path, import.meta.url), 'utf8')
  assert.match(source('../src/index.js'), /export \* from '\.\/utils\/executionStepObservation\.mjs'/)
  assert.match(source('../src/components/ExecutionSteps.vue'), /isStepObservationStopped/)
  assert.match(source('../../../monitor/frontend/src/views/ExecutionList.vue'), /:execution-status="currentExecution.status"/)
  assert.match(source('../../../orchestrator/frontend/src/utils/executionPresentation.js'), /isStepObservationStopped/)
  for (const locale of ['zh-cn', 'en']) {
    const messages = JSON.parse(source(`../src/i18n/${locale}.json`)).common.executionSteps
    assert.match(messages.lastRecorded, /\{status\}/)
    assert.ok(messages.observationStopped)
  }
})
