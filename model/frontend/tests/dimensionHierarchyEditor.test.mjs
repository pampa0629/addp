import test from 'node:test'
import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'

test('dimension hierarchies are edited only inside dimension logical tables', async () => {
  const detail = await readFile(new URL('../src/views/LogicalTableDetail.vue', import.meta.url), 'utf8')
  const editor = await readFile(new URL('../src/components/DimensionHierarchyEditor.vue', import.meta.url), 'utf8')
  const api = await readFile(new URL('../src/api/model.js', import.meta.url), 'utf8')

  assert.match(detail, /v-if="form\.table_type === 'dimension'"/)
  assert.match(detail, /<DimensionHierarchyEditor/)
  assert.match(detail, /:version="table\.version"/)
  assert.match(detail, /@update-version="table\.version = \$event"/)
  assert.match(editor, /version: props\.version/)
  assert.match(editor, /field_id/)
  assert.match(api, /logical-tables\/\$\{tableId\}\/dimension-hierarchies/)
  assert.doesNotMatch(detail, /hierarchy_id|hierarchy_level/)
})
