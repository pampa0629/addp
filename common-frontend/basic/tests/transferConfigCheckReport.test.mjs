import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { isTransferConfigCheckReport, projectTransferConfigCheckReport } from '../src/utils/transferConfigCheckReport.mjs'

const fixture = status => ({
  schema_version: 'addp.transfer_task_config_check/v1', status, observed_at: '2026-10-12T00:00:00Z',
  checked: ['provider_write_definition'], not_checked: ['business_rows'], runtime_checks_required: ['per_value_encoding'],
  diagnostics: [{ status, check: 'target_definition', code: 'field_definition_supported', config_path: '/config/target', source_field: 'members', physical_type: 'TEXT[]' }],
  principal_id: 'private-user', tenant_id: 7, binding_hash: 'private-binding', facts: [{ snapshot_hash: 'private-snapshot' }]
})

test('report projection preserves every tri-state fact without exposing identity or bindings', () => {
  for (const status of ['supported', 'unsupported', 'unknown']) {
    const source = fixture(status), report = projectTransferConfigCheckReport(source)
    assert.equal(report.status, status)
    assert.equal(isTransferConfigCheckReport(report), true)
    assert.deepEqual(report.diagnostics, source.diagnostics)
    assert.equal(JSON.stringify(report).includes('private-'), false)
    source.diagnostics[0].code = 'changed'
    assert.equal(report.diagnostics[0].code, 'field_definition_supported')
  }
})

test('invalid, over-limit and arbitrary presentation fields are rejected instead of becoming unknown', () => {
  const report = projectTransferConfigCheckReport(fixture('unknown'))
  for (const value of [null, {}, { ...report, status: 'passed' }, { ...report, principal_id: 'user' },
    { ...report, action: 'create' }, { ...report, url: '/private' },
    { ...report, checked: Array(129).fill('shape') }, { ...report, diagnostics: Array(513).fill(report.diagnostics[0]) },
    { ...report, diagnostics: [{ ...report.diagnostics[0], arbitrary: 'script' }] }]) {
    assert.equal(isTransferConfigCheckReport(value), false)
  }
  assert.equal(projectTransferConfigCheckReport({ status: 'unknown' }), null)
  const unicode = { ...report, diagnostics: [{ ...report.diagnostics[0], source_field: '😀'.repeat(4096) }] }
  assert.equal(isTransferConfigCheckReport(unicode), true)
  assert.equal(isTransferConfigCheckReport({ ...report, diagnostics: Array.from({ length: 512 }, () => ({ ...report.diagnostics[0], source_field: 'x'.repeat(4096) })) }), false)
})

test('wizard and A2UI only compose the single shared readonly renderer and translations', () => {
  const read = path => readFileSync(new URL(path, import.meta.url), 'utf8')
  const wizard = read('../../../transfer/frontend/src/views/TaskWizard/Step5Review.vue')
  const adapter = read('../../agent-ui/src/a2ui/components/TransferConfigCheckReport.vue')
  const renderer = read('../src/components/TransferConfigCheckReport.vue')
  assert.match(wizard, /<TransferConfigCheckReport/)
  assert.doesNotMatch(wizard, /checkStatusType|checkLabel|check-scope-grid/)
  assert.match(adapter, /basic\/src\/components\/TransferConfigCheckReport.vue/)
  assert.doesNotMatch(adapter, /dispatchAction|fetch\(|el-table/)
  assert.doesNotMatch(renderer, /v-html|@click|<el-button|fetch\(/)
  for (const lang of ['zh-cn', 'en']) {
    const shared = JSON.parse(read(`../src/i18n/${lang}.json`)).transferConfigCheck
    const module = JSON.parse(read(`../../../transfer/frontend/src/i18n/${lang}.json`)).transfer.taskWizard.configCheck
    assert.equal(Object.hasOwn(module, 'codes'), false)
    assert.equal(Object.hasOwn(module, 'status'), false)
    assert.ok(shared.status.unknown && shared.snapshotNotice && shared.codes.source_type_unknown)
  }
})
