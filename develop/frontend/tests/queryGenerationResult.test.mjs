import assert from 'node:assert/strict'

import {
  confirmedResources,
  defaultResourceCandidatesByRole,
  groupResourceCandidates,
  hasSelectedResourceForEveryRole,
  resourceCandidateKey
} from '@addp/common-frontend/basic/src/utils/resourceCandidateSelection.mjs'
import { resolveQueryGenerationResult } from '../src/utils/queryGenerationResult.mjs'

const candidates = [
  {
    role: 'railway',
    engine_id: 11,
    name: 'railway',
    locator: 'addp://engine/11/path/public/railway?type=table&item_id=60',
    source_engine_type: 'postgresql',
    full_name: 'public.railway',
    query_names: { sql: 'public.railway', federated_sql: 'source_pg.public.railway' },
    schema_coverage: 'complete',
    data_type: 'table',
    geometry_column: 'shape',
    crs: 'EPSG:32650'
  },
  {
    role: 'farmland',
    engine_id: 11,
    name: 'farmland_a',
    locator: 'addp://engine/11/path/public/farmland_a?type=table&item_id=61'
  },
  {
    role: 'farmland',
    engine_id: 11,
    name: 'farmland_b',
    locator: 'addp://engine/11/path/public/farmland_b?type=table&item_id=62'
  }
]

const clarification = resolveQueryGenerationResult({
  status: 'need_clarification',
  clarification_reason: 'data_source_confirmation_required',
  data_source_candidates: candidates
})
assert.equal(clarification.clarificationKey, 'develop.query.dataSourceConfirmationRequired')
assert.deepEqual(clarification.candidates, candidates)

const grouped = groupResourceCandidates(candidates)
assert.equal(grouped.length, 2)
assert.deepEqual(defaultResourceCandidatesByRole(candidates), {
  railway: resourceCandidateKey(candidates[0])
})
assert.equal(hasSelectedResourceForEveryRole(candidates, {
  railway: resourceCandidateKey(candidates[0]),
  farmland: resourceCandidateKey(candidates[2])
}), true)

assert.deepEqual(confirmedResources(candidates, {
  railway: resourceCandidateKey(candidates[0]),
  farmland: resourceCandidateKey(candidates[2])
}), [
  {
    role: 'railway',
    engine_id: 11,
    locator: candidates[0].locator,
    source_engine_type: 'postgresql',
    full_name: 'public.railway',
    query_names: { sql: 'public.railway', federated_sql: 'source_pg.public.railway' },
    schema_coverage: 'complete',
    data_type: 'table',
    geometry_column: 'shape',
    crs: 'EPSG:32650'
  },
  {
    role: 'farmland',
    engine_id: 11,
    locator: candidates[2].locator
  }
])

assert.deepEqual(resolveQueryGenerationResult({
  status: 'success',
  query: 'SELECT 1',
  query_language: 'sql',
  query_parameters: [],
  resources: []
}), {
  query: 'SELECT 1',
  queryLanguage: 'sql',
  resources: [],
  warnings: [],
  queryParameters: [],
  explanation: '',
  clarificationKey: null,
  clarificationReason: null,
  candidates: []
})

assert.deepEqual(resolveQueryGenerationResult({
  status: 'success',
  query: '{"find":"Persons","filter":{"userInfo.nickName":{"$param":"nickname"}}}',
  query_language: 'mql',
  query_parameters: [{ name: 'nickname', type: 'string', default: 'PiPi' }]
}).queryParameters, [
  { name: 'nickname', type: 'string', default: 'PiPi' }
])

assert.throws(() => resolveQueryGenerationResult({ status: 'success', query: '' }))
assert.throws(() => resolveQueryGenerationResult({ status: 'success', query: 'SELECT 1', query_parameters: null }))

console.log('queryGenerationResult tests passed')
