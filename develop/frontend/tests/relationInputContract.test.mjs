import test from 'node:test'
import assert from 'node:assert/strict'

import {
  invalidRelationInputs,
  normalizeRelationInputs,
  relationInputsValid
} from '../src/utils/relationInputContract.mjs'

test('relation aliases normalize to one lowercase ordered set', () => {
  assert.deepEqual(normalizeRelationInputs([' Person ', 'participation', 'person']), ['person', 'participation'])
})

test('relation aliases reject physical names and invalid identifiers', () => {
  assert.deepEqual(invalidRelationInputs(['person', 'public.person', '1activity']), ['public.person', '1activity'])
  assert.equal(relationInputsValid(['person', 'participation']), true)
  assert.equal(relationInputsValid(['person', 'person']), false)
})
