import assert from 'node:assert/strict'
import test from 'node:test'
import { buildDataApplicationPreview, confirmDataApplicationAction, normalizedApplicationSnapshot } from '../src/utils/dataApplicationDraft.mjs'

test('normalizes a Vue-style reactive snapshot without cloning the Proxy directly', () => {
  const snapshot = new Proxy({
    page: { id: 'page-a', title: 'Page', display_mode: 'desktop', refresh_interval_seconds: 0, visible_sections: ['title', 'parameters', 'query_actions'], placements: [] },
    components: [{ id: 'component-a' }],
    parameters: [
      { key: 'used', label: 'Used' },
      { key: 'unused', label: 'Unused' },
    ],
    parameter_bindings: [
      { application_parameter_key: 'used', component_id: 'component-a', component_parameter_key: 'status' },
    ],
  }, {})

  assert.deepEqual(normalizedApplicationSnapshot(snapshot), {
    page: { id: 'page-a', title: 'Page', display_mode: 'desktop', refresh_interval_seconds: 0, visible_sections: ['title', 'parameters', 'query_actions'], placements: [] },
    components: [{ id: 'component-a' }],
    parameters: [{ key: 'used', label: 'Used' }],
    parameter_bindings: [
      { application_parameter_key: 'used', component_id: 'component-a', component_parameter_key: 'status' },
    ],
  })
})

test('treats lifecycle dialog cancellation as a normal false result', async () => {
  assert.equal(await confirmDataApplicationAction(async () => { throw 'cancel' }, 'Publish?'), false)
  assert.equal(await confirmDataApplicationAction(async () => { throw 'close' }, 'Publish?'), false)
  await assert.rejects(() => confirmDataApplicationAction(async () => { throw new Error('broken dialog') }, 'Publish?'), /broken dialog/)
})

test('builds a detached preview from the same normalized payload as draft persistence', () => {
  const application = {
    name: '  Example application  ',
    description: '  Current draft  ',
    snapshot: {
      page: { title: 'Unsaved title' },
      components: [{ id: 'component-a' }],
      parameters: [{ key: 'used' }, { key: 'unused' }],
      parameter_bindings: [{ application_parameter_key: 'used', component_id: 'component-a', component_parameter_key: 'filter' }],
    },
  }

  const preview = buildDataApplicationPreview(application)
  assert.deepEqual(preview, {
    name: 'Example application',
    description: 'Current draft',
    revision_number: 0,
    snapshot: {
      page: { title: 'Unsaved title' },
      components: [{ id: 'component-a' }],
      parameters: [{ key: 'used' }],
      parameter_bindings: [{ application_parameter_key: 'used', component_id: 'component-a', component_parameter_key: 'filter' }],
    },
  })
  preview.snapshot.page.title = 'Preview interaction'
  assert.equal(application.snapshot.page.title, 'Unsaved title')
})
