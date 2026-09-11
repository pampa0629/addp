import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'

const explorerTreeSource = readFileSync(
  new URL('../../src/components/explorer/ExplorerTree.vue', import.meta.url),
  'utf8'
)
const zhCn = JSON.parse(readFileSync(new URL('../../src/i18n/zh-cn.json', import.meta.url), 'utf8'))
const en = JSON.parse(readFileSync(new URL('../../src/i18n/en.json', import.meta.url), 'utf8'))

describe('ExplorerTree row actions', () => {
  it('keeps refresh as the only resource-tree row action', () => {
    expect(explorerTreeSource).toContain("id: 'refresh-node'")
    expect(explorerTreeSource).toContain("id: 'refresh-item'")
    expect(explorerTreeSource).not.toContain("id: 'embedding'")
    expect(explorerTreeSource).not.toContain("id: 'embedding-ready'")
    expect(explorerTreeSource).not.toContain("id: 'embedding-batch'")
  })

  it('explains the different node and item refresh semantics', () => {
    expect(zhCn.manager.explorer.refreshNodeTooltip).toContain('基础刷新')
    expect(zhCn.manager.explorer.refreshItemTooltip).toContain('深度刷新')
    expect(en.manager.explorer.refreshNodeTooltip).toContain('Basic refresh')
    expect(en.manager.explorer.refreshItemTooltip).toContain('Deep refresh')
  })
})
