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
  query_language: 'mql',
  resources: [],
  clarifications: [{
    key: 'query.resources',
    category: 'resource_selection',
    prompt: '请选择查询资源',
    control: 'resource_choice',
    required: true,
    options: [],
    resource_candidates: candidates
  }]
})
assert.equal(clarification.queryLanguage, 'mql')
assert.deepEqual(clarification.clarifications[0].resourceCandidates, candidates)

const semanticClarification = resolveQueryGenerationResult({
  status: 'need_clarification',
  query_language: 'mql',
  resources: [{ role: '人员', locator: 'persons' }],
  clarifications: [{
    key: 'metric.definition',
    category: 'calculation_rule',
    prompt: '请选择计算规则',
    control: 'single_choice',
    required: true,
    options: [
      { value: 'count', label: '数量' },
      { value: 'ratio', label: '比例', description: '计算比例' }
    ],
    resource_candidates: []
  }]
})
assert.deepEqual(semanticClarification.clarifications[0], {
  key: 'metric.definition',
  category: 'calculation_rule',
  prompt: '请选择计算规则',
  control: 'single_choice',
  required: true,
  options: [
    { value: 'count', label: '数量', description: '' },
    { value: 'ratio', label: '比例', description: '计算比例' }
  ],
  resourceCandidates: []
})

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
  clarifications: []
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
assert.throws(() => resolveQueryGenerationResult({ status: 'need_clarification', clarifications: [] }))

console.log('queryGenerationResult tests passed')
