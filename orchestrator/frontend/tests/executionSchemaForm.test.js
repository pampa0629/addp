import assert from 'node:assert/strict'
import test from 'node:test'

import {
  activeTaskCapabilityMetadata,
  createParameterDraft,
  executionParameterMode,
  executionSchemaFields,
  serializeParameterDraft,
  validateParameterDraft
} from '../src/utils/executionSchemaForm.js'

const overwriteSchema = {
  type: 'object',
  properties: {
    existing_result_action: { type: 'string', enum: ['overwrite'] }
  },
  additionalProperties: false
}

test('execution schema metadata does not depend on edit URL', () => {
  assert.deepEqual(activeTaskCapabilityMetadata([
    { type: 'scan', execution_schema: overwriteSchema },
    { type: 'legacy', deprecated: true, execution_schema: overwriteSchema }
  ]), [{
    type: 'scan',
    editUrl: '',
    executionSchema: overwriteSchema
  }])
})

test('closed scalar execution schema uses structured parameters', () => {
  assert.equal(executionParameterMode(overwriteSchema), 'structured')
  assert.deepEqual(executionSchemaFields(overwriteSchema), [{
    name: 'existing_result_action',
    schema: { type: 'string', enum: ['overwrite'] },
    required: false
  }])
  assert.deepEqual(createParameterDraft(overwriteSchema, {}), { existing_result_action: null })
  assert.deepEqual(
    serializeParameterDraft(overwriteSchema, { existing_result_action: 'overwrite' }),
    { existing_result_action: 'overwrite' }
  )
})

test('closed empty schema has no execution parameters', () => {
  assert.equal(executionParameterMode({ type: 'object', additionalProperties: false }), 'empty')
})

test('open and complex schemas use the JSON object editor', () => {
  assert.equal(executionParameterMode({ type: 'object', additionalProperties: true }), 'json')
  assert.equal(executionParameterMode({
    type: 'object',
    properties: { payload: { type: 'object', additionalProperties: true } },
    additionalProperties: false
  }), 'json')
})

test('non-string template values preserve the JSON editor path', () => {
  const schema = {
    type: 'object',
    properties: { sample_size: { type: 'integer' } },
    additionalProperties: false
  }
  assert.equal(executionParameterMode(schema, { sample_size: '{{scan.sample_size}}' }), 'json')
})

test('undeclared existing parameters remain visible in the JSON editor', () => {
  assert.equal(executionParameterMode(overwriteSchema, { legacy_action: true }), 'json')
})

test('defaults, optional omission and validation follow the schema', () => {
  const schema = {
    type: 'object',
    properties: {
      force: { type: 'boolean', default: false },
      sample_size: { type: 'integer', minimum: 1, maximum: 1000 },
      label: { type: 'string', minLength: 2 }
    },
    required: ['sample_size'],
    additionalProperties: false
  }
  assert.deepEqual(createParameterDraft(schema, {}), { force: false, sample_size: null, label: null })
  assert.deepEqual(serializeParameterDraft(schema, { force: false, sample_size: 10, label: '' }), {
    force: false,
    sample_size: 10
  })
  assert.deepEqual(validateParameterDraft(schema, { force: false, sample_size: null, label: null }), {
    field: 'sample_size',
    reason: 'required'
  })
  assert.deepEqual(validateParameterDraft(schema, { force: false, sample_size: 1001, label: null }), {
    field: 'sample_size',
    reason: 'maximum',
    limit: 1000
  })
})

test('string length follows JSON Schema Unicode character semantics', () => {
  const schema = {
    type: 'object',
    properties: {
      label: { type: 'string', minLength: 1, maxLength: 1 }
    },
    additionalProperties: false
  }

  assert.equal(validateParameterDraft(schema, { label: '🚀' }), null)
})
