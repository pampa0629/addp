import test from 'node:test'
import assert from 'node:assert/strict'

import {
  discoverTransferSources,
  verifyTransferSource,
  groupResourceCandidates,
  inferTargetEngineForClarification,
  inferTargetEngineFromPrompt,
  inferSourceEngineFromPrompt,
  inferSourceEnginesFromPrompt,
  needsTargetConfiguration,
  resolveAuthoritativeSourceFields,
  resourceCandidateKey,
  resourceFact
} from '../src/utils/transferCopilot.mjs'

const sourceLocator = 'addp://engine/8/path/public/roads?type=table&item_id=60'

function resourceOwners(overrides = {}) {
  return {
    search: async () => ({ results: [{ name: 'roads', engine_id: 8, locator: sourceLocator }] }),
    ancestors: async () => ({ target_locator: sourceLocator, ancestors: [{ label: 'public' }] }),
    facts: async () => ({
      locator: sourceLocator, engine_id: 8, data_type: 'table', schema_coverage: 'complete',
      fields: [{ name: 'road_id', type: 'bigint' }], connection_info: { password: 'must-not-forward' }
    }),
    ...overrides
  }
}

test('Transfer 调用方使用 Owner 搜索范围、确认资源，并去重检索词命中', async () => {
  const calls = []
  const owners = resourceOwners({ search: async (query, engineID) => {
    calls.push([query, engineID])
    return { results: [
      { name: 'roads', engine_id: 8, locator: sourceLocator },
      { name: 'target', engine_id: 9, locator: 'addp://engine/9/path/public/roads?type=table' }
    ] }
  } })
  const candidates = await discoverTransferSources([{ role: '道路', search_queries: ['道路', 'roads'] }], owners, [8])
  assert.deepEqual(calls, [['道路', 8], ['roads', 8]])
  assert.equal(candidates.length, 1)
  assert.equal(candidates[0].role, '道路')
  assert.equal(candidates[0].schema_coverage, 'complete')
  assert.deepEqual(candidates[0].fields, [{ name: 'road_id', type: 'bigint' }])
  assert.equal('connection_info' in candidates[0], false)
  assert.equal('connection_info' in resourceFact(candidates[0]), false)
})

test('Transfer 保留同名资源歧义，不让意图中的 locator 或字段成为资源事实', async () => {
  const other = 'addp://engine/9/path/public/roads?type=table&item_id=61'
  const owners = resourceOwners({
    search: async () => ({ results: [
      { name: 'roads', engine_id: 8, locator: sourceLocator },
      { name: 'roads', engine_id: 9, locator: other }
    ] }),
    ancestors: async (_engineID, locator) => ({ target_locator: locator, ancestors: [] }),
    facts: async locator => ({ locator, engine_id: locator === other ? 9 : 8, data_type: 'table', fields: [] })
  })
  const candidates = await discoverTransferSources([{
    role: '道路', search_queries: ['roads'], locator: 'fabricated', fields: [{ name: 'guessed' }]
  }], owners)
  assert.equal(candidates.length, 2)
  assert.deepEqual(candidates.map(candidate => candidate.locator), [sourceLocator, other])
  assert.deepEqual(candidates.map(candidate => candidate.fields), [[], []])
})

test('Transfer 确认时重新获取 Owner 事实，不复用候选字段快照', async () => {
  const source = { role: 'source', locator: sourceLocator, engine_id: 8, fields: [{ name: 'obsolete' }] }
  const confirmed = await verifyTransferSource(source, resourceOwners())
  assert.deepEqual(confirmed.fields, [{ name: 'road_id', type: 'bigint' }])
  assert.deepEqual(source.fields, [{ name: 'obsolete' }])
})

test('Transfer 拒绝未确认或矛盾的 Owner 响应，并保留 Owner 授权/服务错误', async () => {
  const source = { role: 'source', locator: sourceLocator, engine_id: 8 }
  for (const override of [
    { ancestors: async () => ({ target_locator: 'other' }) },
    { facts: async () => ({ locator: sourceLocator, engine_id: 9, data_type: 'table', fields: [] }) },
    { facts: async () => ({ locator: 'other', engine_id: 8, data_type: 'table', fields: [] }) },
    { facts: async () => ({ locator: sourceLocator, engine_id: 8, data_type: 'table', fields: null }) },
    { facts: async () => ({ locator: sourceLocator, engine_id: 8, data_type: 'table', fields: Array(201).fill({}) }) }
  ]) {
    await assert.rejects(verifyTransferSource(source, resourceOwners(override)), /transfer_source_owner_response_invalid/)
  }
  const forbidden = new Error('Owner permission denied')
  await assert.rejects(verifyTransferSource(source, resourceOwners({ facts: async () => { throw forbidden } })), error => error === forbidden)
  await assert.rejects(discoverTransferSources([{ role: 'source', search_queries: ['roads'] }], resourceOwners({
    search: async () => { throw forbidden }
  })), error => error === forbidden)
  await assert.rejects(discoverTransferSources([], resourceOwners()), /transfer_source_intent_invalid/)
})

test('Owner 契约省略空字段时，不沿用候选中旧字段', async () => {
  const source = { role: 'source', locator: sourceLocator, engine_id: 8, fields: [{ name: 'obsolete' }] }
  const result = await verifyTransferSource(source, resourceOwners({
    facts: async () => ({ locator: sourceLocator, engine_id: 8, data_type: 'table' })
  }))
  assert.deepEqual(result.fields, [])
})

test('Transfer 单次发现最多确认 20 个候选，不继续扩大检索范围', async () => {
  let searches = 0
  let factReads = 0
  const result = await discoverTransferSources([{ role: 'source', search_queries: ['roads', 'road'] }], resourceOwners({
    search: async () => {
      searches += 1
      return { results: Array.from({ length: 25 }, (_, index) => ({
        name: `roads_${index}`, engine_id: 8, locator: `addp://engine/8/path/public/roads_${index}?type=table`
      })) }
    },
    ancestors: async (_engineID, locator) => ({ target_locator: locator, ancestors: [] }),
    facts: async locator => {
      factReads += 1
      return { locator, engine_id: 8, data_type: 'table', fields: [] }
    }
  }))
  assert.equal(result.length, 20)
  assert.equal(searches, 1)
  assert.equal(factReads, 20)
})

test('Transfer Copilot source confirmation prefers authoritative Meta field facts', () => {
  const candidateFields = [{ name: 'geometry', type: 'geometry' }]
  const metadataFields = [{ name: 'geometry', type: 'geometry', nullable: false }]
  assert.deepEqual(resolveAuthoritativeSourceFields(candidateFields, metadataFields, 51572), metadataFields)
  assert.deepEqual(resolveAuthoritativeSourceFields(candidateFields, [], 51572), [])
  assert.deepEqual(resolveAuthoritativeSourceFields(candidateFields, [], 0), candidateFields)
})

test('Transfer Copilot groups every verified source candidate without dropping ambiguity', () => {
  const candidates = [
    { role: '道路', engine_id: 8, locator: 'addp://engine/8/path/public/roads?type=table' },
    { role: '道路', engine_id: 9, locator: 'addp://engine/9/path/public/roads?type=table' }
  ]
  const groups = groupResourceCandidates(candidates)
  assert.equal(groups.length, 1)
  assert.equal(groups[0].candidates.length, 2)
  assert.notEqual(resourceCandidateKey(candidates[0]), resourceCandidateKey(candidates[1]))
})

test('Transfer Copilot forwards only verified resource facts and real fields', () => {
  assert.deepEqual(resourceFact({
    role: '道路',
    engine_id: 8,
    locator: 'addp://engine/8/path/public/roads?type=table',
    data_type: 'table',
    fields: [{ name: 'road_id', type: 'bigint' }],
    recommendation_reason: 'name match'
  }), {
    role: '道路',
    engine_id: 8,
    locator: 'addp://engine/8/path/public/roads?type=table',
    data_type: 'table',
    fields: [{ name: 'road_id', type: 'bigint' }]
  })
})

test('Transfer Copilot infers a registered target engine from the target phrase', () => {
  const mysql = { id: 3, name: 'Business MySQL', engine_type: 'mysql' }
  const postgresql = { id: 2, name: 'Business PostgreSQL', engine_type: 'postgresql' }
  assert.equal(inferTargetEngineFromPrompt('从 pg 到 mysql，同步 farmland', [postgresql, mysql]), mysql)
  assert.equal(inferTargetEngineFromPrompt('同步 farmland，目标是 mysql 库', [postgresql, mysql]), mysql)
  assert.equal(inferTargetEngineFromPrompt('同步 farmland', [postgresql, mysql]), null)
  assert.equal(inferSourceEngineFromPrompt('从 pg 到 mysql，同步 farmland', [postgresql, mysql]), postgresql)
  assert.equal(inferSourceEngineFromPrompt('从 pg 到 mysql，同步 farmland', [
    postgresql,
    { id: 4, name: 'Another PostgreSQL', engine_type: 'postgresql' },
    mysql
  ]), null)
  assert.deepEqual(inferSourceEnginesFromPrompt('从 pg 到 mysql，同步 farmland', [
    postgresql,
    { id: 4, name: 'Another PostgreSQL', engine_type: 'postgresql' },
    mysql
  ]).map(engine => engine.id), [2, 4])
})

test('Transfer Copilot resolves the target engine whenever target configuration is requested', () => {
  const clarification = {
    status: 'need_clarification',
    clarification_reason: 'target_configuration_required'
  }
  const mysql = { id: 3, name: 'Business MySQL', engine_type: 'mysql' }
  assert.equal(needsTargetConfiguration(clarification), true)
  assert.equal(
    inferTargetEngineForClarification(clarification, '从 pg 到 mysql，同步 farmland', [mysql]),
    mysql
  )
  assert.equal(needsTargetConfiguration({ status: 'need_clarification' }), false)
  assert.equal(inferTargetEngineForClarification({ status: 'need_clarification' }, '到 mysql', [mysql]), null)
})
