import assert from 'node:assert/strict'
import test from 'node:test'
import { normalizeDomainFilter, executionDomainLabel } from '../src/utils/domainOwnership.js'
import { resolvePlanRouteState } from '../src/utils/planRouteState.js'
import { resolveRuleRouteState } from '../src/utils/ruleRouteState.js'
import { resolveIssueListRouteState } from '../src/utils/issueListRouteState.js'

test('all, public and exact-domain filters survive canonical route restoration', () => {
  for (const resolve of [resolvePlanRouteState, resolveRuleRouteState, resolveIssueListRouteState]) {
    for (const value of ['0', '42']) {
      const state = resolve({ owner_domain_id: value })
      assert.equal(state.ownerDomainID, Number(value))
      assert.deepEqual(state.query, { owner_domain_id: value })
      assert.equal(state.changed, false)
    }
    assert.equal(resolve({}).ownerDomainID, null)
    assert.deepEqual(resolve({ owner_domain_id: '-1' }).query, {})
  }
  for (const value of [null, '', '-1', '1.1', '9007199254740992', ['42']]) assert.equal(normalizeDomainFilter(value), null)
})

test('execution ownership distinguishes historical absence from explicit public snapshots', () => {
  const t = (key, args) => args ? `${key}:${args.id}` : key
  assert.equal(executionDomainLabel(undefined, t), 'quality.domain.unrecorded')
  assert.equal(executionDomainLabel({}, t), 'quality.domain.unrecorded')
  assert.equal(executionDomainLabel({ owner_domain_id: null }, t), 'quality.domain.public')
  assert.equal(executionDomainLabel({ owner_domain_id: 42 }, t), 'quality.domain.identifier:42')
})
