import test from 'node:test'
import assert from 'node:assert/strict'

import {
  compileRelationalSQLQuery,
  createRelationalSQLQuery,
  parseRelationalSQLQuery,
  relationalSQLFilterOperators,
  relationalSQLParameterType,
  relationalSQLOutputFields,
  validateRelationalSQLQuery
} from '../src/views/TaskWizard/relationalSQLQuery.mjs'

const sourceFields = [
  { name: 'id', type: 'bigint', primary_key: true, nullable: false },
  { name: 'status', type: 'string', nullable: false },
  { name: 'area', type: 'decimal', nullable: true },
  { name: 'observed_at', type: 'timestamp', nullable: true },
  { name: 'attributes', type: 'json', nullable: true },
  { name: 'geom', type: 'geometry', nullable: true }
]

const options = {
  sourcePath: ['public', 'farmland'],
  sourceFields,
  identifierQuote: '"',
  parametersSupported: true,
  parameterTypes: new Set(['string', 'integer', 'number', 'boolean'])
}

test('basic relational query compiles projection and one-level typed filters', () => {
  const model = createRelationalSQLQuery(options.sourcePath, sourceFields)
  model.selectedFields = ['id', 'status', 'area']
  model.filters = [
    { field: 'status', operator: 'eq', value: 'active' },
    { field: 'area', operator: 'gte', value: '1000.5' },
    { field: 'observed_at', operator: 'is_not_null' }
  ]

  assert.deepEqual(compileRelationalSQLQuery(model, options), {
    statement: 'SELECT "id", "status", "area" FROM "public"."farmland" WHERE "status" = :p1 AND "area" >= :p2 AND "observed_at" IS NOT NULL',
    parameters: { p1: 'active', p2: '1000.5' }
  })
})

test('any-match filters and IN values use stable ordered parameters', () => {
  const model = createRelationalSQLQuery(options.sourcePath, sourceFields)
  model.selectedFields = ['id']
  model.matchMode = 'any'
  model.filters = [
    { field: 'status', operator: 'in', value: ['active', 'pending'] },
    { field: 'id', operator: 'eq', value: '9007199254740993' }
  ]

  const compiled = compileRelationalSQLQuery(model, options)
  assert.equal(compiled.statement, 'SELECT "id" FROM "public"."farmland" WHERE "status" IN (:p1, :p2) OR "id" = :p3')
  assert.deepEqual(compiled.parameters, { p1: 'active', p2: 'pending', p3: '9007199254740993' })
})

test('canonical generated SQL round-trips while SQL IDE features remain outside Transfer', () => {
  const statement = 'SELECT "id", "status" FROM "public"."farmland" WHERE "status" = :p1'
  const parsed = parseRelationalSQLQuery(statement, { p1: 'active' }, options)
  assert.equal(parsed.supported, true)
  assert.equal(compileRelationalSQLQuery(parsed.model, options).statement, statement)

  for (const unsupported of [
    'SELECT COUNT(*) FROM "public"."farmland"',
    'SELECT "id" AS "source_id" FROM "public"."farmland"',
    'SELECT "id" FROM "public"."farmland" ORDER BY "id"',
    'SELECT f."id" FROM "public"."farmland" f JOIN "public"."owners" o ON o."id" = f."id"'
  ]) {
    assert.equal(parseRelationalSQLQuery(unsupported, {}, options).supported, false)
  }
})

test('canonical parsing ignores JSON object key order for more than nine parameters', () => {
  const model = createRelationalSQLQuery(options.sourcePath, sourceFields)
  model.selectedFields = ['id']
  model.filters = Array.from({ length: 11 }, (_, index) => ({
    field: 'id',
    operator: 'ne',
    value: String(index + 1)
  }))
  const compiled = compileRelationalSQLQuery(model, options)
  const reorderedParameters = Object.fromEntries(
    Object.entries(compiled.parameters).sort(([left], [right]) => left.localeCompare(right))
  )

  assert.equal(parseRelationalSQLQuery(compiled.statement, reorderedParameters, options).supported, true)
})

test('field types and declared parameter types constrain filters while preserving Meta facts', () => {
  assert.deepEqual(relationalSQLFilterOperators(sourceFields[1], options.parameterTypes), ['eq', 'ne', 'in', 'is_null', 'is_not_null'])
  assert.deepEqual(relationalSQLFilterOperators(sourceFields[2], options.parameterTypes), ['eq', 'ne', 'gt', 'gte', 'lt', 'lte', 'in', 'is_null', 'is_not_null'])
  assert.deepEqual(relationalSQLFilterOperators(sourceFields[4], options.parameterTypes), [])
  assert.deepEqual(relationalSQLFilterOperators(sourceFields[5], options.parameterTypes), [])
  assert.deepEqual(relationalSQLFilterOperators(sourceFields[2], new Set(['integer'])), ['is_null', 'is_not_null'])
  assert.deepEqual(relationalSQLFilterOperators({ name: 'count', type: 'int' }, new Set(['number'])), ['eq', 'ne', 'gt', 'gte', 'lt', 'lte', 'in', 'is_null', 'is_not_null'])
  assert.equal(relationalSQLParameterType(sourceFields[0]), 'string')
  assert.equal(relationalSQLParameterType(sourceFields[2]), 'string')

  const model = createRelationalSQLQuery(options.sourcePath, sourceFields)
  model.selectedFields = ['status', 'geom']
  assert.deepEqual(relationalSQLOutputFields(model, sourceFields), [sourceFields[1], sourceFields[5]])
})

test('filters are rejected when the engine does not declare parameter binding', () => {
  const model = createRelationalSQLQuery(options.sourcePath, sourceFields)
  model.filters = [{ field: 'status', operator: 'eq', value: 'active' }]
  assert.deepEqual(
    validateRelationalSQLQuery(model, sourceFields, false, options.parameterTypes).map(issue => issue.code),
    ['parameters_unsupported']
  )
})

test('bigint and decimal filters require and preserve exact decimal text', () => {
  const model = createRelationalSQLQuery(options.sourcePath, sourceFields)
  model.selectedFields = ['id', 'area']
  model.filters = [
    { field: 'id', operator: 'eq', value: '9007199254740993' },
    { field: 'area', operator: 'eq', value: '12345678901234567890.12345678901234567890' }
  ]

  const compiled = compileRelationalSQLQuery(model, options)
  assert.deepEqual(compiled.parameters, {
    p1: '9007199254740993',
    p2: '12345678901234567890.12345678901234567890'
  })
  assert.equal(parseRelationalSQLQuery(compiled.statement, compiled.parameters, options).supported, true)

  model.filters[0].value = 9007199254740992
  assert.deepEqual(
    validateRelationalSQLQuery(model, sourceFields, true, options.parameterTypes).map(issue => issue.code),
    ['filter_value_required']
  )
})

test('integer fields reject fractional and unsafe JSON number values', () => {
  const integerField = { name: 'count', type: 'int' }
  const model = createRelationalSQLQuery(options.sourcePath, [integerField])
  model.filters = [{ field: 'count', operator: 'eq', value: 1.5 }]
  assert.deepEqual(
    validateRelationalSQLQuery(model, [integerField], true, options.parameterTypes).map(issue => issue.code),
    ['filter_value_required']
  )
  model.filters[0].value = Number.MAX_SAFE_INTEGER + 1
  assert.deepEqual(
    validateRelationalSQLQuery(model, [integerField], true, options.parameterTypes).map(issue => issue.code),
    ['filter_value_required']
  )
})
