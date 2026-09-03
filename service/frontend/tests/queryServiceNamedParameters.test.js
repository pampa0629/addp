import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

test('query service publishing sends typed named parameters to output detection and creation', async () => {
  const source = await readFile(new URL('../src/views/QueryServiceForm.vue', import.meta.url), 'utf8')
  assert.match(source, /parameters:\s*sqlNamedParameterValues\(\)/)
  assert.match(source, /requestData\.named_parameters\s*=\s*sqlNamedParameters\.value\.map/)
  assert.match(source, /named_parameter.*required/s)
})

test('query service detail submits named parameter values through the existing query request', async () => {
  const source = await readFile(new URL('../src/views/QueryServiceDetail.vue', import.meta.url), 'utf8')
  assert.match(source, /request\s*=\s*\{\s*parameters:/s)
  assert.match(source, /previewNamedParameterRequired/)
})
