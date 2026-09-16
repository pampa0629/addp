// 范围执行状态、已完成深度和最近成功时间是三个独立事实。
export function buildNodeScanSummary(metadata = {}, t) {
  const status = ['pending', 'running', 'completed', 'failed'].includes(metadata.scan_status)
    ? t(`manager.explorer.nodeScan.status.${metadata.scan_status}`) : '-'
  const depth = ['none', 'basic', 'deep'].includes(metadata.scanned_depth)
    ? t(`manager.explorer.nodeScan.depth.${metadata.scanned_depth}`) : '-'
  return { status, depth, scannedAt: metadata.scanned_at || '-' }
}
