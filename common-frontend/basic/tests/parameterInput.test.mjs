import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync, existsSync } from 'node:fs'
import { parameterControlType, parameterOptionsAllow, validParameterOptions, intersectParameterOptions } from '../src/utils/parameterInput.mjs'
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
  for (const path of ['service/frontend/src/views/QueryServiceDetail.vue', 'service/frontend/src/views/QueryServiceForm.vue', 'workbench/frontend/src/components/ApplicationComponentEditor.vue', 'workbench/frontend/src/components/SpatialExplorationWizard.vue', 'workbench/frontend/src/components/DataApplicationCanvas.vue', 'workbench/frontend/src/views/DataApplicationEditor.vue']) {
    const source = readFileSync(new URL(path, root), 'utf8')
    assert.match(source, /common-frontend\/basic\/src\/components\/ParameterValueInput.vue/)
    assert.doesNotMatch(source, /<el-(?:input-number|date-picker|select)\s+v-(?:else-)?if="parameter\.(?:controlType|type)/)
  }
  const input = readFileSync(new URL('../src/components/ParameterValueInput.vue', import.meta.url), 'utf8')
  assert.match(input, /option\.labels\[locale\]/)
  assert.match(input, /:value="option.value"/)
})
