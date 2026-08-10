import test from 'node:test'
import assert from 'node:assert/strict'

import {
  groupResourceCandidates,
  inferTargetEngineForClarification,
  inferTargetEngineFromPrompt,
  inferSourceEngineFromPrompt,
  needsTargetConfiguration,
  resourceCandidateKey,
  resourceFact
} from '../src/utils/transferCopilot.mjs'

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
