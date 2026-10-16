import { describe, expect, it, vi } from 'vitest'
import { canPreviewStaticTrajectory, readStaticTrajectoryTable } from '../../src/utils/staticTrajectoryPreview'

describe('static trajectory preview adapter', () => {
  it('uses scanned format and file identity without enabling containers or arbitrary JSON documents', () => {
    const input = { format: 'csv', itemType: 'file', mode: 'table' }
    for (const format of ['csv', 'json', 'geojson']) expect(canPreviewStaticTrajectory({ ...input, format })).toBe(true)
    for (const change of [{ mode: 'object' }, { itemType: 'table' }, { format: 'xlsx' }, { childName: 'Sheet1' }, { refPath: 'data.csv' }]) expect(canPreviewStaticTrajectory({ ...input, ...change })).toBe(false)
  })
  it('passes a single structured bounded selection through the existing preview route', async () => {
    const client = { get: vi.fn().mockResolvedValue({ preview_type: 'table', data: {} }) }
    const selection = { device_field: 'unit', time_field: 'at', time_encoding: 'iso', devices: ['A'], start: '2026-10-16T01:00:00Z', end: '2026-10-16T03:00:00Z' }
    const signal = new AbortController().signal
    await readStaticTrajectoryTable(client, 'locator', signal, selection)
    expect(client.get).toHaveBeenCalledExactlyOnceWith('/manager/preview', { params: { locator: 'locator', page: 1, page_size: 2000, trajectory_selection: JSON.stringify(selection) }, signal })
  })
  it('reads one protected first page without downloading source bytes or altering ordinary pagination', async () => {
    const data = { rows: [], columns: [], page: 1, total: 0, has_more: false }
    const client = { get: vi.fn().mockResolvedValue({ preview_type: 'table', data }) }
    const signal = new AbortController().signal
    const locator = 'addp://engine/14/path/telemetry.csv?type=file&item_id=42'
    expect(await readStaticTrajectoryTable(client, locator, signal)).toBe(data)
    expect(client.get).toHaveBeenCalledExactlyOnceWith('/manager/preview', { params: { locator, page: 1, page_size: 2000 }, signal })
    client.get.mockResolvedValue({ preview_type: 'object', data: { content: {} } })
    expect(await readStaticTrajectoryTable(client, locator, signal)).toBeNull()
  })
})
