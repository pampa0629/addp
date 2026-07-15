import assert from 'node:assert/strict'

import { resolveWorkflowGenerationResult } from '../src/utils/workflowGenerationResult.mjs'


const clarification = resolveWorkflowGenerationResult({
  status: 'need_clarification',
  clarification_reason: 'data_source_not_found'
})

assert.deepEqual(clarification, {
  workflow: null,
  clarificationKey: 'develop.workflow.dataSourceNotFound'
})

const unverified = resolveWorkflowGenerationResult({
  status: 'need_clarification',
  clarification_reason: 'data_source_unverified'
})

assert.equal(unverified.clarificationKey, 'develop.workflow.dataSourceUnverified')

const successWorkflow = { tasks: [{ id: 'task1' }] }
const success = resolveWorkflowGenerationResult({
  status: 'success',
  workflow: successWorkflow
})

assert.deepEqual(success, {
  workflow: successWorkflow,
  clarificationKey: null
})

console.log('workflowGenerationResult tests passed')
