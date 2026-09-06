import assert from 'node:assert/strict'
import test from 'node:test'
import { boundedExportHasMore, descriptorSupportsExport, downloadCurrentBoundedExport, exportFormatForRenderer } from '../src/utils/boundedExport.mjs'

test('maps renderers to the bounded format declared by the Service descriptor', () => {
  const descriptor = { input_contract: { formats: ['json', 'csv'] } }
  assert.equal(exportFormatForRenderer('table'), 'csv')
  assert.equal(exportFormatForRenderer('chart'), 'csv')
  assert.equal(exportFormatForRenderer('map'), 'geojson')
  assert.equal(descriptorSupportsExport(descriptor, 'table'), true)
  assert.equal(descriptorSupportsExport(descriptor, 'map'), false)
})

test('treats only an explicit true response header as an incomplete export', () => {
  assert.equal(boundedExportHasMore({ 'x-addp-has-more': 'TRUE' }), true)
  assert.equal(boundedExportHasMore({ 'x-addp-has-more': 'false' }), false)
  assert.equal(boundedExportHasMore({}), false)
  assert.equal(downloadCurrentBoundedExport(
    { data: 'partial', headers: { 'x-addp-has-more': 'true' } },
    'partial.csv',
    () => true,
  ), 'incomplete')
})

test('downloads a current complete export before releasing the Blob URL', async () => {
  const originalDocument = globalThis.document
  const originalCreateObjectURL = URL.createObjectURL
  const originalRevokeObjectURL = URL.revokeObjectURL
  const calls = []
  const link = {
    style: {},
    click() { calls.push('click') },
    remove() { calls.push('remove') },
  }
  globalThis.document = {
    body: { appendChild(candidate) { assert.equal(candidate, link); calls.push('append') } },
    createElement(tag) { assert.equal(tag, 'a'); return link },
  }
  URL.createObjectURL = () => { calls.push('create'); return 'blob:addp-export' }
  URL.revokeObjectURL = (url) => { assert.equal(url, 'blob:addp-export'); calls.push('revoke') }
  try {
    assert.equal(downloadCurrentBoundedExport(
      { data: 'id\n1\n', headers: {} },
      'result.csv',
      () => true,
    ), 'downloaded')
    assert.equal(link.href, 'blob:addp-export')
    assert.equal(link.download, 'result.csv')
    assert.deepEqual(calls, ['create', 'append', 'click', 'remove'])
    await new Promise((resolve) => setTimeout(resolve, 0))
    assert.deepEqual(calls, ['create', 'append', 'click', 'remove', 'revoke'])
  } finally {
    globalThis.document = originalDocument
    URL.createObjectURL = originalCreateObjectURL
    URL.revokeObjectURL = originalRevokeObjectURL
  }
})
