import assert from 'node:assert/strict'
import test from 'node:test'

import {
  containerTableChildren,
  isTransferableTableContainer,
  resolveContainerTableChild
} from '../src/views/TaskWizard/containerSource.mjs'

const attributes = {
  type_info: {
    container: {
      default_child: 'WGS84_Points',
      children: [
        { name: 'Readme', data_type: 'document' },
        { name: 'WGS84_Points', data_type: 'table', row_count: 265 },
        { name: 'WebMerc_Points', data_type: 'table', row_count: 265 }
      ]
    }
  }
}

test('容器源只暴露可传输的 table child', () => {
  assert.deepEqual(containerTableChildren(attributes).map(child => child.name), [
    'WGS84_Points',
    'WebMerc_Points'
  ])
  assert.equal(resolveContainerTableChild(attributes).name, 'WGS84_Points')
  assert.equal(resolveContainerTableChild(attributes, 'WebMerc_Points').name, 'WebMerc_Points')
})

test('容器源必须是已声明可读格式且含 table child', () => {
  const readableFormats = new Set(['pgeo', 'filegdb'])
  assert.equal(isTransferableTableContainer({
    dataType: 'container',
    representation: 'encoded',
    format: 'pgeo',
    attributes,
    readableFormats
  }), true)
  assert.equal(isTransferableTableContainer({
    dataType: 'container',
    representation: 'encoded',
    format: 'zip',
    attributes,
    readableFormats
  }), false)
})
