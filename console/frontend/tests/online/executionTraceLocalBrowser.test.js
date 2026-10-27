import { readFileSync } from 'node:fs'
import { expect, it } from 'vitest'
import { admitExecutionTraceBrowserEnvironment, executionTraceBrowserInput, executionTraceTOTP } from '../../e2e/online/execution-trace-local-support.mjs'

const now = Date.parse('2026-10-23T06:00:00Z')
const fixture = () => ({ schema: 'addp.execution-trace-browser/v1', run_id: 'local-trace-1', module_name: 'orchestrator', instance_id: 'owned-orchestrator', runtime_instance_id: 2, execution_id: '11111111-1111-4111-8111-111111111111', trace_id: '1'.repeat(32), span_id: '2'.repeat(16), started_at: '2026-10-23T05:59:00Z', start: '2026-10-23T05:58:59Z', end: '2026-10-23T05:59:01Z', links: [{ trace_id: '3'.repeat(32), span_id: '4'.repeat(16), module_name: 'orchestrator', instance_id: 'owned-orchestrator', role: 'backend', runtime_instance_id: 2, started_at: '2026-10-23T03:00:00Z' }] })
const environment = () => ({ ADDP_EXECUTION_TRACE_LOCAL: '1', CONSOLE_URL: 'http://nginx/', ADDP_EXECUTION_TRACE_ADMIN_USERNAME: 'synthetic-admin', ADDP_EXECUTION_TRACE_ADMIN_PASSWORD: 'synthetic-password', ADDP_EXECUTION_TRACE_ADMIN_TOTP: 'GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ' })

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
