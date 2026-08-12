import test from 'node:test'
import assert from 'node:assert/strict'
import { getModelErrorCode, isPermissionDenied } from '../src/utils/apiError.js'

test('permission errors use a stable code', () => {
  const error = { response: { status: 403, data: { error_code: 'permission_denied' } } }
  assert.equal(isPermissionDenied(error), true)
  assert.equal(getModelErrorCode(error), 'permission_denied')
})

test('not found and generic errors remain distinguishable', () => {
  assert.equal(getModelErrorCode({ response: { status: 404, data: {} } }), 'not_found')
  assert.equal(getModelErrorCode({ response: { status: 400, data: { error_code: 'ddl_preview_invalid' } } }), 'ddl_preview_invalid')
  assert.equal(getModelErrorCode({}), 'model_operation_failed')
})
