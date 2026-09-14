import assert from 'node:assert/strict'
import { readFileSync, existsSync } from 'node:fs'
import test from 'node:test'
test('Model offers structural creation and no publication workflow', () => {
 const detail=readFileSync(new URL('../src/views/LogicalTableDetail.vue',import.meta.url),'utf8')
 assert.match(detail,/logicalTableAPI.createTarget/)
 assert.doesNotMatch(detail,/MaterializationActions|materialization_groups/)
 assert.equal(existsSync(new URL('../src/views/MaterializationGroupList.vue',import.meta.url)),false)
})
