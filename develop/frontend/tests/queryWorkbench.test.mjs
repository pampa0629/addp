import assert from 'node:assert/strict'
import {
  buildQueryExecutionContract,
  buildQueryResultCSV,
  formatGeneratedQueryForEditor,
  formatMQLQuery,
  formatterLanguageForQuery,
  monacoLanguageForQuery,
  queryCapabilityForEngine,
  queryParameterReference,
  queryErrorMessage,
  queryResultFromExecution,
  diagnoseQuery,
  extractQueryParameterReferences,
  mqlCollectionReferences,
  matchMQLCollectionReferences,
  isQueryInputResource,
  mqlPrimaryCollection,
  parseSQLSources
} from '../src/utils/queryWorkbench.mjs'

const capability = queryCapabilityForEngine({
  capabilities: JSON.stringify({
    compute: {
      query: {
        supported: true,
        languages: ['Cypher', 'cypher'],
        default_language: 'cypher',
        result_kinds: ['graph', 'table'],
        parameters: {
          supported: true,
          languages: ['cypher'],
          types: ['string', 'integer', 'number', 'boolean']
        }
      }
    }
  })
})
assert.deepEqual(capability, {
  languages: ['cypher'],
  defaultLanguage: 'cypher',
  resultKinds: ['graph', 'table'],
  parameters: {
    supported: true,
    languages: ['cypher'],
    types: ['string', 'integer', 'number', 'boolean']
  },
  federation: { supported: false, sourceEngineTypes: [], objectFormats: [] }
})
const federatedCapability = queryCapabilityForEngine({
  capabilities: {
    compute: {
      query: {
        supported: true,
        languages: ['SQL'],
        default_language: 'sql',
        result_kinds: ['table'],
        federation: {
          supported: true,
          source_engine_types: ['PostgreSQL', 'mysql', 'postgresql'],
          object_formats: ['Parquet']
        }
      }
    }
  }
})
assert.deepEqual(federatedCapability.federation, {
  supported: true,
  sourceEngineTypes: ['postgresql', 'mysql'],
  objectFormats: ['parquet']
})
assert.equal(queryParameterReference('sql', 'status'), ':status')
assert.equal(queryParameterReference('cypher', 'status'), '$status')
assert.equal(queryParameterReference('mql', 'status'), '{"$param":"status"}')
assert.deepEqual(
  extractQueryParameterReferences('sql', 'SELECT created_at::date FROM events WHERE status = :status'),
  ['status']
)
const parameterContract = buildQueryExecutionContract([
  { name: 'status', type: 'string', default: 'active', title: 'Status' },
  { name: 'limit', type: 'integer', default: 10 }
])
assert.equal(parameterContract.input_schema.properties.status.type, 'string')
assert.equal(parameterContract.input_defaults.limit, 10)
assert.equal(parameterContract.input_ui_schema.status.order, 0)
assert.equal(monacoLanguageForQuery('mql'), 'mql')
assert.equal(monacoLanguageForQuery('cypher'), 'cypher')
assert.equal(formatterLanguageForQuery('sql'), 'sql')
assert.equal(formatterLanguageForQuery('mql'), 'mql')
assert.equal(formatterLanguageForQuery('cypher'), '')
assert.equal(formatMQLQuery('{"find":"Persons","filter":{},"limit":10}'), `{
  "find": "Persons",
  "filter": {},
  "limit": 10
}`)
assert.equal(
  formatGeneratedQueryForEditor('{"aggregate":"Persons","pipeline":[]}', 'mql'),
  `{
  "aggregate": "Persons",
  "pipeline": []
}`
)
assert.equal(formatGeneratedQueryForEditor('SELECT * FROM users', 'sql'), 'SELECT * FROM users')
assert.throws(() => formatMQLQuery('db.Persons.find({})'))
assert.equal(
  queryErrorMessage('mongodb_database_required', 'raw provider error', key => key),
  'develop.queryResult.mongodbDatabaseRequired'
)
assert.equal(
  queryErrorMessage(
    'query_execution_failed',
    'ERROR: column "smid" does not exist (SQLSTATE 42703)',
    (key, params) => `${key}:${params.field}`
  ),
  'develop.queryResult.postgresqlUndefinedColumn:smid'
)
const diagnosticContext = {
  language: 'sql',
  fields: ['id', 'name'],
  targetLocator: 'addp://engine/1/table?item_id=2',
  referencedParameters: ['status'],
  definedParameters: []
}
assert.deepEqual(diagnoseQuery({ ...diagnosticContext, query: 'SELECT ID FROM users WHERE name = :status' }), [
  { code: 'parameter_undefined', severity: 'error', name: 'status' },
  {
    code: 'field_case_mismatch',
    severity: 'warning',
    field: 'ID',
    suggested: 'id',
    start: 7,
    end: 9,
    replacement: 'id'
  }
])
assert.deepEqual(diagnoseQuery({ ...diagnosticContext, referencedParameters: [], query: 'SELECT u.name FROM users u' }), [])
assert.deepEqual(diagnoseQuery({
  language: 'sql',
  query: 'SELECT * FROM "public"."farmland" LIMIT 10',
  fields: ['id', 'geom'],
  targetLocator: 'addp://engine/1/table?item_id=2'
}), [])

const multiTableQuery = `WITH railway_buffer AS (
  SELECT ST_Union(r.geom) AS geom
  FROM public.railway AS r
)
SELECT f.geometry, rb.geom AS clipped_geom
FROM public.farmland AS f
CROSS JOIN railway_buffer AS rb
WHERE f.geometry IS NOT NULL AND rb.geom IS NOT NULL`
const parsedSources = parseSQLSources(multiTableQuery)
assert.deepEqual(parsedSources.sources.map(source => ({ name: source.name, alias: source.alias, kind: source.kind, fields: source.fields })), [
  { name: 'public.railway', alias: 'r', kind: 'table', fields: [] },
  { name: 'public.farmland', alias: 'f', kind: 'table', fields: [] },
  { name: 'railway_buffer', alias: 'rb', kind: 'cte', fields: ['geom'] }
])
assert.deepEqual(diagnoseQuery({
  language: 'sql',
  engineType: 'postgresql',
  query: multiTableQuery,
  fieldSources: [
    { name: 'public.railway', alias: 'r', fields: ['geom'], known: true },
    { name: 'public.farmland', alias: 'f', fields: ['geometry'], known: true },
    { name: 'railway_buffer', alias: 'rb', fields: ['geom'], known: true }
  ]
}), [])
assert.deepEqual(diagnoseQuery({
  language: 'sql',
  query: 'SELECT f.missing FROM public.farmland AS f JOIN tmp_result AS tmp ON f.geometry = tmp.geometry',
  fieldSources: [
    { name: 'public.farmland', alias: 'f', fields: ['geometry'], known: true },
    { name: 'tmp_result', alias: 'tmp', fields: [], known: false }
  ]
}), [{ code: 'field_unknown', severity: 'warning', field: 'missing' }])
assert.deepEqual(diagnoseQuery({
  language: 'sql',
  query: 'SELECT f.geometry FROM public.farmland AS f',
  fieldSources: []
}), [])
const postgresqlQuotedFieldQuery = 'SELECT * FROM "public"."farmland" WHERE SmID > 10'
const postgresqlQuotedFieldStart = postgresqlQuotedFieldQuery.indexOf('SmID')
assert.deepEqual(diagnoseQuery({
  language: 'sql',
  engineType: 'postgresql',
  query: postgresqlQuotedFieldQuery,
  fields: ['SmID'],
  targetLocator: 'addp://engine/1/table?item_id=2'
}), [
  {
    code: 'field_requires_quote',
    severity: 'warning',
    field: 'SmID',
    suggested: '"SmID"',
    start: postgresqlQuotedFieldStart,
    end: postgresqlQuotedFieldStart + 4,
    replacement: '"SmID"'
  }
])
const reservedFieldQuery = 'SELECT user FROM users'
const reservedFieldStart = reservedFieldQuery.indexOf('user')
assert.deepEqual(diagnoseQuery({
  language: 'sql',
  engineType: 'mysql',
  query: reservedFieldQuery,
  fields: ['user'],
  targetLocator: 'addp://engine/1/table?item_id=2'
}), [
  {
    code: 'field_requires_quote',
    severity: 'warning',
    field: 'user',
    suggested: '`user`',
    start: reservedFieldStart,
    end: reservedFieldStart + 4,
    replacement: '`user`'
  }
])
assert.equal(isQueryInputResource({ itemId: 51657 }), true)
assert.equal(isQueryInputResource({ type: 'database', nodeId: 276 }), false)
assert.equal(mqlPrimaryCollection('{"find":"Persons","filter":{}}'), 'Persons')
assert.equal(mqlPrimaryCollection('{"aggregate":"Orders","pipeline":[]}'), 'Orders')
assert.equal(mqlPrimaryCollection('{"count":"Persons","query":{}}'), 'Persons')
assert.equal(mqlPrimaryCollection('{"distinct":"Persons","key":"name"}'), 'Persons')
assert.equal(mqlPrimaryCollection('db.Persons.find({})'), '')
assert.equal(mqlPrimaryCollection('{"find":"Persons","count":"Persons"}'), '')
assert.deepEqual(mqlCollectionReferences(JSON.stringify({
  aggregate: 'Outdoors',
  pipeline: [
    { $lookup: { from: 'Persons', localField: 'members.userInfo.nickName', foreignField: 'userInfo.nickName', as: 'persons' } },
    { $facet: { joined: [{ $unionWith: { coll: 'ArchivedOutdoors', pipeline: [] } }] } },
    { $graphLookup: { from: 'Groups', startWith: '$group', connectFromField: 'parent', connectToField: '_id', as: 'groups' } }
  ]
})), ['Outdoors', 'Persons', 'ArchivedOutdoors', 'Groups'])
const availableCollections = [
  { name: 'Outdoors', locator: 'outdoors-locator' },
  { name: 'Persons', locator: 'persons-locator' }
]
assert.deepEqual(matchMQLCollectionReferences(
  '{"aggregate":"Outdoors","pipeline":[{"$lookup":{"from":"Persons"}}]}',
  availableCollections
), {
  references: ['Outdoors', 'Persons'],
  matches: availableCollections,
  missing: []
})
assert.deepEqual(matchMQLCollectionReferences(
  '{"find":"outdoors","filter":{}}',
  availableCollections
), {
  references: ['outdoors'],
  matches: [],
  missing: ['outdoors']
})
assert.deepEqual(matchMQLCollectionReferences(
  '{"aggregate":"Outdoors","pipeline":[{"$unionWith":"ArchivedOutdoors"}]}',
  availableCollections
), {
  references: ['Outdoors', 'ArchivedOutdoors'],
  matches: [availableCollections[0]],
  missing: ['ArchivedOutdoors']
})
assert.deepEqual(diagnoseQuery({
  language: 'mql',
  query: '{"find":"Persons","filter":{"_id":"W71wut2AWotkbETX"},"limit":10}',
  fields: ['_id', 'name'],
  targetLocator: 'addp://engine/1/collection?item_id=3'
}), [])
assert.deepEqual(diagnoseQuery({
  language: 'mql',
  query: '{"find":"Persons","filter":{"missing":true},"limit":10}',
  fields: ['_id', 'name'],
  targetLocator: 'addp://engine/1/collection?item_id=3'
}), [{ code: 'field_unknown', severity: 'warning', field: 'missing' }])

const overlapPipeline = JSON.stringify({
  aggregate: 'Persons',
  pipeline: [
    {
      $facet: {
        left: [
          { $match: { 'userInfo.nickName': { $param: 'entity_1' } } },
          { $project: { _values: { $setUnion: ['$myOutdoors', '$entriedOutdoors'] } } },
          { $group: { _id: null, _value_sets: { $push: '$_values' } } },
          { $project: { _id: 0, values: '$_value_sets' } }
        ],
        right: [
          { $match: { 'userInfo.nickName': { $param: 'entity_2' } } },
          { $project: { _values: { $setUnion: ['$myOutdoors', '$entriedOutdoors'] } } },
          { $group: { _id: null, _value_sets: { $push: '$_values' } } },
          { $project: { _id: 0, values: '$_value_sets' } }
        ]
      }
    },
    { $project: { left: { $arrayElemAt: ['$left.values', 0] }, right: { $arrayElemAt: ['$right.values', 0] } } },
    {
      $project: {
        left_count: { $size: '$left' },
        right_count: { $size: '$right' },
        intersection_count: { $size: { $setIntersection: ['$left', '$right'] } }
      }
    },
    { $set: { _overlap_denominator: { $min: ['$left_count', '$right_count'] } } },
    {
      $project: {
        left_count: 1,
        right_count: 1,
        intersection_count: 1,
        overlap_coefficient: { $divide: ['$intersection_count', '$_overlap_denominator'] }
      }
    }
  ]
})
assert.deepEqual(diagnoseQuery({
  language: 'mql',
  engineType: 'mongodb',
  query: overlapPipeline,
  fields: ['userInfo.nickName', 'myOutdoors', 'entriedOutdoors'],
  targetLocator: 'addp://engine/1/collection?item_id=3'
}), [])
assert.deepEqual(diagnoseQuery({
  language: 'mql',
  engineType: 'mongodb',
  query: '{"aggregate":"Persons","pipeline":[{"$match":{"missing":true}},{"$project":{"derived":"$missing"}}]}',
  fields: ['userInfo.nickName'],
  targetLocator: 'addp://engine/1/collection?item_id=3'
}), [{ code: 'field_unknown', severity: 'warning', field: 'missing' }])

const result = queryResultFromExecution({
  execution_id: 'execution-1',
  status: 'success',
  progress: 100,
  execution_time_ms: 42,
  metadata: {
    result: {
      columns: ['id'], rows_count: 1, rows_affected: 1,
      result_kind: 'graph', result_limit: 500, truncated: true,
      summary: { preview_rows: [{ id: 1 }] },
      graph_data: { nodes: [], relationships: [] }
    }
  }
})
assert.deepEqual(result.rows, [{ id: 1 }])
assert.equal(result.execution_id, 'execution-1')
assert.equal(result.truncated, true)

assert.equal(
  buildQueryResultCSV(['name', 'payload'], [{ name: 'a,"b"\nline', payload: { ok: true } }]),
  'name,payload\r\n"a,""b""\nline","{""ok"":true}"'
)

console.log('query workbench tests passed')
