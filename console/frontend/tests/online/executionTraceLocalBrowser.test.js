import { readFileSync, writeFileSync, mkdtempSync, rmSync, chmodSync, symlinkSync, statSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { spawnSync } from 'node:child_process'
import { expect, it, vi, beforeEach, afterEach } from 'vitest'
import { admitExecutionTraceBrowserEnvironment, executionTraceBrowserInput, executionTraceTOTP, tenantStageBrowserInput, tenantStagePermissions, causalLinkDiagnostics, writeExecutionTraceFirstFailure } from '../../e2e/online/execution-trace-local-support.mjs'

const now = Date.parse('2026-10-23T06:00:00Z')
const fixture = () => ({ schema: 'addp.execution-trace-browser/v1', run_id: 'local-trace-1', module_name: 'orchestrator', instance_id: 'owned-orchestrator', runtime_instance_id: 2, execution_id: '11111111-1111-4111-8111-111111111111', trace_id: '1'.repeat(32), span_id: '2'.repeat(16), started_at: '2026-10-23T05:59:00Z', start: '2026-10-23T05:58:59Z', end: '2026-10-23T05:59:01Z', links: [{ trace_id: '3'.repeat(32), span_id: '4'.repeat(16), module_name: 'orchestrator', instance_id: 'owned-orchestrator', role: 'backend', runtime_instance_id: 2, started_at: '2026-10-23T03:00:00Z' }] })
const environment = () => ({ ADDP_EXECUTION_TRACE_LOCAL: '1', CONSOLE_URL: 'http://nginx/', ADDP_EXECUTION_TRACE_ADMIN_USERNAME: 'synthetic-admin', ADDP_EXECUTION_TRACE_ADMIN_PASSWORD: 'synthetic-password', ADDP_EXECUTION_TRACE_ADMIN_TOTP: 'GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ', ADDP_EXECUTION_TRACE_USER_USERNAME: 'synthetic-user', ADDP_EXECUTION_TRACE_USER_PASSWORD: 'synthetic-password' })

it('accepts only local admitted identities and never a Hosted/SGM authorization substitute', () => {
  expect(() => admitExecutionTraceBrowserEnvironment(environment())).not.toThrow()
  for (const key of ['GITHUB_ACTIONS', 'ADDP_ONLINE_HOSTED', 'ADDP_ONLINE_OWNER_MANAGED', 'ADDP_ONLINE_LOCAL_SGM']) expect(() => admitExecutionTraceBrowserEnvironment({ ...environment(), [key]: '1' })).toThrow()
  for (const value of ['0', '', undefined]) expect(() => admitExecutionTraceBrowserEnvironment({ ...environment(), ADDP_EXECUTION_TRACE_LOCAL: value })).toThrow()
  for (const CONSOLE_URL of ['https://name:password@nginx/', 'http://example.com/', 'http://127.0.0.1/', 'http://nginx:8080/', 'https://nginx/']) expect(() => admitExecutionTraceBrowserEnvironment({ ...environment(), CONSOLE_URL })).toThrow()
})
it('derives one exact Monitor query from only independently observed public references', () => {
  const input = executionTraceBrowserInput(fixture(), now), url = new URL(input.trace_path, 'http://console')
  expect(url.pathname).toBe('/monitor/traces')
  expect(url.searchParams.get('instance_id')).toBe('2')
  expect(url.searchParams.get('trace_id')).toBe('1'.repeat(32))
  expect(url.searchParams.get('start')).toBe(fixture().start)
  for (const mutate of [
    value => { value.access_token = 'private' },
    value => { value.password = 'private' },
    value => { value.module_name = 'system' },
    value => { value.runtime_instance_id = 0 },
    value => { value.span_id = '0'.repeat(16) },
    value => { value.end = '2026-10-23T07:00:00Z' },
    value => { value.start = '2026-10-23T01:00:00Z' },
    value => { value.links = [] },
    value => { value.links[0].runtime_instance_id = 0 },
    value => { value.links[0].trace_id = value.trace_id },
    value => { value.links.push(value.links[0]) }
  ]) { const input = fixture(); mutate(input); expect(() => executionTraceBrowserInput(input, now)).toThrow() }
})
it('uses RFC 6238 current-time TOTP without changing clocks or replaying API phase codes', () => {
  const secret = 'GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ'
  expect(executionTraceTOTP(secret, 59000)).toBe('287082')
  expect(executionTraceTOTP(secret, 1111111109000)).toBe('081804')
  expect(executionTraceTOTP(secret, 2000000000000)).toBe('279037')
  expect(() => executionTraceTOTP('invalid')).toThrow()
})
it('keeps browser ownership with real UI and public responses, with no token injection or captures', () => {
  const source = readFileSync(new URL('../../e2e/online/execution-trace-local.spec.js', import.meta.url), 'utf8')
  const protocol = readFileSync(new URL('../../e2e/online/local-identity-support.mjs', import.meta.url), 'utf8')
  const combined = source + protocol
  expect(source).toContain("test.use({ screenshot: 'off', trace: 'off', video: 'off'")
  for (const forbidden of ['page.route(', 'context.route(', 'storageState', 'localStorage', 'setExtraHTTPHeaders', 'Bearer ', 'request.newContext', '.screenshot(', '.tracing.']) expect(combined).not.toContain(forbidden)
  for (const required of ['autocomplete="current-password"', 'autocomplete="one-time-code"', '/api/v1/system/auth/context', "getByTestId('trace-spans')", "getByTestId('trace-links')", "getByTestId('runtime-log-output')", "body.msg === 'execution.checkpoint'", "flag: 'wx'", "[200, 404].includes"]) expect(combined).toContain(required)
  expect(source).not.toMatch(/\.post\(|\.put\(|\.patch\(|\.delete\(/)
  expect(source).toContain("report.assertions.link_instance_currently_readable = true")
})

const tenantFixture = () => ({ schema: 'addp.tenant-stage-browser/v1', run_id: 'local-tenant-1', tenant_id: '7', user_id: '9',
  navigation: { engine_id: 13, query_concurrency: 20, query_per_engine_concurrency: 5, backend_instances: 1 },
  cases: ['orchestrator', 'develop'].flatMap((module, i) => ['readable', 'missing'].map(kind => ({ module, kind,
    execution_id: `${i ? '22222222' : '11111111'}-1111-4111-8111-111111111111`, stages: [
      { slot: 'enqueue', state: 'not_found' },
      { slot: 'latest', state: 'available', phase: 'terminal', attempt: 1, outcome: 'failed', start: '2026-11-13T00:00:00Z', end: '2026-11-13T00:00:00Z', duration_ms: 0 }
    ] }))) })

it('admits four safe tenant cases without accepting platform references or credentials', () => {
  expect(tenantStageBrowserInput(tenantFixture()).cases).toHaveLength(4)
  expect(tenantStagePermissions).toHaveLength(11)
  for (const mutate of [v => { v.trace_id = '1'.repeat(32) }, v => { v.password = 'secret' },
    v => { v.cases[0].stages[1].instance_id = 'private' }, v => { v.cases[0].execution_id = 'bad' },
    v => { v.cases[0].stages[1].attempt = 0 }, v => { v.cases[0].stages[1].duration_ms = Infinity },
    v => { v.cases[1].stages[0].state = 'not_recorded' }, v => { v.cases.push(v.cases[0]) },
    v => { v.cases[1] = v.cases[0] }, v => { v.cases[0].stages[1].outcome = 'unknown' }
  ]) { const candidate = tenantFixture(); mutate(candidate); expect(() => tenantStageBrowserInput(candidate)).toThrow() }
})
it('runs tenant and platform in separate real contexts with four lazy reads and logout', () => {
  const source = readFileSync(new URL('../../e2e/online/execution-trace-local.spec.js', import.meta.url), 'utf8')
  for (const marker of ["principal: 'USER', scope: 'tenant'", 'tenantStagePermissions', 'tenantContext.close()',
    "expect(stageRequests).toBe(4)", "name: '查看阶段追踪'", "name: '收起阶段追踪'", "'cache-control'", 'tenant-browser-logout']) expect(source).toContain(marker)
})


const captured = vi.hoisted(() => ({ body: null }))
vi.mock('@playwright/test', async () => {
  const { expect } = await vi.importActual('@playwright/test')
  const test = (_name, body) => { captured.body = body }
  test.use = () => {}
  test.setTimeout = value => { captured.timeout = value }
  return { request: { newContext: vi.fn() }, test, expect: value => value?.locator ? {
    toContainText: async () => { if (value.fail === 'text') expect(false).toBe(true) },
    toHaveCount: async () => { if (value.fail === 'count') expect(false).toBe(true) },
    toBeVisible: async () => { if (value.fail === 'visible') expect(false).toBe(true) }
  } : expect(value) }
})
vi.mock('../../e2e/online/local-identity-support.mjs', async importOriginal => ({
  ...await importOriginal(),
  localConsoleLogin: async (_page, _expect, options) => { options.beforeMFA(); return { principal: { id: 1 } } }
}))
await import('../../e2e/online/execution-trace-local.spec.js')
let directory
beforeEach(() => {
  directory = mkdtempSync(join(tmpdir(), 'trace-browser-diagnostic-')); vi.setSystemTime(now)
  // Only this synthetic worker supplies the local profile; real admission remains strict.
  for (const key of ['GITHUB_ACTIONS', 'ADDP_ONLINE_HOSTED', 'ADDP_ONLINE_OWNER_MANAGED', 'ADDP_ONLINE_LOCAL_SGM']) vi.stubEnv(key, undefined)
})
afterEach(() => { vi.unstubAllEnvs(); vi.useRealTimers(); rmSync(directory, { recursive: true, force: true }) })

function browserCase(failure, status = 200) {
  const input = fixture(), link = input.links[0]
  const inputPath = join(directory, 'input.json'), reportPath = join(directory, 'report.json'), failurePath = join(directory, 'first.json')
  writeFileSync(inputPath, JSON.stringify(input), { mode: 0o600 }); writeFileSync(failurePath, '', { mode: 0o600 })
  for (const [key, value] of Object.entries({ ...environment(), ADDP_EXECUTION_TRACE_BROWSER_CONFIG_FILE: inputPath,
    ADDP_EXECUTION_TRACE_BROWSER_REPORT_FILE: reportPath, ADDP_EXECUTION_TRACE_BROWSER_FIRST_FAILURE_FILE: failurePath })) vi.stubEnv(key, value)
  const privateError = Object.assign(new Error('Bearer SECRET password=PRIVATE https://private.invalid/?token=PRIVATE'), { name: 'TimeoutError' })
  const response = (kind, code, body, url) => ({ status: () => code, url: () => url,
    json: async () => { if (failure === kind + '-body') throw new SyntaxError('PRIVATE RESPONSE'); return body } })
  const initial = response('initial', 200, { data: [{ trace_id: input.trace_id, spans: [{ span_id: input.span_id,
    scope: 'addp.execution', phase: 'terminal', outcome: 'failed', failed: true, attempt: 1,
    module_name: input.module_name, instance_id: input.instance_id, links: failure === 'link-binding' ? [] : input.links }] }] })
  const instances = response('instance', failure === 'instance-status' ? 503 : 200, { data: failure === 'instance-identity' ? [] : [{ id: link.runtime_instance_id, instance_id: link.instance_id, module_name: link.module_name, role: link.role }] })
  const url = new URL('http://nginx/api/v1/monitor/platform/traces/' + link.trace_id)
  url.search = new URLSearchParams({ instance_id: failure === 'trace-query-instance' ? '999' : String(link.runtime_instance_id),
    start: new Date(Date.parse(link.started_at) - (failure === 'trace-query-start' ? 2000 : 1000)).toISOString(),
    end: new Date(Date.parse(link.started_at) + (failure === 'trace-query-end' ? 2000 : 1000)).toISOString() })
  const trace = response('trace', failure === 'trace-status' ? 503 : status,
    status === 404 ? { error_code: failure === 'trace-not-found-code' ? 'wrong' : 'trace_not_found' }
      : { data: [{ trace_id: failure === 'trace-identity' ? '0'.repeat(32) : link.trace_id }] }, url.href)
  let waiter = 0, clicks = 0
  const monitor = { getByTestId: id => ({ locator: true,
    fail: id === 'trace-error' && failure === 'trace-missing-ui' ? 'text' : id === 'trace-spans' && failure === 'trace-missing-cleared' ? 'count'
      : id === 'trace-spans' && failure === 'trace-present-ui' ? 'visible' : id === 'trace-error' && failure === 'trace-error-cleared' ? 'count' : null,
    getByRole() { return this }, filter() { return this }, first() { return this },
    click: async () => { clicks++; if (failure === 'link-click') throw privateError }
  }) }
  const page = { frameLocator: () => monitor, goto: async () => { throw new Error('stop after causal assertions') },
    waitForResponse: predicate => {
      const index = waiter++, value = [initial, instances, trace][index]
      const requestURL = index === 0 ? 'http://nginx/api/v1/monitor/platform/traces/' + input.trace_id
        : index === 1 ? 'http://nginx/api/v1/system/platform/module-instances?ids=' + link.runtime_instance_id : url.href
      expect(predicate({ url: () => requestURL })).toBe(true)
      return failure === (index === 1 ? 'instance-response' : index === 2 ? 'trace-response' : '') ? Promise.reject(privateError) : Promise.resolve(value)
    }
  }
  return { page, failurePath, reportPath, counts: () => ({ waiter, clicks }) }
}

it.each(['link-binding', 'link-click', 'instance-response', 'instance-status', 'instance-body', 'instance-identity',
  'trace-response', 'trace-status', 'trace-query-instance', 'trace-query-start', 'trace-query-end', 'trace-body', 'trace-identity',
  'trace-not-found-code', 'trace-missing-ui', 'trace-missing-cleared', 'trace-present-ui', 'trace-error-cleared'])(
  'records the real browser causal operation failure at %s without leaking the error or adding calls', async failure => {
    const item = browserCase(failure, ['trace-not-found-code', 'trace-missing-ui', 'trace-missing-cleared'].includes(failure) ? 404 : 200)
    await expect(captured.body({ page: item.page })).rejects.toThrow('failed at stage causal-link')
    const raw = readFileSync(item.failurePath, 'utf8'), report = JSON.parse(raw)
    expect(report.causal_link.substep).toBe(failure)
    expect(report.causal_link.error_category).toBe(['link-click', 'instance-response', 'trace-response'].includes(failure) ? 'timeout'
      : failure.endsWith('-body') ? 'invalid_json' : 'assertion_failed')
    expect(report.result).toBe('failed'); expect(raw.length).toBeLessThanOrEqual(2048)
    for (const secret of ['SECRET', 'PRIVATE', 'private.invalid', fixture().trace_id]) expect(raw).not.toContain(secret)
    expect(item.counts()).toEqual({ waiter: failure === 'link-binding' ? 1 : 3, clicks: failure === 'link-binding' ? 0 : 1 })
    expect(captured.timeout).toBe(240000)
    if (failure === 'instance-status') expect(report.causal_link.instance_http_status).toBe(503)
    if (failure === 'trace-status') expect(report.causal_link.trace_http_status).toBe(503)
  })

it.each([200, 404])('keeps the original retained/missing causal success checks for HTTP %s', async status => {
  const item = browserCase(null, status)
  await expect(captured.body({ page: item.page })).rejects.toThrow('failed at stage checkpoint-log')
  const report = JSON.parse(readFileSync(item.reportPath))
  expect(report.causal_link.substep).toBe('complete'); expect(report.causal_link.first_failure).toBeNull()
  expect(report.link_result).toBe(status === 200 ? 'retained' : 'not_found')
  expect(report.assertions.link_instance_currently_readable).toBe(true)
  expect(item.counts()).toEqual({ waiter: 3, clicks: 1 })
})

it('freezes and writes the first rejection immediately, before any later await or cleanup', () => {
  const path = join(directory, 'first.json'); writeFileSync(path, '', { mode: 0o600 })
  const report = { run_id: 'local-trace-1', stage: 'causal-link' }
  const causal = causalLinkDiagnostics(() => writeExecutionTraceFirstFailure(path, report)); report.causal_link = causal.receipt
  causal.response('instance', 200); causal.step('link-click'); causal.fail({ name: 'TimeoutError', message: 'SECRET' }, 'trace-response')
  const first = readFileSync(path, 'utf8')
  expect(JSON.parse(first).causal_link).toEqual({ substep: 'trace-response', error_category: 'timeout', instance_http_status: 200, trace_http_status: null })
  causal.response('trace', 503); causal.step('trace-status'); causal.fail(new SyntaxError('PRIVATE'))
  expect(readFileSync(path, 'utf8')).toBe(first)
  expect(() => writeExecutionTraceFirstFailure(path, report)).toThrow('file_invalid')
  expect(readFileSync(path, 'utf8')).toBe(first)
})

it('keeps the primary failure when persistence fails and never serializes arbitrary exception fields', () => {
  const causal = causalLinkDiagnostics(() => { throw new Error('PRIVATE PATH') })
  expect(() => causal.fail({ message: 'SECRET', stack: 'PRIVATE', code: 'TOKEN' })).not.toThrow()
  expect(causal.receipt.first_failure.error_category).toBe('operation_failed')
  expect(causal.receipt.persistence).toBe('filesystem_error')
  expect(JSON.stringify(causal.receipt)).not.toMatch(/SECRET|PRIVATE|TOKEN/)
})

it('requires one empty private non-symlink evidence file', () => {
  const target = join(directory, 'first.json'); writeFileSync(target, '', { mode: 0o600 })
  expect(statSync(target).mode & 0o777).toBe(0o600)
  symlinkSync(target, join(directory, 'alias.json'))
  expect(() => writeExecutionTraceFirstFailure(join(directory, 'alias.json'), {})).toThrow()
  chmodSync(target, 0o644)
  expect(() => writeExecutionTraceFirstFailure(target, {})).toThrow('file_invalid')
  expect(readFileSync(target, 'utf8')).toBe('')
})

it('retains a durable first failure after abrupt child exit without a final browser report', () => {
  const path = join(directory, 'first.json'); writeFileSync(path, '', { mode: 0o600 })
  const moduleURL = new URL('../../e2e/online/execution-trace-local-support.mjs', import.meta.url).href
  const child = spawnSync(process.execPath, ['--input-type=module', '-e', `import { causalLinkDiagnostics, writeExecutionTraceFirstFailure } from ${JSON.stringify(moduleURL)};
    const report={run_id:'local-trace-1',stage:'causal-link'};
    const causal=causalLinkDiagnostics(()=>writeExecutionTraceFirstFailure(process.argv[1],report));report.causal_link=causal.receipt;
    causal.step('link-click');causal.fail({name:'TimeoutError',message:'PRIVATE'});process.exit(17);`, path], { timeout: 5000, encoding: 'utf8' })
  expect(child.status).toBe(17)
  expect(JSON.parse(readFileSync(path)).causal_link.substep).toBe('link-click')
  expect(readFileSync(path, 'utf8')).not.toContain('PRIVATE')
})


it('keeps navigation failure counters closed, durable and independent of the original causal failure', () => {
  const path = join(directory, 'navigation-first.json'); writeFileSync(path, '', { mode: 0o600 })
  const report = { run_id: 'local-trace-1', stage: 'navigation-pending', navigation: {
    error_code: 'navigation_state_window_lost', polls: 19, comparisons: 3, page_reads: 27,
    states: { pending: true, running: false, secret: 'PRIVATE' }, token: 'PRIVATE' } }
  writeExecutionTraceFirstFailure(path, report)
  expect(JSON.parse(readFileSync(path)).navigation).toEqual({ error_code: 'navigation_state_window_lost', polls: 19, comparisons: 3, page_reads: 27, states: { pending: true } })
  expect(readFileSync(path, 'utf8')).not.toContain('PRIVATE')
  const first = readFileSync(path, 'utf8'); report.stage = 'navigation-success'
  expect(() => writeExecutionTraceFirstFailure(path, report)).toThrow('file_invalid')
  expect(readFileSync(path, 'utf8')).toBe(first)
})
