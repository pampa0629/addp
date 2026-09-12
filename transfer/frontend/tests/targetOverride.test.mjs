import test from 'node:test'
import assert from 'node:assert/strict'

import {
  TARGET_OVERRIDE_POLICY,
  targetOverrideAfterParentSelection,
  targetOverrideEligible,
  withTargetOverride
} from '../src/views/TaskWizard/targetOverride.mjs'

test('target override eligibility is independent of source query state', () => {
  const candidate = {
    boundary: 'bounded',
    loadMode: 'snapshot',
    representation: 'native',
    existingTarget: true,
    applyMode: 'append'
  }
  assert.equal(targetOverrideEligible(candidate), true)
  assert.equal(targetOverrideEligible({ ...candidate, loadMode: 'incremental' }), false)
  assert.equal(targetOverrideEligible({ ...candidate, existingTarget: false }), false)
  assert.equal(targetOverrideEligible({ ...candidate, applyMode: 'replace' }), false)
})

test('target override augments a complete saved default target', () => {
  const target = {
    parent_locator: 'addp://engine/2/path/public?type=schema',
    name: 'default_target',
    data_type: 'table',
    representation: 'native',
    policy: { apply_mode: 'append' }
  }
  assert.deepEqual(withTargetOverride(target, true), {
    ...target,
    override_policy: TARGET_OVERRIDE_POLICY
  })
  assert.equal(withTargetOverride(target, false), target)
})

test('restoring the same target parent preserves existing-table override state', () => {
  assert.deepEqual(targetOverrideAfterParentSelection({
    parentChanged: false,
    existingTarget: true,
    overrideEnabled: true
  }), {
    existingTarget: true,
    overrideEnabled: true
  })
  assert.deepEqual(targetOverrideAfterParentSelection({
    parentChanged: true,
    existingTarget: true,
    overrideEnabled: true
  }), {
    existingTarget: false,
    overrideEnabled: false
  })
})
