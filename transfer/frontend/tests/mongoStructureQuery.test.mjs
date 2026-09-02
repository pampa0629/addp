import test from 'node:test'
import assert from 'node:assert/strict'

import {
  compileMongoStructureQuery,
  createMongoPathProjection,
  createMongoStructureQuery,
  defaultMongoIndexOutput,
  defaultMongoOutputName,
  isMongoArrayElementLeafField,
  isMongoParentLeafField,
  isMongoProjectionLeafField,
  mongoStructureOutputFields,
  parseMongoStructureQuery,
  validateMongoStructureQuery
} from '../src/views/TaskWizard/mongoStructureQuery.mjs'

const personsStatement = '{"aggregate":"Persons","pipeline":[{"$project":{"_id":"$_id","_openid":{"$ifNull":["$_openid",null]},"userInfo__nickName":{"$ifNull":["$userInfo.nickName",null]},"userInfo__gender":{"$ifNull":["$userInfo.gender",null]}}}]}'
const activitiesStatement = '{"aggregate":"Outdoors","pipeline":[{"$project":{"_id":"$_id","status":{"$ifNull":["$status",null]},"title__date":{"$ifNull":["$title.date",null]},"title__level":{"$ifNull":["$title.level",null]},"leader__personid":{"$ifNull":["$leader.personid",null]},"leader__userInfo__nickName":{"$ifNull":["$leader.userInfo.nickName",null]}}}]}'
const membersStatement = '{"aggregate":"Outdoors","pipeline":[{"$unwind":{"path":"$members","preserveNullAndEmptyArrays":false,"includeArrayIndex":"members__index"}},{"$project":{"_id":"$_id","_openid":{"$ifNull":["$_openid",null]},"QcCode":{"$ifNull":["$QcCode",null]},"members__personid":{"$ifNull":["$members.personid",null]},"members__entryInfo__status":{"$ifNull":["$members.entryInfo.status",null]},"members__userInfo__nickName":{"$ifNull":["$members.userInfo.nickName",null]},"members__index":1}}]}'

test('the three canonical MongoDB structure queries round-trip without extra stages', () => {
  for (const statement of [personsStatement, activitiesStatement, membersStatement]) {
    const parsed = parseMongoStructureQuery(statement)
    assert.equal(parsed.supported, true)
    assert.equal(compileMongoStructureQuery(parsed.model), statement)
    const pipeline = JSON.parse(statement).pipeline
    assert.equal(pipeline.some(stage => '$match' in stage || '$sort' in stage), false)
  }
})

test('document mode carries the record identifier and selected source fields only', () => {
  const sourceFields = [
    { name: '_id', type: 'string', primary_key: true, nullable: false },
    { name: 'profile.address.city', type: 'string', nullable: true }
  ]
  const model = createMongoStructureQuery('customers')
  model.projections.push(
    createMongoPathProjection('_id', sourceFields),
    createMongoPathProjection('profile.address.city', sourceFields, model.projections)
  )

  assert.equal(
    compileMongoStructureQuery(model),
    '{"aggregate":"customers","pipeline":[{"$project":{"_id":"$_id","profile__address__city":{"$ifNull":["$profile.address.city",null]}}}]}'
  )
})

test('array mode carries the parent identifier, selected parent fields, element fields, and optional index', () => {
  const sourceFields = [
    { name: '_id', type: 'string', primary_key: true, nullable: false },
    { name: '_openid', type: 'string', nullable: true },
    { name: 'items.sku', type: 'string', nullable: true }
  ]
  const model = createMongoStructureQuery('orders')
  model.unwind = {
    enabled: true,
    path: 'items',
    includeIndex: true,
    indexOutput: defaultMongoIndexOutput('items')
  }
  model.projections.push(
    createMongoPathProjection('_id', sourceFields),
    createMongoPathProjection('_openid', sourceFields, model.projections),
    createMongoPathProjection('items.sku', sourceFields, [{ output: model.unwind.indexOutput }])
  )

  const output = mongoStructureOutputFields(model, sourceFields)
  assert.deepEqual(output.map(field => ({ name: field.name, path: field.source_path, role: field.source_role })), [
    { name: '_id', path: '_id', role: 'parent_identifier' },
    { name: '_openid', path: '_openid', role: 'parent_field' },
    { name: 'items__index', path: 'items', role: 'array_index' },
    { name: 'items__sku', path: 'items.sku', role: 'array_element_field' }
  ])
})

test('basic mode rejects filters, sorts, multiple unwind stages, and business aggregation', () => {
  const statements = [
    '{"aggregate":"orders","pipeline":[{"$match":{"_id":{"$ne":""}}},{"$project":{"_id":"$_id"}}]}',
    '{"aggregate":"orders","pipeline":[{"$project":{"_id":"$_id"}},{"$sort":{"_id":1}}]}',
    '{"aggregate":"orders","pipeline":[{"$unwind":{"path":"$items","preserveNullAndEmptyArrays":false}},{"$unwind":{"path":"$items.discounts","preserveNullAndEmptyArrays":false}},{"$project":{"_id":"$_id"}}]}',
    '{"aggregate":"orders","pipeline":[{"$group":{"_id":"$customer_id"}}]}'
  ]
  statements.forEach(statement => assert.equal(parseMongoStructureQuery(statement).supported, false))
})

test('basic mode rejects noncanonical aliases and accepts parent fields beside the selected array', () => {
  assert.deepEqual(
    parseMongoStructureQuery('{"aggregate":"orders","pipeline":[{"$project":{"order_id":"$_id"}}]}'),
    { supported: false, reason: 'noncanonical_output' }
  )

  const model = createMongoStructureQuery('orders')
  model.unwind = { enabled: true, path: 'items', includeIndex: false, indexOutput: '' }
  model.projections.push(
    { source: '_id', output: '_id', nullable: false },
    { source: 'customer.name', output: 'customer__name', nullable: true }
  )
  assert.deepEqual(validateMongoStructureQuery(model), [])
  assert.equal(parseMongoStructureQuery(compileMongoStructureQuery(model)).supported, true)
})

test('output aliases are deterministic and collision-safe without user configuration', () => {
  assert.equal(defaultMongoOutputName('items.product.sku'), 'items__product__sku')
  const first = createMongoPathProjection('a.b')
  const second = createMongoPathProjection('a__b', [], [first])
  assert.deepEqual([first.output, second.output], ['a__b', 'a__b__2'])
})

test('validation requires the record identifier and selected fields', () => {
  const model = createMongoStructureQuery('orders')
  assert.deepEqual(validateMongoStructureQuery(model).map(item => item.code), [
    'projection_required',
    'identifier_required'
  ])
})

test('mixed leaf fields remain selectable while structural fields stay excluded', () => {
  assert.equal(isMongoProjectionLeafField({ name: 'members.userInfo.phone', type: 'mixed' }), true)
  assert.equal(isMongoProjectionLeafField({ name: 'members', type: 'array' }), false)
  assert.equal(isMongoProjectionLeafField({ name: 'members.userInfo', type: 'json' }), false)
  assert.equal(isMongoProjectionLeafField({ name: 'members.entryInfo', native_type: 'object' }), false)
})

test('parent and array element candidates preserve one-array row grain', () => {
  const sourceFields = [
    { name: '_id', type: 'string' },
    { name: '_openid', type: 'string' },
    { name: 'leader.userInfo.nickName', type: 'string' },
    { name: 'members', type: 'array' },
    { name: 'members.personid', type: 'string' },
    { name: 'members.tags', type: 'array' },
    { name: 'members.tags.name', type: 'string' },
    { name: 'aaMembers', type: 'array' },
    { name: 'aaMembers.personid', type: 'string' }
  ]

  assert.equal(isMongoParentLeafField(sourceFields[1], sourceFields), true)
  assert.equal(isMongoParentLeafField(sourceFields[2], sourceFields), true)
  assert.equal(isMongoParentLeafField(sourceFields[4], sourceFields), false)
  assert.equal(isMongoArrayElementLeafField(sourceFields[4], 'members', sourceFields), true)
  assert.equal(isMongoArrayElementLeafField(sourceFields[6], 'members', sourceFields), false)
  assert.equal(isMongoArrayElementLeafField(sourceFields[8], 'members', sourceFields), false)
})
