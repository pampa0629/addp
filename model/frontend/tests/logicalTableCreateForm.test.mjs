import test from 'node:test'
import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'

const source = await readFile(new URL('../src/views/LogicalTableList.vue', import.meta.url), 'utf8')

test('fact table creation exposes and submits its required grain declaration', () => {
  assert.match(source, /v-if="createForm\.table_type === 'fact'"[\s\S]*grain_description/)
  assert.match(source, /v-model="createForm\.grain_description"/)
  assert.match(source, /grain_description: ''/)
  assert.match(source, /grain_required/)
  assert.match(source, /logicalTableAPI\.create\(createForm\)/)
})

