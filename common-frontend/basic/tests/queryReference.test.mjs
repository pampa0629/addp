import test from 'node:test'
import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { queryReferenceParts, renderQueryReference, quoteQueryIdentifier } from '../src/utils/queryReference.mjs'

test('exact owner parts render using the selected engine quote without renaming', () => {
  const reference = { parts: ['Business_PostgreSQL', ' 原.schema" ', ' 订单.table` '] }
  assert.equal(renderQueryReference(reference, { identifierQuotes: { sql: '"' } }, 'sql'), '"Business_PostgreSQL"." 原.schema"" "." 订单.table` "')
  assert.equal(renderQueryReference(reference, { identifierQuotes: { sql: '`' } }, 'sql'), '`Business_PostgreSQL`.` 原.schema" `.` 订单.table`` `')
  assert.deepEqual(reference.parts, ['Business_PostgreSQL', ' 原.schema" ', ' 订单.table` '])
  assert.equal(quoteQueryIdentifier(' ', { identifierQuotes: { sql: '"' } }, 'sql'), '" "')
})

test('same flat display name cannot collapse distinct physical paths', () => {
  const capability = { identifierQuotes: { sql: '"' } }
  assert.notEqual(renderQueryReference({ parts: ['a.b', 'c'] }, capability, 'sql'), renderQueryReference({ parts: ['a', 'b.c'] }, capability, 'sql'))
  assert.equal(renderQueryReference({ parts: [' Persons.v2 '] }, {}, 'mql'), '" Persons.v2 "')
})

test('missing or malformed owner facts and missing target grammar have no fallback', () => {
  for (const value of ['schema.table', {}, { parts: [] }, { parts: [''] }, { parts: ['x'], connection_info: {} }, { parts: ['x\0'] }]) {
    assert.equal(queryReferenceParts(value), null)
    assert.equal(renderQueryReference(value, { identifierQuotes: { sql: '"' } }, 'sql'), '')
  }
  assert.equal(renderQueryReference({ parts: ['schema', 'table'] }, {}, 'sql'), '')
  assert.equal(renderQueryReference({ parts: ['db', 'collection'] }, {}, 'mql'), '')
})

test('Develop consumes the shared renderer and never reconstructs source names', async () => {
  const source = await readFile(new URL('../../../develop/frontend/src/views/QueryEditor.vue', import.meta.url), 'utf8')
  const oldOwner = await readFile(new URL('../../../develop/frontend/src/utils/queryWorkbench.mjs', import.meta.url), 'utf8')
  assert.match(source, /renderQueryReference\(/)
  assert.match(source, /getResourceFacts\('\/api\/v1\/manager'/)
  assert.doesNotMatch(source, /federatedIdentifier|segments\.map\(federated/)
  assert.doesNotMatch(oldOwner, /export function (?:quoteQueryIdentifier|nativeCatalogPathText|nativeCatalogSegmentText)/)
})


test('expanded catalog completions use owner references, never display paths', async () => {
  const source = await readFile(new URL('../../../develop/frontend/src/views/QueryEditor.vue', import.meta.url), 'utf8')
  const node = source.slice(source.indexOf('const rememberCatalogNode'), source.indexOf('const isQueryCatalogSelectionAllowed'))
  assert.match(node, /await loadQueryReferenceFacts/)
  assert.match(node, /queryTextForCatalogSelection/)
  assert.doesNotMatch(node, /formatLocatorDisplayPath|federatedIdentifier/)
})
