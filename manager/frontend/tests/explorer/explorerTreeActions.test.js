import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'

const explorerTreeSource = readFileSync(
  new URL('../../src/components/explorer/ExplorerTree.vue', import.meta.url),
  'utf8'
)

describe('ExplorerTree row actions', () => {
  it('keeps refresh as the only resource-tree row action', () => {
    expect(explorerTreeSource).toContain("id: 'refresh'")
    expect(explorerTreeSource).not.toContain("id: 'embedding'")
    expect(explorerTreeSource).not.toContain("id: 'embedding-ready'")
    expect(explorerTreeSource).not.toContain("id: 'embedding-batch'")
  })
})
