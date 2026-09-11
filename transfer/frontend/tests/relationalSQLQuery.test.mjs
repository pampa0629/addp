import test from 'node:test'
import assert from 'node:assert/strict'

import {
  compileRelationalSQLQuery,
  createRelationalSQLQuery,
  parseRelationalSQLQuery,
  relationalSQLFilterOperators,
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
  parametersSupported: true
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
    parameters: { p1: 'active', p2: 1000.5 }
  })
})

test('any-match filters and IN values use stable ordered parameters', () => {
  const model = createRelationalSQLQuery(options.sourcePath, sourceFields)
  model.selectedFields = ['id']
  model.matchMode = 'any'
  model.filters = [
    { field: 'status', operator: 'in', value: ['active', 'pending'] },
    { field: 'id', operator: 'eq', value: 42 }
  ]

  const compiled = compileRelationalSQLQuery(model, options)
  assert.equal(compiled.statement, 'SELECT "id" FROM "public"."farmland" WHERE "status" IN (:p1, :p2) OR "id" = :p3')
  assert.deepEqual(compiled.parameters, { p1: 'active', p2: 'pending', p3: 42 })
})

test('canonical generated SQL round-trips while SQL IDE features stay advanced-only', () => {
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
    value: index + 1
  }))
  const compiled = compileRelationalSQLQuery(model, options)
  const reorderedParameters = Object.fromEntries(
    Object.entries(compiled.parameters).sort(([left], [right]) => left.localeCompare(right))
  )

  assert.equal(parseRelationalSQLQuery(compiled.statement, reorderedParameters, options).supported, true)
})

test('field types constrain filters and output fields preserve Meta facts', () => {
  assert.deepEqual(relationalSQLFilterOperators(sourceFields[1]), ['eq', 'ne', 'in', 'is_null', 'is_not_null'])
  assert.deepEqual(relationalSQLFilterOperators(sourceFields[2]), ['eq', 'ne', 'gt', 'gte', 'lt', 'lte', 'in', 'is_null', 'is_not_null'])
  assert.deepEqual(relationalSQLFilterOperators(sourceFields[4]), [])
  assert.deepEqual(relationalSQLFilterOperators(sourceFields[5]), [])

  const model = createRelationalSQLQuery(options.sourcePath, sourceFields)
  model.selectedFields = ['status', 'geom']
  assert.deepEqual(relationalSQLOutputFields(model, sourceFields), [sourceFields[1], sourceFields[5]])
})

test('filters are rejected when the engine does not declare parameter binding', () => {
  const model = createRelationalSQLQuery(options.sourcePath, sourceFields)
  model.filters = [{ field: 'status', operator: 'eq', value: 'active' }]
  assert.deepEqual(
    validateRelationalSQLQuery(model, sourceFields, false).map(issue => issue.code),
    ['parameters_unsupported']
  )
})
