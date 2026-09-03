import { describe, expect, it } from 'vitest'
import {
  buildDynamicSchemaColumnDescriptors,
  buildTablePreviewColumnDescriptors
} from '../../../../common-frontend/basic/src/utils/dynamicSchemaColumns.js'
import { tabularCellValue } from '../../../../common-frontend/basic/src/utils/tabularResult.js'

describe('dynamic schema table columns', () => {
  it('expands a stable object column from provider field paths', () => {
    const rows = [{
      _id: '1',
      userInfo: { gender: 1, nickName: 'Ada', phone: '10086' },
      myOutdoors: [{ id: 1 }]
    }]
    const descriptors = buildDynamicSchemaColumnDescriptors(
      ['_id', 'userInfo', 'myOutdoors'],
      [
        { column_name: '_id', path: ['_id'] },
        { column_name: 'userInfo', path: ['userInfo'], type: 'object' },
        { column_name: 'userInfo.gender', path: ['userInfo', 'gender'], type: 'int' },
        { column_name: 'userInfo.nickName', path: ['userInfo', 'nickName'], type: 'string' },
        { column_name: 'userInfo.phone', path: ['userInfo', 'phone'], type: 'string' },
        { column_name: 'myOutdoors', path: ['myOutdoors'], type: 'array' }
      ]
    )

    expect(descriptors.map(column => column.key)).toEqual([
      '_id',
      'userInfo.gender',
      'userInfo.nickName',
      'userInfo.phone',
      'myOutdoors'
    ])
    expect(tabularCellValue(rows[0], descriptors[2])).toBe('Ada')
  })

  it('does not expand arrays or object columns without stable schema children', () => {
    const descriptors = buildDynamicSchemaColumnDescriptors(
      ['items', 'payload'],
      [{ column_name: 'items.name', path: ['items', 'name'] }]
    )

    expect(descriptors.map(column => column.key)).toEqual(['items', 'payload'])
  })

  it('provides selectable columns for regular tables and hides raw geometry', () => {
    const descriptors = buildTablePreviewColumnDescriptors({
      columns: ['id', 'name', 'geom'],
      columnMetadata: [],
      geometryColumns: ['geom']
    })

    expect(descriptors.map(column => column.key)).toEqual(['id', 'name'])
  })
})
