import test from 'node:test'
import assert from 'node:assert/strict'
import { createDefaultDWLayers } from '../src/utils/dwLayerDefaults.js'

test('default DW layers distinguish structural ODS shaping from business processing', () => {
  const messages = {
    'model.dw_layer.defaults.ods.name': '贴源层',
    'model.dw_layer.defaults.ods.description': '允许确定性的嵌套展开、数组拆行和类型规范化，但不承载业务口径加工。',
    'model.dw_layer.defaults.dwd.name': '明细层',
    'model.dw_layer.defaults.dwd.description': '标准化明细模型层',
    'model.dw_layer.defaults.dws.name': '汇总层',
    'model.dw_layer.defaults.dws.description': '主题汇总层',
    'model.dw_layer.defaults.ads.name': '应用层',
    'model.dw_layer.defaults.ads.description': '应用数据集市'
  }
  const layers = createDefaultDWLayers(key => messages[key])
  const ods = layers.find(layer => layer.layer_code === 'ods')
  const dwd = layers.find(layer => layer.layer_code === 'dwd')
  const dws = layers.find(layer => layer.layer_code === 'dws')

  assert.deepEqual(
    [ods.sort_order, dwd.sort_order, dws.sort_order],
    [1, 2, 3]
  )
  assert.equal(ods.naming_rule, 'ods_{domain}_{entity}')
  assert.match(ods.description, /嵌套展开、数组拆行和类型规范化/)
  assert.match(ods.description, /不承载业务口径加工/)
  assert.equal(dwd.naming_rule, 'dwd_{domain}_{entity} / dim_{domain}_{entity}')
  assert.equal(dws.naming_rule, 'dws_{domain}_{subject}')
})
