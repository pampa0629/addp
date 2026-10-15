import { describe, expect, it } from 'vitest'
import { scanProgressView, formatScanBytes } from '../../src/utils/scanProgress.js'

const t = (key, params) => `${key}:${JSON.stringify(params || {})}`
const now = Date.parse('2026-10-14T08:00:00Z')
const snapshot = { phase: 'processing', files_discovered: 19238, directories_discovered: 172,
  files_total_known: true, items_planned: 100, items_total_known: true,
  items_completed: 20, items_succeeded: 19, items_failed: 1,
  current_path: '3d/large.obj', operation: 'hashing', current_bytes_read: 1048576, current_bytes_total: 1073741824,
  last_progress_at: '2026-10-14T07:59:20Z' }

describe('scan progress observations', () => {
  it('keeps unknown totals indeterminate regardless of the old target percentage', () => {
    const view = scanProgressView({ status: 'running', progress: 10,
      scan_progress: { ...snapshot, phase: 'discovering', items_total_known: false, files_total_known: false } }, t, now)
    expect(view.indeterminate).toBe(true)
    expect(view.percent).toBe(0)
    expect(view.lines.join(' ')).toContain('19238')
  })
  it('counts logical items and current-file bytes, and ignores lease updated_at for inactivity', () => {
    const view = scanProgressView({ status: 'running', progress: 10, started_at: '2026-10-14T07:00:00Z',
      updated_at: '2026-10-14T08:00:00Z', scan_progress: snapshot }, t, now)
    expect(view.percent).toBe(20)
    expect(view.indeterminate).toBe(false)
    expect(view.stale).toBe(true)
    expect(view.timing).toContain('01:00:00')
    expect(view.timing).toContain('00:40')
    expect(view.lines.join(' ')).toContain('1.0 MiB')
    expect(view.lines.join(' ')).toContain('1.0 GiB')
  })
  it('shows confirmed cleanup batches independently of item processing', () => {
    const view = scanProgressView({ status: 'running', scan_progress: { ...snapshot, operation: 'cleaning_index',
      index_cleanup_total: 205, index_cleanup_completed: 100, index_cleanup_failed: 100 } }, t, now)
    expect(view.percent).toBe(20)
    expect(view.lines.join(' ')).toContain('"completed":100,"total":205,"failed":100')
    expect(view.lines.join(' ')).toContain('operation.cleaning_index')
    expect(view.lines.join(' ')).not.toContain('1.0 MiB')
  })
  it('shows real attribute range reads without changing item or fingerprint progress', () => {
    const run = { status: 'running', scan_progress: { ...snapshot, operation: 'enriching',
      current_range_bytes_read: 229376, current_ranges_read: 4096,
      last_progress_at: new Date(now).toISOString() } }
    const view = scanProgressView(run, t, now)
    expect(view.percent).toBe(20)
    expect(view.stale).toBe(false)
    expect(view.lines.join(' ')).toContain('rangeReads:{"read":"224.0 KiB","ranges":4096}')
    expect(view.lines.join(' ')).not.toContain('1.0 MiB')
    expect(scanProgressView({ ...run, scan_progress: { ...run.scan_progress, operation: 'hashing' } }, t, now).lines.join(' ')).not.toContain('rangeReads')
  })
  it('never reports completion before success or fabricates 100 percent on failure', () => {
    const run = { status: 'running', scan_progress: { ...snapshot, items_completed: 100, items_succeeded: 99 } }
    expect(scanProgressView(run, t).percent).toBe(99)
    expect(scanProgressView({ ...run, status: 'failed' }, t).percent).toBe(99)
    expect(scanProgressView({ ...run, status: 'success' }, t).percent).toBe(100)
    expect(scanProgressView({ status: 'failed', progress: 41 }, t).percent).toBe(41)
    expect(formatScanBytes(0)).toBe('0 B')
    const empty = scanProgressView({ status: 'running', scan_progress: { ...snapshot, items_planned: 0, items_completed: 0 } }, t)
    expect(empty.totalKnown).toBe(true)
    expect(empty.indeterminate).toBe(true)
    expect(empty.percent).toBe(0)
  })
})
