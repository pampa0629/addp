import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { resolve } from 'node:path'

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

console.log('operatorMetadataContract tests passed')
