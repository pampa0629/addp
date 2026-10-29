import { resourceMetrics, pressureMetrics, filesystemMetrics, inodeMetrics, mountMetrics, resourceDimensionRows, currentResourceValue, blockCapacityMetrics, validateResourceResponse } from '../../../../monitor/frontend/src/utils/nodeResources.js'
import { formatBytes } from '../../../../common-frontend/basic/src/utils/formatters.js'
import { formatFieldPresentationValue } from '../../../../common-frontend/basic/src/utils/fieldPresentation.mjs'
import zhCN from '../../../../monitor/frontend/src/i18n/zh-cn.json'

// Resource families arrive concurrently. Request identity, not arrival order, owns the receipt.
function resourceReceipts(receipts, nodeId, catalog) {
  const keys = catalog.map(metric => metric.key)
  return receipts.filter(row => {
    const query = row?.query
    if (row?.path !== '/api/v1/monitor/platform/resource_observations' || query?.node_id !== nodeId ||
        Object.keys(query).sort().join(',') !== 'metrics,node_id' || typeof query.metrics !== 'string') return false
    const metrics = query.metrics.split(',')
    return metrics.length === keys.length && new Set(metrics).size === keys.length && keys.every(key => metrics.includes(key))
  })
}

export function scalarResourceReceipts(receipts, nodeId) {
  return resourceReceipts(receipts, nodeId, resourceMetrics)
}

// Bind the displayed mount to both exact request families, including concurrent completion order.
// Formatting reuses the shared presentation functions and the page's metric/unit definitions.
export function filesystemMountReceipt(receipts, nodeId) {
  const capacity = resourceReceipts(receipts, nodeId, filesystemMetrics).at(-1)
  const inodes = resourceReceipts(receipts, nodeId, inodeMetrics).at(-1)
  if (!capacity || !inodes) throw new Error('filesystem mount receipts missing')
  const rows = resourceDimensionRows(capacity.value, inodes.value)
  const rowIndex = rows.findIndex(row => mountMetrics.every(metric => currentResourceValue(row.metrics[metric.key]) !== null))
  if (rowIndex < 0) throw new Error('filesystem mount has no complete valid readings')
  const row = rows[rowIndex]
  const metrics = Object.fromEntries(mountMetrics.map(metric => {
    const value = currentResourceValue(row.metrics[metric.key])
    return [metric.key, metric.unit === 'bytes' ? formatBytes(value, 2, 'zh-CN')
      : formatFieldPresentationValue(value, { precision: metric.precision, unit: zhCN.monitor.resources.units[metric.unit] }, 'zh-CN')]
  }))
  return { capacity_index: receipts.indexOf(capacity), inode_index: receipts.indexOf(inodes),
    row_index: rowIndex, dimensions: { ...row.dimensions }, metrics }
}

// One DOM receipt reader for both real Online and controlled Console iframe acceptance.
export async function readFilesystemMountTable(table, receipts, nodeId, expect) {
  const receipt = filesystemMountReceipt(receipts, nodeId)
  const row = table.locator('.el-table__body tr').nth(receipt.row_index)
  const dimensionCells = row.locator('td .cell')
  for (const [index, key] of ['device', 'mountpoint', 'fstype'].entries()) {
    await expect(dimensionCells.nth(index)).toHaveText(receipt.dimensions[key])
  }
  const values = row.locator('strong')
  await expect(values).toHaveCount(Object.keys(receipt.metrics).length)
  for (const [index, [key, text]] of Object.entries(receipt.metrics).entries()) {
    await values.nth(index).scrollIntoViewIfNeeded()
    await expect(values.nth(index)).toBeVisible()
    await expect(values.nth(index)).toHaveText(text)
    receipt.metrics[key] = (await values.nth(index).textContent()).trim()
  }
  return { receipt, row }
}

export function resourceHeatmapReceipt(receipts, nodeId) {
  const scalar = scalarResourceReceipts(receipts, nodeId).at(-1)
  const capacity = resourceReceipts(receipts, nodeId, filesystemMetrics).at(-1)
  if (!scalar || !capacity) throw new Error('heatmap resource receipts missing')
  const cell = point => {
    const value = currentResourceValue(point)
    return { data_state: point?.data_state || 'no_data', value, neutral: value === null,
      text: formatFieldPresentationValue(value, { unit: '%', precision: 2 }, 'zh-CN') }
  }
  return { scalar_index: receipts.indexOf(scalar), capacity_index: receipts.indexOf(capacity),
    scalars: Object.fromEntries(pressureMetrics.map(metric => [metric.key, cell(scalar.value.series.find(line => line.metric_key === metric.key)?.points[0])])),
    partitions: resourceDimensionRows(capacity.value).map(row => ({ dimensions: { ...row.dimensions }, ...cell(row.metrics['node.filesystem.used_percent']) })) }
}

// No extra request: collect the rendered heatmap against the page's existing observations.
export async function readResourceHeatmap(panel, receipts, nodeId, expect) {
  const receipt = resourceHeatmapReceipt(receipts, nodeId)
  const readCell = async (tile, cell) => {
    await tile.scrollIntoViewIfNeeded()
    await expect(tile).toBeVisible()
    await expect(tile).toHaveAttribute('data-state', cell.data_state)
    await expect(tile.locator('strong')).toHaveText(cell.text)
    const bar = tile.getByRole('progressbar')
    if (cell.value === null) {
      await expect(bar).toHaveCount(0)
      await expect(tile.locator('.heatmap-missing')).toBeVisible()
      expect(await tile.evaluate(element => element.style.borderTopColor)).toBe('')
    } else {
      await expect(bar).toHaveAttribute('aria-valuemin', '0')
      await expect(bar).toHaveAttribute('aria-valuemax', '100')
      await expect(bar).toHaveAttribute('aria-valuenow', String(cell.value))
      expect(await tile.evaluate(element => element.style.borderTopColor)).not.toBe('')
      cell.value = Number(await bar.getAttribute('aria-valuenow'))
    }
    cell.data_state = await tile.getAttribute('data-state')
    cell.text = (await tile.locator('strong').textContent()).trim()
  }
  for (const metric of pressureMetrics) await readCell(panel.getByTestId(`heatmap-${metric.name}`), receipt.scalars[metric.key])
  const partitions = panel.getByTestId('heatmap-partition')
  await expect(partitions).toHaveCount(receipt.partitions.length)
  for (const [index, cell] of receipt.partitions.entries()) {
    const tile = partitions.nth(index)
    await expect(tile.locator('h4')).toHaveText(cell.dimensions.mountpoint)
    await expect(tile.locator('.heatmap-mount')).toHaveText(`${cell.dimensions.device} · ${cell.dimensions.fstype}`)
    await readCell(tile, cell)
  }
  return receipt
}

// The complete current device frame is captured, never a sum or a selected row.
export function blockCapacityReceipt(receipts, nodeId) {
  const source = resourceReceipts(receipts, nodeId, blockCapacityMetrics).at(-1)
  if (!source) throw new Error('block capacity receipt missing')
  validateResourceResponse(source.value, nodeId, blockCapacityMetrics.map(metric => metric.key))
  if (source.value.collection?.state !== 'collecting' || source.value.collection.block_capacity !== 'available') throw new Error('block capacity frame unavailable')
  const rows = resourceDimensionRows(source.value)
  if (!rows.length || rows.length !== source.value.series.length || new Set(rows.map(row => row.dimensions.device)).size !== rows.length) throw new Error('block capacity frame incomplete')
  return { resource_index: receipts.indexOf(source), devices: rows.map(row => {
    const bytes = currentResourceValue(row.metrics['node.block.capacity_bytes'])
    if (!Number.isSafeInteger(bytes) || bytes <= 0) throw new Error('block capacity device unavailable')
    return { dimensions: { ...row.dimensions }, bytes, capacity_text: formatBytes(bytes, 2, 'zh-CN'),
      kind_text: zhCN.monitor.resources.block.kinds[row.dimensions.kind], state_text: zhCN.monitor.resources.states.valid }
  }) }
}

export async function readBlockCapacityTable(table, receipts, nodeId, expect) {
  const receipt = blockCapacityReceipt(receipts, nodeId)
  const rows = table.locator('.el-table__body tr')
  await expect(rows).toHaveCount(receipt.devices.length)
  for (const [index, device] of receipt.devices.entries()) {
    const row = rows.nth(index)
    await row.scrollIntoViewIfNeeded()
    const cells = row.locator('td .cell')
    for (const [column, value] of [device.dimensions.device, device.kind_text, device.capacity_text, device.state_text].entries()) {
      await expect(cells.nth(column)).toBeVisible()
      await expect(cells.nth(column)).toHaveText(value)
    }
    device.kind_text = (await cells.nth(1).textContent()).trim()
    device.capacity_text = (await cells.nth(2).textContent()).trim()
    device.state_text = (await cells.nth(3).textContent()).trim()
  }
  return receipt
}
