import test from 'node:test'
import assert from 'node:assert/strict'
import { mkdtempSync, writeFileSync, readFileSync, rmSync, chmodSync, symlinkSync, statSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { spawnSync } from 'node:child_process'
import { navigationInput, navigationSQL, openNavigationLedger, validateNavigationLedger, runExecutionNavigation, navigationFailure } from '../../console/frontend/e2e/online/execution-navigation-local-support.mjs'

const input = () => ({ run_id: 'owned-navigation', tenant_id: '7', user_id: '9', engine_id: 13, query_concurrency: 20, query_per_engine_concurrency: 5, backend_instances: 1 })
const executionID = index => `11111111-1111-4111-8111-${String(index + 1).padStart(12, '0')}`
function fixture(t) {
  const directory = mkdtempSync(join(tmpdir(), 'addp-navigation-unit-'))
  t.after(() => rmSync(directory, { recursive: true, force: true }))
  const path = join(directory, 'navigation-ownership.json')
  const binding = input()
  const { query_concurrency, query_per_engine_concurrency, backend_instances, ...identity } = binding
  writeFileSync(path, JSON.stringify({ schema: 'addp.execution-navigation-ownership/v1', ...identity, definitions: [], executions: [], reads: 0 }), { mode: 0o600 })
  return { directory, path, binding, ledger: openNavigationLedger(path, binding) }
}
function scene(t, fault) {
  const f = fixture(t), tasks = [], executions = [], calls = [], windows = [], receipt = {}
  let clock = 0, connected = 0, disposed = 0
  const row = execution => ({ execution_id: execution.id, module: 'develop', task_type: 'query', source_task_id: String(execution.task), triggered_by: 9,
    attempt: clock < execution.start ? 0 : 1, started_at: clock < execution.start ? null : '2026-11-14T00:00:00Z', completed_at: clock >= execution.end ? '2026-11-14T00:00:20Z' : null,
    status: clock < execution.start ? 'pending' : clock < execution.end ? 'running' : 'success' })
  const connect = async () => {
    connected++
    const api = async (method, path, body, timeout) => {
      calls.push({ method, path, body, timeout })
      assert(timeout > 0 && timeout <= 5000)
      if (method === 'POST' && path === '/api/v1/develop/task-definitions') {
        if (fault === 'unknown-definition') throw new Error('Bearer private response')
        assert.equal(body.content.query, navigationSQL); assert.equal(body.timeout, 30)
        const task = { ...body, id: 100 + tasks.length, tenant_id: 7, created_by: 9 }
        tasks.push(task); return task
      }
      if (method === 'POST' && path.endsWith('/execute')) {
        if (fault === 'unknown-execution') throw new Error('private accepted command')
        const index = executions.length, task = Number(path.split('/').at(-2))
        const start = index === 6 ? 40000 : clock
        const execution = { id: executionID(index), task, start, end: start + 20000 }
        executions.push(execution); return { execution_id: execution.id }
      }
      if (path.includes('/executions/')) {
        const execution = executions.find(row => row.id === path.split('/').at(-1)); assert(execution)
        const result = row(execution)
        if (fault === 'wrong-source') result.source_task_id = '999'
        if (fault === 'wrong-user') result.triggered_by = 33
        if (fault === 'private-field') result.metadata = { nested: { lease_token: 'private' } }
        if (fault === 'invented-start') result.completed_at = '2026-11-14T00:00:00Z'
        if (fault === 'old-changed' && executions.length === 7 && execution === executions[0]) result.completed_at = '2026-11-14T00:00:21Z'
        return result
      }
      if (path.includes('/task-definitions/')) {
        const task = tasks.find(task => task.id === Number(path.split('/').at(-1)))
        const current = executions.filter(row => row.task === task.id).at(-1)
        return { ...task, last_execution_id: fault === 'old-latest' ? executions[0].id : current.id, last_execution_status: row(current).status }
      }
      assert.fail('unregistered control request')
    }
    api.dispose = async () => { disposed++ }; return api
  }
  const run = () => runExecutionNavigation({ input: f.binding, ledger: f.ledger, connect, receipt,
    remaining: () => fault === 'budget' ? 129999 : 240000 - clock,
    now: () => clock, wait: async ms => { clock += ms }, stage: () => {}, navigate: async value => {
      windows.push(value.state)
      assert.equal(value.id, executionID(6)); assert.equal(value.old, executionID(0))
      assert(value.deadline <= clock + (value.state === 'success' ? 30000 : 15000))
      clock += fault === 'window-lost' ? 25000 : 500
    } })
  return { ...f, calls, tasks, executions, windows, receipt, run, counts: () => ({ connected, disposed }) }
}

test('fixed input rejects a changed policy, identity, extra purpose or runtime count', () => {
  assert.equal(navigationInput(input()).engine_id, 13)
  for (const change of [{ backend_instances: 2 }, { query_concurrency: 1 }, { query_per_engine_concurrency: 6 }, { tenant_id: 'other' }, { user_id: '' }, { permission: 'admin' }, { engine_id: true }])
    assert.throws(() => navigationInput({ ...input(), ...change }))
})
test('seven admissions cover real-order state observations without retries or replacing the old success', async t => {
  const f = scene(t); await f.run()
  assert.deepEqual(f.windows, ['pending', 'running', 'success'])
  assert.deepEqual(f.receipt.states, { pending: true, running: true, success: true })
  assert.equal(f.receipt.task_id, 100); assert.equal(f.receipt.previous_execution_id, executionID(0)); assert.equal(f.receipt.execution_id, executionID(6))
  assert.equal(f.receipt.result, 'passed'); assert.equal(f.tasks.length, 6); assert.equal(f.executions.length, 7)
  assert(f.receipt.polls <= 45); assert(f.receipt.comparisons <= 12)
  assert.equal(f.counts().connected, f.counts().disposed)
  const ledger = JSON.parse(readFileSync(f.path))
  assert.equal(ledger.reads, f.receipt.polls); assert.equal(ledger.definitions.length, 6); assert.equal(ledger.executions.length, 7)
  assert.equal(statSync(f.path).mode & 0o777, 0o600)
  assert(!/SELECT|Bearer|password|token|response/.test(readFileSync(f.path, 'utf8')))
  assert(readFileSync(f.path).length <= 4096)
})
for (const fault of ['unknown-definition', 'unknown-execution']) test(`${fault} preserves an unknown intent and never retries a mutation`, async t => {
  const f = scene(t, fault); await assert.rejects(f.run(), /navigation_control_request_failed/)
  const ledger = JSON.parse(readFileSync(f.path))
  assert.equal((fault === 'unknown-definition' ? ledger.definitions : ledger.executions).at(-1).id, null)
  assert.equal(f.calls.filter(call => call.method === 'POST' && (fault === 'unknown-definition' ? !call.path.endsWith('/execute') : call.path.endsWith('/execute'))).length, 1)
  assert.equal(f.receipt.result, undefined)
})
for (const fault of ['wrong-source', 'wrong-user', 'private-field', 'invented-start', 'old-latest']) test(`rejects ${fault} without exporting response text`, async t => {
  const f = scene(t, fault); await assert.rejects(f.run(), /navigation_/)
  assert.equal(f.receipt.result, undefined)
  assert(!JSON.stringify(f.receipt).includes('private'))
})
test('insufficient original browser budget makes no connection or intent', async t => {
  const f = scene(t, 'budget'); await assert.rejects(f.run(), /budget_insufficient/)
  assert.equal(f.calls.length, 0); assert.equal(f.ledger.value.definitions.length, 0); assert.equal(f.counts().connected, 0)
})
test('a state transition during navigation remains the first failure and never counts later success', async t => {
  const f = scene(t, 'window-lost'); await assert.rejects(f.run(), /state_window_lost/)
  assert.deepEqual(f.windows, ['pending']); assert.deepEqual(f.receipt.states, {})
  assert.equal(f.executions.length, 7); assert.equal(f.receipt.result, undefined)
})
test('ledger is purpose-bound, limited, and cannot alias another definition or execution', t => {
  const f = fixture(t), value = structuredClone(f.ledger.value)
  for (const mutate of [v => { v.tenant_id = '8' }, v => { v.engine_id++ }, v => { v.extra = 'secret' }, v => { v.reads = 46 }, v => { v.definitions = [{}] }, v => { v.executions = [{}] }]) {
    const wrong = structuredClone(value); mutate(wrong); assert.throws(() => validateNavigationLedger(wrong, f.binding))
  }
  const first = f.ledger.definition(0); first.id = 37; f.ledger.save()
  const second = f.ledger.definition(1); second.id = 37
  assert.throws(() => validateNavigationLedger(f.ledger.value, f.binding), /duplicate/)
})
test('ledger rejects symlink, public permissions, oversize, and replay of a started file', t => {
  const f = fixture(t), alias = join(f.directory, 'alias'); symlinkSync(f.path, alias)
  assert.throws(() => openNavigationLedger(alias, f.binding))
  chmodSync(f.path, 0o644); assert.throws(() => openNavigationLedger(f.path, f.binding), /file_invalid/)
  chmodSync(f.path, 0o600); writeFileSync(f.path, ' '.repeat(4097)); assert.throws(() => openNavigationLedger(f.path, f.binding), /file_invalid/)
  f.ledger.definition(0); assert.throws(() => openNavigationLedger(f.path, f.binding), /already_started/)
})
test('an abruptly exiting process leaves the reserved unknown intent durably available', t => {
  const f = fixture(t)
  const module = new URL('../../console/frontend/e2e/online/execution-navigation-local-support.mjs', import.meta.url).href
  const result = spawnSync(process.execPath, ['--input-type=module', '-e', `import {openNavigationLedger} from ${JSON.stringify(module)};const ledger=openNavigationLedger(${JSON.stringify(f.path)},${JSON.stringify(f.binding)});ledger.definition(0);process.exit(17)`])
  assert.equal(result.status, 17); assert.equal(JSON.parse(readFileSync(f.path)).definitions[0].id, null)
})


test('navigation diagnostics export only closed failure classes, never arbitrary error text', () => {
  for (const code of ['navigation_state_window_lost', 'timeout', 'filesystem_error', 'assertion_failed'])
    assert.equal(navigationFailure({ message: code }), code)
  assert.equal(navigationFailure(new Error('navigation_PRIVATE_token')), 'operation_failed')
  assert.equal(navigationFailure({ name: 'TimeoutError', message: 'PRIVATE' }), 'timeout')
})


test('an older success must keep its original terminal times and result after the three windows', async t => {
  const f = scene(t, 'old-changed'); await assert.rejects(f.run(), /navigation_old_terminal_changed/)
  assert.equal(f.receipt.result, undefined); assert.equal(f.executions.length, 7)
})
