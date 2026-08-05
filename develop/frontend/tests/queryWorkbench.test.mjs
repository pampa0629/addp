import assert from 'node:assert/strict'
import {
  buildQueryResultCSV,
  formatterLanguageForQuery,
  monacoLanguageForQuery,
  queryCapabilityForEngine,
  queryResultFromExecution
} from '../src/utils/queryWorkbench.mjs'

const capability = queryCapabilityForEngine({
  capabilities: JSON.stringify({
    compute: {
      query: {
        supported: true,
        languages: ['Cypher', 'cypher'],
        default_language: 'cypher',
        result_kinds: ['graph', 'table']
      }
    }
  })
})
assert.deepEqual(capability, {
  languages: ['cypher'],
  defaultLanguage: 'cypher',
  resultKinds: ['graph', 'table']
})
assert.equal(monacoLanguageForQuery('mql'), 'json')
assert.equal(monacoLanguageForQuery('cypher'), 'cypher')
assert.equal(formatterLanguageForQuery('sql'), 'sql')
assert.equal(formatterLanguageForQuery('cypher'), '')

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
