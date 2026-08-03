import assert from 'node:assert/strict'
import test from 'node:test'

import {
  applySQLExecutionEngine,
  federatedQueryRuntimes,
  queryServiceExecutionEngines,
  tableSelectionUsesRuntime
} from '../src/utils/queryServiceEngines.js'

const engines = [
  { id: 1, name: 'business-postgres', engine_type: 'postgresql', lifecycle_state: 'active', capabilities: { compute: { query: { supported: true, languages: ['sql'] } } } },
  { id: 2, name: 'DuckDB Runtime', engine_type: 'duckdb', lifecycle_state: 'active', is_builtin: true, capabilities: { compute: { query: { supported: true, languages: ['sql'], federation: { supported: true } } } } },
  { id: 3, name: 'old-runtime', engine_type: 'duckdb', lifecycle_state: 'deleting', capabilities: { compute: { query: { supported: true, languages: ['sql'], federation: { supported: true } } } } },
  { id: 4, name: 'object-store', engine_type: 'minio', lifecycle_state: 'active', capabilities: { storage: {} } },
  { id: 5, name: 'spark-sql', engine_type: 'spark_sql', lifecycle_state: 'active', capabilities: JSON.stringify({ compute: { query: { supported: true, languages: ['sql'] } } }) },
  { id: 6, name: 'mongo', engine_type: 'mongodb', lifecycle_state: 'active', capabilities: { compute: { query: { supported: true, languages: ['mql'] } } } }
]

test('uses only real active query engines and runtimes', () => {
  assert.deepEqual(queryServiceExecutionEngines(engines).map(engine => engine.id), [1, 2, 5])
  assert.deepEqual(federatedQueryRuntimes(engines).map(engine => engine.id), [2])
})

test('maps the selected real engine to exactly one SQL execution field', () => {
  const form = {}
  applySQLExecutionEngine(form, 2, engines)
  assert.deepEqual(form, { execution_engine_id: 2, engine_id: null, runtime_engine_id: 2 })

  applySQLExecutionEngine(form, 1, engines)
  assert.deepEqual(form, { execution_engine_id: 1, engine_id: 1, runtime_engine_id: null })
})

test('requires a runtime only for object table engines', () => {
  assert.equal(tableSelectionUsesRuntime({ display: { engine_type: 'minio' } }), true)
  assert.equal(tableSelectionUsesRuntime({ display: { engine_type: 'postgresql' } }), false)
})
