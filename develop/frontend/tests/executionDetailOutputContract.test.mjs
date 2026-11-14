import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'

const source = readFileSync(
  fileURLToPath(new URL('../src/views/ExecutionDetail.vue', import.meta.url)),
  'utf8'
)

assert.match(source, /Object\.entries\(execution\.value\?\.outputs \|\| \{\}\)/)
assert.doesNotMatch(source, /result\.outputs/)

console.log('execution detail output contract tests passed')

const { workflowProfessionalResult, workflowNumericColumns, workflowColumnSum } = await import('../src/utils/workflowProfessionalResult.mjs')
const table = { schema_version: 'addp.workflow.table-result/v1', columns: ['instance_id', 'area', 'label'], row_count: 3,
  rows: [{ instance_id: 1, area: 1e16, label: 'a' }, { instance_id: 2, area: 1, label: 'b' }, { instance_id: 3, area: -1e16, label: 'c' }] }
const current = { execution_id: 'own', task_type: 'workflow', status: 'success', metadata: { result: {
  final_result: table, professional_result_expires_at: '2026-11-14T12:00:00Z'
} } }
const result = workflowProfessionalResult(current, 'own')
assert.equal(result.rows_count, 3)
assert.equal(result.plaintext_expires_at, '2026-11-14T12:00:00Z')
// Unsafe integer magnitudes are not eligible for a silently rounded total.
assert.deepEqual(workflowNumericColumns(result), ['instance_id'])
assert.equal(workflowColumnSum(result, 'instance_id'), 6)
assert.equal(workflowColumnSum(result, 'label'), null)
const decimals = { columns: ['surface', 'empty', 'text', 'infinite'], rows: [
  { surface: 819.0184107347761, empty: null, text: '42', infinite: Infinity },
  { surface: 1151.1167000021796, empty: 4, text: '43', infinite: 1 }
] }
assert.deepEqual(workflowNumericColumns(decimals), ['surface'])
assert.equal(workflowColumnSum(decimals, 'surface'), 1970.1351107369557)
assert.equal(workflowColumnSum({ columns: ['x'], rows: [{ x: 1e15 }, { x: 0.1 }, { x: -1e15 }] }, 'x'), 0.1)
assert.equal(workflowColumnSum(null, 'area'), null)
assert.deepEqual(workflowNumericColumns({ columns: ['x'], rows: [] }), [])
for (const changed of [
  { ...current, execution_id: 'other' }, { ...current, task_type: 'query' }, { ...current, status: 'running' },
  { ...current, metadata: { result: { final_result: { runtime_type: 'dataframe', row_count: 50 } } } },
  { ...current, metadata: { result: { ...current.metadata.result, final_result: { ...table, row_count: 4 } } } }
]) assert.throws(() => workflowProfessionalResult(changed, 'own'), /workflow_result_unavailable/)
assert.match(source, /QueryResult :result="professionalResult"/)
assert.match(source, /createQueryPlaintextRefresh/)
assert.match(source, /professionalRequests\.current\(ticket\)/)
assert.match(source, /professionalRefresh\.dispose\(\)/)
assert.doesNotMatch(source, /createObjectURL|text\/csv|\.download\s*=/)
console.log('workflow professional table presentation and numeric sum tests passed')
