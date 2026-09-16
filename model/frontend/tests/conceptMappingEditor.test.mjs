import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

test('concept realization mappings have one logical-table aggregate editor and canonical API', async () => {
  const [component, detail, api, state] = await Promise.all([
    readFile(new URL('../src/components/ConceptMappingEditor.vue', import.meta.url), 'utf8'),
    readFile(new URL('../src/views/LogicalTableDetail.vue', import.meta.url), 'utf8'),
    readFile(new URL('../src/api/model.js', import.meta.url), 'utf8'),
    readFile(new URL('../src/utils/modelDetailState.js', import.meta.url), 'utf8')
  ])

  assert.match(detail, /name="concept-mappings"/)
  assert.match(detail, /<ConceptMappingEditor/)
  assert.match(component, /getConceptMappings/)
  assert.match(component, /replaceConceptMappings/)
  assert.match(component, /table_mappings/)
  assert.match(component, /field_mappings/)
  assert.match(component, /relation_mappings/)
  assert.match(api, /logical-tables\/\$\{id\}\/concept-mappings/)
  assert.doesNotMatch(state, /entity_id: table\?\.entity_id/)
})
