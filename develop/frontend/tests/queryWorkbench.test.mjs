import assert from 'node:assert/strict'
import {
  buildQueryExecutionContract,
  formatGeneratedQueryForEditor,
  formatMQLQuery,
  formatterLanguageForQuery,
  monacoLanguageForQuery,
  nativeCatalogPathText,
  nativeCatalogSegmentText,
  queryCapabilityForEngine,
  queryParameterReference,
  queryErrorMessage,
  queryResultFromExecution,
  extractQueryParameterReferences,
  mqlCollectionReferences,
  matchMQLCollectionReferences,
  isQueryInputResource,
  mqlPrimaryCollection
} from '../src/utils/queryWorkbench.mjs'

const capability = queryCapabilityForEngine({
  capabilities: JSON.stringify({
    compute: {
      query: {
        supported: true,
        languages: ['Cypher', 'cypher'],
        default_language: 'cypher',
        identifier_quotes: { cypher: '`', sql: '"' },
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
  identifierQuotes: { cypher: '`' },
  resultKinds: ['graph', 'table'],
  parameters: {
    supported: true,
    languages: ['cypher'],
    types: ['string', 'integer', 'number', 'boolean']
  },
  federation: { supported: false, sourceEngineTypes: [], objectFormats: [] }
})
assert.equal(nativeCatalogSegmentText('Order`Item', capability, 'cypher'), '`Order``Item`')
assert.equal(nativeCatalogPathText(['business', 'orders'], { identifierQuotes: { sql: '`' } }, 'sql'), '`business`.`orders`')
assert.equal(nativeCatalogPathText(['business', 'orders'], { identifierQuotes: {} }, 'sql'), 'business.orders')
assert.equal(nativeCatalogPathText(['business', 'orders'], capability, 'mql'), '"orders"')
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
  { name: 'status', type: 'string', default: 'active' },
  { name: 'limit', type: 'integer', default: 10 },
  { name: 'include_archived', type: 'boolean', default: false },
  { name: 'keyword', type: 'string', default: '' },
  { name: 'offset', type: 'integer' },
  { name: 'members', type: 'relation', default: { locator: 'addp://engine/12/path/public/members?type=table' } },
  { name: 'activities', type: 'relation' }
], { engineId: 12 })
assert.equal(parameterContract.input_schema.properties.status.type, 'string')
assert.equal(parameterContract.input_defaults.limit, 10)
assert.equal(parameterContract.input_defaults.include_archived, false)
assert.equal(parameterContract.input_defaults.keyword, '')
assert.equal(parameterContract.input_ui_schema.status.order, 0)
assert.deepEqual(parameterContract.input_defaults.members, { locator: 'addp://engine/12/path/public/members?type=table' })
assert.deepEqual(parameterContract.input_schema.required, ['offset', 'activities'])
assert.equal(parameterContract.input_schema.properties.members.properties.locator.format, 'resource-locator')
assert.equal(parameterContract.input_ui_schema.activities.engine_id, 12)
assert.equal(parameterContract.input_ui_schema.members.control, 'resource_tree_picker')
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
const result = queryResultFromExecution({
  execution_id: 'execution-1',
  status: 'success',
  progress: 100,
  execution_time_ms: 42,
  metadata: {
    result: {
      columns: ['id'], rows_count: 1, rows_affected: 1, effect: 'read',
      result_kind: 'graph', result_limit: 500, truncated: true,
      summary: { preview_rows: [{ id: 1 }] },
      graph_data: { nodes: [], relationships: [] }
    }
  }
})
assert.deepEqual(result.rows, [{ id: 1 }])
assert.equal(result.execution_id, 'execution-1')
assert.equal(result.truncated, true)
assert.equal(result.effect, 'read')

console.log('query workbench tests passed')
