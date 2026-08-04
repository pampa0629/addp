import test from 'node:test'
import assert from 'node:assert/strict'

import {
  applyExistingTargetFields,
  applyFieldMappingEdit
} from '../src/views/TaskWizard/fieldMapping.mjs'

test('existing target fields define target types and decimal facts', () => {
  const mappings = [
    { source_field: 'area', target_field: 'AREA', target_type: 'decimal', precision: 18, scale: 4 },
    { source_field: 'name', target_field: 'name', target_type: 'string' },
    { source_field: 'missing', target_field: 'missing', target_type: 'bigint' }
  ]
  const targetFields = [
    { name: 'area', type: 'decimal', precision: 24, scale: 8 },
    { name: 'name', type: 'string' }
  ]

  assert.deepEqual(applyExistingTargetFields(mappings, targetFields), [
    { source_field: 'area', target_field: 'area', target_type: 'decimal', precision: 24, scale: 8 },
    { source_field: 'name', target_field: 'name', target_type: 'string' },
    { source_field: 'missing', target_field: 'missing', target_type: 'bigint' }
  ])
})

test('manual decimal facts survive ordinary mapping edits', () => {
  const mapping = {
    source_field: 'area',
    target_field: 'area',
    target_type: 'decimal',
    precision: 20,
    scale: 10
  }

  assert.deepEqual(
    applyFieldMappingEdit(mapping, { name: 'area', type: 'decimal' }),
    mapping
  )
  assert.deepEqual(
    applyFieldMappingEdit(
      { ...mapping, precision: 24, scale: undefined },
      { name: 'area', type: 'decimal', precision: 18, scale: 4 }
    ),
    { ...mapping, precision: 24, scale: undefined }
  )
})
