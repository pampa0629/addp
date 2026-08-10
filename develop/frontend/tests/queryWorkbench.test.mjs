import assert from 'node:assert/strict'
import {
  buildQueryExecutionContract,
  buildQueryResultCSV,
  formatterLanguageForQuery,
  monacoLanguageForQuery,
  queryCapabilityForEngine,
  queryParameterReference,
  queryErrorMessage,
  queryResultFromExecution,
  diagnoseQuery,
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
  }
})
assert.equal(queryParameterReference('sql', 'status'), ':status')
assert.equal(queryParameterReference('cypher', 'status'), '$status')
assert.equal(queryParameterReference('mql', 'status'), '{"$param":"status"}')
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
assert.equal(formatterLanguageForQuery('cypher'), '')
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
assert.deepEqual(diagnoseQuery({
  language: 'mql',
  query: '{"Members":{"status":"lead"}}',
  fields: ['Members'],
  targetLocator: 'addp://engine/1/collection?item_id=3'
}), [])

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
