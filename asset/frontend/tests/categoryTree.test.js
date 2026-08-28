import { describe, expect, it } from 'vitest'

import {
  buildCategoryParentOptions,
  collectCategorySubtreeIds,
  findCategoryNode
} from '@/utils/categoryTree'

const tree = [
  {
    id: 1,
    name: 'Government',
    children: [
      { id: 2, name: 'Education', children: [{ id: 3, name: 'Schools', children: [] }] }
    ]
  },
  { id: 4, name: 'Healthcare', children: [] }
]

describe('AssetCategory parent selection', () => {
  it('excludes the edited category and every descendant while preserving readable paths', () => {
    const edited = findCategoryNode(tree, 2)
    const excluded = collectCategorySubtreeIds(edited)

    expect(buildCategoryParentOptions(tree, excluded)).toEqual([
      { value: 1, label: 'Government' },
      { value: 4, label: 'Healthcare' }
    ])
  })

  it('finds nested categories without exposing IDs as labels', () => {
    expect(findCategoryNode(tree, 3)?.name).toBe('Schools')
    expect(buildCategoryParentOptions(tree).map(option => option.label)).toEqual([
      'Government',
      'Government / Education',
      'Government / Education / Schools',
      'Healthcare'
    ])
  })
})
