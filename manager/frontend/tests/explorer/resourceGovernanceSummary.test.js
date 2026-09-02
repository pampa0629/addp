import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

const itemPanelSource = readFileSync(
  fileURLToPath(new URL('../../src/components/explorer/ItemPanel.vue', import.meta.url)),
  'utf8'
)
const governanceTemplate = itemPanelSource.slice(
  itemPanelSource.indexOf('<section\n      v-if="itemFingerprint"'),
  itemPanelSource.indexOf('<div class="item-tab-bar"')
)

const zhCnMessages = JSON.parse(readFileSync(
  fileURLToPath(new URL('../../src/i18n/zh-cn.json', import.meta.url)),
  'utf8'
))

const enMessages = JSON.parse(readFileSync(
  fileURLToPath(new URL('../../src/i18n/en.json', import.meta.url)),
  'utf8'
))

describe('resource governance summary', () => {
  it('keeps Catalog and Security in one compact summary instead of stacked cards', () => {
    expect(itemPanelSource).toContain('class="resource-governance-summary"')
    expect(itemPanelSource).toContain('resource-governance-summary__catalog')
    expect(itemPanelSource).toContain('resource-governance-summary__security')
    expect(itemPanelSource).toContain('v-if="showCatalogDisplayName"')
    expect(itemPanelSource).not.toContain('catalog-summary-card')
    expect(itemPanelSource).not.toContain('protection-entry-card')
  })

  it('renders loading, missing, and unavailable Catalog states as compact labels', () => {
    expect(governanceTemplate).toContain("catalogLookupState === 'loading'")
    expect(governanceTemplate).toContain('catalogPendingShort')
    expect(governanceTemplate).toContain('catalogUnavailableShort')
    expect(governanceTemplate).not.toContain('<el-alert')
    expect(governanceTemplate).not.toContain('<el-skeleton')
  })

  it('defines the compact summary labels in both supported languages', () => {
    for (const messages of [zhCnMessages, enMessages]) {
      const explorer = messages.manager.explorer
      expect(explorer.governanceSummaryLabel).toBeTruthy()
      expect(explorer.catalogLoading).toBeTruthy()
      expect(explorer.catalogPendingShort).toBeTruthy()
      expect(explorer.catalogUnavailableShort).toBeTruthy()
      expect(explorer.openDataProtection).toBeTruthy()
    }
  })
})
