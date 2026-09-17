import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync, existsSync } from 'node:fs'
import { parameterLabel, parameterDescription, parameterControlType, parameterOptionsAllow, validParameterOptions, intersectParameterOptions } from '../src/utils/parameterInput.mjs'
const option = (value) => ({ value, labels: { 'zh-cn': String(value), en: String(value) } })
test('finite options preserve typed values and reject malformed domains', () => {
  assert.equal(parameterControlType('bool'), 'select')
  assert.equal(parameterControlType('date'), 'date')
  assert.equal(parameterOptionsAllow([option(1)], '1'), false)
  assert.equal(parameterOptionsAllow([option(false)], false), true)
  assert.equal(validParameterOptions([option(1), option(1)], 'int'), false)
  assert.equal(validParameterOptions([{ value: 'a', labels: { en: 'A' } }], 'string'), false)
  assert.equal(validParameterOptions([option(null)], 'string'), false)
  assert.equal(validParameterOptions([option('a')], 'string'), true)
  assert.deepEqual(intersectParameterOptions([option('a'), option('b')], [option('b')]), [option('b')])
  assert.throws(() => intersectParameterOptions([option('a')], [option('b')]))
  assert.throws(() => intersectParameterOptions([option('a')], [{ value: 'a', labels: { 'zh-cn': 'different', en: 'a' } }]))
})
test('service and application input rendering has one shared owner', () => {
  const root = new URL('../../../', import.meta.url)
  assert.equal(existsSync(new URL('workbench/frontend/src/components/ApplicationParameterValueInput.vue', root)), false)
  for (const path of ['service/frontend/src/views/QueryServiceDetail.vue', 'service/frontend/src/views/QueryServiceForm.vue', 'workbench/frontend/src/components/ApplicationComponentEditor.vue', 'workbench/frontend/src/components/SpatialExplorationWizard.vue', 'workbench/frontend/src/components/ApplicationParameterFields.vue', 'workbench/frontend/src/views/DataApplicationEditor.vue']) {
    const source = readFileSync(new URL(path, root), 'utf8')
    assert.match(source, /common-frontend\/basic\/src\/components\/ParameterValueInput.vue/)
    assert.doesNotMatch(source, /<el-(?:input-number|date-picker|select)\s+v-(?:else-)?if="parameter\.(?:controlType|type)/)
  }
  const canvas = readFileSync(new URL('workbench/frontend/src/components/DataApplicationCanvas.vue', root), 'utf8')
  assert.match(canvas, /import ApplicationParameterFields from ['"]\.\/ApplicationParameterFields\.vue['"]/)
  assert.equal((canvas.match(/<ApplicationParameterFields\b/g) || []).length, 2)
  assert.doesNotMatch(canvas, /<ParameterValueInput\b|class="parameter-field"/)
  const input = readFileSync(new URL('../src/components/ParameterValueInput.vue', import.meta.url), 'utf8')
  assert.match(input, /option\.labels\[locale\]/)
  assert.match(input, /:value="option.value"/)
})

test('parameter presentation uses declared language and never infers business meaning from names', () => {
 const p = { name: 'subject_id', presentation: { labels: { 'zh-cn': '设备编号', en: 'Device ID' }, descriptions: { 'zh-cn': '输入设备编号', en: 'Enter device ID' } } }
 assert.equal(parameterLabel(p, 'zh-cn'), '设备编号')
 assert.equal(parameterLabel(p, 'en'), 'Device ID')
 assert.equal(parameterDescription(p, 'en'), 'Enter device ID')
 assert.equal(parameterLabel({ name: 'subject_id' }, 'zh-cn'), 'subject_id')
 assert.equal(parameterDescription({ name: 'end_date' }, 'zh-cn'), '')
 assert.equal(parameterDescription({ name: 'p', description: 'Publisher help' }, 'en'), 'Publisher help')
 for (const path of ['service/frontend/src/views/QueryServiceDetail.vue', 'workbench/frontend/src/components/ApplicationComponentEditor.vue']) {
  const source = readFileSync(new URL(path, new URL('../../../', import.meta.url)), 'utf8')
  assert.match(source, /<ParameterCaption[^>]*:parameter=/)
  assert.doesNotMatch(source, /parameter\.presentation\.(labels|descriptions)/)
 }
})
