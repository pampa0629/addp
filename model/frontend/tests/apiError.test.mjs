import test from 'node:test'
import assert from 'node:assert/strict'
import { getModelErrorCode, getModelErrorMessage, isPermissionDenied } from '../src/utils/apiError.js'

test('permission errors use a stable code', () => {
  const error = { response: { status: 403, data: { error_code: 'permission_denied' } } }
  assert.equal(isPermissionDenied(error), true)
  assert.equal(getModelErrorCode(error), 'permission_denied')
  assert.equal(isPermissionDenied({ response: { status: 400, data: { error_code: 'permission_denied' } } }), true)
})

test('not found and generic errors remain distinguishable', () => {
  assert.equal(getModelErrorCode({ response: { status: 404, data: {} } }), 'not_found')
  assert.equal(getModelErrorCode({ response: { status: 400, data: { error_code: 'ddl_preview_invalid' } } }), 'ddl_preview_invalid')
  assert.equal(getModelErrorCode({}), 'model_operation_failed')
})

test('permission feedback is local while domain and upstream messages are preserved', () => {
  const t = key => ({
    'model.common.permission_denied': 'permission denied',
    fallback: 'fallback'
  })[key]

  assert.equal(
    getModelErrorMessage({ response: { status: 403, data: { error: 'upstream text' } } }, t, 'fallback'),
    'permission denied'
  )
  for (const status of [400, 404, 409, 503]) {
    assert.equal(
      getModelErrorMessage({ response: { status, data: { error: `domain ${status}` } } }, t, 'fallback'),
      `domain ${status}`
    )
  }
  assert.equal(getModelErrorMessage({}, t, 'fallback'), 'fallback')
})
