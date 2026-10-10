import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createLatestRequestScope } from '../src/utils/latestRequestScope.mjs'

test('replacing a request aborts transport and rejects late replies even when transport ignores abort', async () => {
  const scope = createLatestRequestScope(), first = scope.invalidate()
  let signal, complete
  const pending = scope.request(config => { signal = config.signal; return new Promise(resolve => { complete = resolve }) })
  const second = scope.invalidate()
  assert.equal(signal.aborted, true)
  complete('old evidence')
  assert.equal(await pending, 'old evidence')
  assert.equal(scope.current(first), false)
  assert.equal(scope.current(second), true)
  scope.dispose()
  assert.equal(scope.current(second), false)
  await assert.rejects(scope.request(() => 'new'), /request_scope_disposed/)
})

test('both monitoring resource pages use the sole request scope', () => {
  for (const page of ['NodeResources', 'ProcessResources']) {
    const source = readFileSync(new URL(`../../../monitor/frontend/src/views/${page}.vue`, import.meta.url), 'utf8')
    assert.match(source, /createLatestRequestScope/)
    assert.match(source, /\.current\(ticket\)/)
    assert.doesNotMatch(source, /new AbortController/)
  }
})
