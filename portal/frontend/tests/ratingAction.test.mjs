import test from 'node:test'
import assert from 'node:assert/strict'
import { resolveRatingAction } from '../src/utils/ratingAction.js'

test('a create-only user can submit without application status read', () => {
  assert.equal(resolveRatingAction({ hasOwnRating: false, canCreate: true, canUpdate: false, knownAccessStatus: null }), 'create')
  assert.equal(resolveRatingAction({ hasOwnRating: true, canCreate: true, canUpdate: false, knownAccessStatus: null }), null)
})

test('an update-only user can edit only an existing own rating', () => {
  assert.equal(resolveRatingAction({ hasOwnRating: true, canCreate: false, canUpdate: true, knownAccessStatus: null }), 'update')
  assert.equal(resolveRatingAction({ hasOwnRating: false, canCreate: false, canUpdate: true, knownAccessStatus: null }), null)
})

test('a known missing or pending asset grant hides rating actions', () => {
  for (const status of ['none', 'pending', 'fulfilling', 'revoking']) {
    assert.equal(resolveRatingAction({ hasOwnRating: false, canCreate: true, canUpdate: true, knownAccessStatus: status }), null)
  }
  assert.equal(resolveRatingAction({ hasOwnRating: false, canCreate: true, canUpdate: true, knownAccessStatus: 'effective' }), 'create')
  assert.equal(resolveRatingAction({ hasOwnRating: true, canCreate: true, canUpdate: true, knownAccessStatus: 'effective' }), 'update')
})
