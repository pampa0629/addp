import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'
import { buildNodeScanSummary } from '../../src/utils/nodeScanSummary.js'

describe('node scan summary', () => {
  const t = key => key
  it('keeps failed range status independent from previously successful depth and time', () => {
    expect(buildNodeScanSummary({ scan_status: 'failed', scanned_depth: 'deep', scanned_at: '2026-01-01T00:00:00Z', item_count: 518 }, t)).toEqual({
      status: 'manager.explorer.nodeScan.status.failed', depth: 'manager.explorer.nodeScan.depth.deep', scannedAt: '2026-01-01T00:00:00Z'
    })
  })
  it('does not infer missing facts from item count or localized legacy values', () => {
    expect(buildNodeScanSummary({ item_count: 518, scan_status: '已扫描' }, t)).toEqual({ status: '-', depth: '-', scannedAt: '-' })
    expect(buildNodeScanSummary({ scan_status: 'running', scanned_depth: 'none' }, t)).toEqual({ status: 'manager.explorer.nodeScan.status.running', depth: 'manager.explorer.nodeScan.depth.none', scannedAt: '-' })
  })
  it('wires three independent labels into the one node panel with both locales', () => {
    const source = readFileSync(new URL('../../src/components/explorer/NodePanel.vue', import.meta.url), 'utf8')
    for (const locale of ['zh-cn', 'en']) {
      const messages = JSON.parse(readFileSync(new URL(`../../src/i18n/${locale}.json`, import.meta.url), 'utf8')).manager.explorer.nodeScan
      for (const key of ['statusLabel', 'depthLabel', 'successTimeLabel']) {
        expect(messages[key]).toBeTruthy()
        expect(source).toContain(`manager.explorer.nodeScan.${key}`)
      }
      for (const key of ['pending', 'running', 'completed', 'failed']) expect(messages.status[key]).toBeTruthy()
      for (const key of ['none', 'basic', 'deep']) expect(messages.depth[key]).toBeTruthy()
    }
    expect(source).toContain('buildNodeScanSummary(')
    expect(source).not.toContain('statusMap')
  })
})
