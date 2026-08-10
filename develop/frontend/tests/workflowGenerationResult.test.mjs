import assert from 'node:assert/strict'

import {
  resolveWorkflowGenerationResult
} from '../src/utils/workflowGenerationResult.mjs'
import {
  confirmedResources,
  defaultResourceCandidatesByRole,
  groupResourceCandidates,
  hasSelectedResourceForEveryRole,
  resourceCandidateKey
} from '@addp/common-frontend/basic/src/utils/resourceCandidateSelection.mjs'


const clarification = resolveWorkflowGenerationResult({
  status: 'need_clarification',
  clarification_reason: 'data_source_not_found'
})

assert.deepEqual(clarification, {
  workflow: null,
  clarificationKey: 'develop.workflow.dataSourceNotFound',
  clarificationReason: 'data_source_not_found',
  candidates: []
})

const unverified = resolveWorkflowGenerationResult({
  status: 'need_clarification',
  clarification_reason: 'data_source_unverified'
})

assert.equal(unverified.clarificationKey, 'develop.workflow.dataSourceUnverified')

const resourceFactsRequired = resolveWorkflowGenerationResult({
  status: 'need_clarification',
  clarification_reason: 'resource_facts_required'
})

assert.equal(resourceFactsRequired.clarificationKey, 'develop.workflow.resourceFactsRequired')

const resourceCandidates = [{
  role: 'input_1',
  name: 'railway',
  locator: 'addp://engine/8/path/public/railway?type=table&item_id=60'
}]
const ambiguous = resolveWorkflowGenerationResult({
  status: 'need_clarification',
  clarification_reason: 'data_source_ambiguous',
  data_source_candidates: resourceCandidates
})

assert.equal(ambiguous.clarificationReason, 'data_source_ambiguous')
assert.deepEqual(ambiguous.candidates, resourceCandidates)

const confirmationRequired = resolveWorkflowGenerationResult({
  status: 'need_clarification',
  clarification_reason: 'data_source_confirmation_required',
  data_source_candidates: resourceCandidates
})

assert.equal(
  confirmationRequired.clarificationKey,
  'develop.workflow.dataSourceConfirmationRequired'
)

assert.deepEqual(
  confirmedResources([
    {
      role: 'input_1',
      name: 'railway',
      locator: 'addp://engine/8/path/public/railway?type=table&item_id=60',
      data_type: 'table',
      geometry_column: 'shape',
      geometry_type: 'LineString',
      crs: 'EPSG:32650',
      fields: [{ name: 'shape', type: 'geometry' }]
    },
    {
      role: 'input_2',
      name: 'farmland',
      locator: 'addp://engine/8/path/public/farmland?type=table&item_id=61',
      data_type: 'table'
    }
  ], {
    input_1: resourceCandidateKey({
      role: 'input_1',
      locator: 'addp://engine/8/path/public/railway?type=table&item_id=60'
    }),
    input_2: resourceCandidateKey({
      role: 'input_2',
      locator: 'addp://engine/8/path/public/farmland?type=table&item_id=61'
    })
  }),
  [
    {
      role: 'input_1',
      locator: 'addp://engine/8/path/public/railway?type=table&item_id=60',
      data_type: 'table',
      geometry_column: 'shape',
      geometry_type: 'LineString',
      crs: 'EPSG:32650',
      fields: [{ name: 'shape', type: 'geometry' }]
    },
    {
      role: 'input_2',
      locator: 'addp://engine/8/path/public/farmland?type=table&item_id=61',
      data_type: 'table'
    }
  ]
)

const groupedCandidates = [
  { role: 'railway', locator: 'railway-1' },
  { role: 'railway', locator: 'railway-2' },
  { role: 'farmland', locator: 'farmland-1' }
]

assert.deepEqual(
  defaultResourceCandidatesByRole(groupedCandidates),
  { farmland: resourceCandidateKey({ role: 'farmland', locator: 'farmland-1' }) }
)
assert.equal(
  hasSelectedResourceForEveryRole(groupedCandidates, {
    farmland: resourceCandidateKey({ role: 'farmland', locator: 'farmland-1' })
  }),
  false
)
assert.equal(
  hasSelectedResourceForEveryRole(groupedCandidates, {
    railway: resourceCandidateKey({ role: 'railway', locator: 'railway-2' }),
    farmland: resourceCandidateKey({ role: 'farmland', locator: 'farmland-1' })
  }),
  true
)
assert.deepEqual(groupResourceCandidates(groupedCandidates), [
  { role: 'railway', candidates: groupedCandidates.slice(0, 2) },
  { role: 'farmland', candidates: groupedCandidates.slice(2) }
])
assert.deepEqual(
  confirmedResources(groupedCandidates, {
    railway: resourceCandidateKey({ role: 'railway', locator: 'railway-1' }),
    farmland: resourceCandidateKey({ role: 'farmland', locator: 'farmland-1' })
  }),
  [
    { role: 'railway', locator: 'railway-1' },
    { role: 'farmland', locator: 'farmland-1' }
  ]
)

const successWorkflow = { tasks: [{ id: 'task1' }] }
const success = resolveWorkflowGenerationResult({
  status: 'success',
  workflow: successWorkflow
})

assert.deepEqual(success, {
  workflow: successWorkflow,
  clarificationKey: null,
  clarificationReason: null,
  candidates: []
})

console.log('workflowGenerationResult tests passed')
