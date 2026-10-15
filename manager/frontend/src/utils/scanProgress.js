// File and logical-item totals describe different work; only known item totals
// produce a processing percentage. Worker updated_at is deliberately not used.
export function scanProgressView(run, t, now = Date.now()) {
  const snapshot = run?.scan_progress
  const success = run?.status === 'success'
  const terminal = ['success', 'failed', 'timeout', 'cancelled', 'canceled'].includes(run?.status)
  const known = snapshot ? snapshot.items_total_known : Number(run?.progress) > 0
  const percent = success ? 100 : known && (!snapshot || snapshot.items_planned > 0)
    ? Math.max(0, Math.min(99, Math.floor(snapshot
      ? snapshot.items_completed * 100 / snapshot.items_planned : Number(run.progress)))) : 0
  const indeterminate = !terminal && (!known || snapshot?.items_planned === 0 || ['discovering', 'identifying'].includes(snapshot?.phase))
  const lines = []
  const prefix = 'manager.explorer.scanProgress.'
  if (snapshot) {
    lines.push(t(prefix + (snapshot.files_total_known ? 'filesKnown' : 'files'), {
      files: snapshot.files_discovered, directories: snapshot.directories_discovered
    }))
    lines.push(t(prefix + (snapshot.items_total_known ? 'items' : 'itemsPending'), {
      completed: snapshot.items_completed, total: snapshot.items_planned
    }))
    lines.push(t(prefix + 'results', { succeeded: snapshot.items_succeeded, failed: snapshot.items_failed }))
    if (snapshot.index_cleanup_total > 0) lines.push(t(prefix + 'cleanup', { completed: snapshot.index_cleanup_completed, total: snapshot.index_cleanup_total, failed: snapshot.index_cleanup_failed }))
    if (snapshot.current_path) lines.push(t(prefix + 'current', { path: snapshot.current_path }))
    if (snapshot.operation) lines.push(t(prefix + 'operation.' + snapshot.operation))
    if (snapshot.operation === 'hashing') lines.push(t(prefix + 'bytes', {
      read: formatScanBytes(snapshot.current_bytes_read), total: formatScanBytes(snapshot.current_bytes_total)
    }))
  }
  const started = Date.parse(run?.started_at || run?.created_at || '')
  const finished = terminal ? Date.parse(run?.completed_at || '') : NaN
  const progressed = Date.parse(snapshot?.last_progress_at || '')
  const clock = Number.isFinite(finished) ? finished : now
  const timing = Number.isFinite(started) ? t(prefix + 'timing', {
    elapsed: formatScanDuration(clock - started),
    idle: Number.isFinite(progressed) ? formatScanDuration(clock - progressed) : t(prefix + 'noProgress')
  }) : ''
  return { percent, indeterminate, totalKnown: known, lines, timing,
    title: snapshot ? t(prefix + 'phase.' + snapshot.phase) : '',
    stale: !terminal && Number.isFinite(progressed) && now - progressed >= 30000 }
}

export function formatScanBytes(value) {
  const bytes = Math.max(0, Number(value) || 0)
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB']
  const unit = bytes ? Math.min(units.length - 1, Math.floor(Math.log(bytes) / Math.log(1024))) : 0
  return `${(bytes / 1024 ** unit).toFixed(unit ? 1 : 0)} ${units[unit]}`
}

export function formatScanDuration(milliseconds) {
  const seconds = Math.max(0, Math.floor(milliseconds / 1000))
  const h = Math.floor(seconds / 3600)
  const m = Math.floor(seconds / 60) % 60
  const s = seconds % 60
  return `${h ? String(h).padStart(2, '0') + ':' : ''}${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`
}
