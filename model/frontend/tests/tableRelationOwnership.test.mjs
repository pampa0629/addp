import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync, readdirSync } from 'node:fs'

const sourceRoot = new URL('../src/', import.meta.url)
const filesUnder = directory => readdirSync(directory, { withFileTypes: true }).flatMap(entry => {
  const url = new URL(entry.name + (entry.isDirectory() ? '/' : ''), directory)
  return entry.isDirectory() ? filesUnder(url) : [url]
})

test('dimension relation mutations have a single frontend owner', () => {
  const owners = filesUnder(sourceRoot).filter(file => /\.vue$/.test(file.pathname))
    .filter(file => /logicalTableAPI\.(add|update|remove)DimensionRelation\(/.test(readFileSync(file, 'utf8')))
  assert.deepEqual(owners.map(file => file.pathname.split('/').pop()), ['TableRelationEditor.vue'])
  const overview = readFileSync(new URL('views/StarSchemaView.vue', sourceRoot), 'utf8')
  assert.doesNotMatch(overview, /addDimDialog|canModifyDimensionRelation|editableDimensionTables/)
  assert.match(overview, /buildTableRelationRoute/)
})
