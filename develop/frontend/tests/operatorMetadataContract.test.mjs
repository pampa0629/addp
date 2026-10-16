import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { resolve } from 'node:path'
import { buildOperatorCategories, filterOperatorCategories, operatorCategoryKeys } from '../src/utils/operatorCategories.js'

const source = await readFile(resolve('src/utils/operatorMetadataContract.js'), 'utf8')
const mod = await import(`data:text/javascript;charset=utf-8,${encodeURIComponent(source)}`)

const validOperator = {
  id: 'buffer',
  name: 'buffer',
  display_name: '缓冲区分析',
  description: '生成缓冲区',
  category_path: ['空间分析'],
  parameters: [{ name: 'connection_info', type: 'object' }],
  public_parameters: [],
  output_ports: [
    {
      name: 'default',
      type: 'geodataframe',
      is_default: true
    }
  ]
}

assert.equal(mod.isStandardOperatorMetadata(validOperator), true)
assert.equal(mod.isStandardOperatorMetadata({ ...validOperator, category_path: [] }), false)
assert.equal(mod.isStandardOperatorMetadata({ ...validOperator, public_parameters: undefined }), false)
assert.equal(mod.isStandardOperatorMetadata({ ...validOperator, output_ports: [] }), false)
assert.equal(mod.findInvalidOperatorMetadata([validOperator]), null)
assert.equal(
  mod.findInvalidOperatorMetadata([validOperator, { ...validOperator, name: '' }]).name,
  ''
)

const operators = [
  { name: 'table_load', displayName: '表格加载', description: '读取表格', categoryPath: ['通用处理', '数据读写'] },
  { name: 'table_save', displayName: '表格保存', description: '写出表格', categoryPath: ['通用处理', '数据读写'] },
  { name: 'raster_load', displayName: '栅格加载', description: '读取影像', categoryPath: ['栅格处理', '数据读写'] },
  { name: 'extension', displayName: '扩展算子', description: '单级目录', categoryPath: ['扩展分类'] },
  { name: 'deep', displayName: '深层算子', description: '多级目录', categoryPath: ['a', 'b', 'c'] },
  { name: 'slash', displayName: '斜线组名', description: '独立目录', categoryPath: ['a / b', 'c'] }
]
const original = structuredClone(operators)
const categories = buildOperatorCategories(operators)
assert.deepEqual(categories.map(category => [category.name, category.count]), [
  ['通用处理', 2], ['栅格处理', 1], ['扩展分类', 1], ['a', 1], ['a / b', 1]
])
assert.notEqual(categories[0].children[0].key, categories[1].children[0].key)
assert.notEqual(categories[3].children[0].children[0].key, categories[4].children[0].key)
assert.deepEqual(categories[0].children[0].operators.map(operator => operator.name), ['table_load', 'table_save'])
assert.equal(categories[2].operators[0].name, 'extension')
assert.equal(new Set(operatorCategoryKeys(categories)).size, 10)
assert.equal(filterOperatorCategories(categories, ''), categories)
const matches = filterOperatorCategories(categories, ' RASTER_LOAD ')
assert.deepEqual(matches.map(category => [category.name, category.count]), [['栅格处理', 1]])
assert.deepEqual(matches[0].children[0].operators.map(operator => operator.name), ['raster_load'])
assert.deepEqual(operatorCategoryKeys(matches), ['["栅格处理"]', '["栅格处理","数据读写"]'])
assert.equal(filterOperatorCategories(categories, '数据读写').length, 2)
assert.equal(filterOperatorCategories(categories, '影像')[0].name, '栅格处理')
assert.deepEqual(filterOperatorCategories(categories, '不存在'), [])
assert.deepEqual(operators, original)

console.log('operatorMetadataContract tests passed')
