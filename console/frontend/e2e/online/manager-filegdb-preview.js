import { expect } from '@playwright/test'

// Inspect rendered scalar cells, not the preview API payload or index row count.
export async function assertFileGDBChild(container, child, expectedRows, previousLabels = [], totalRows = expectedRows.length) {
  const meta = container.locator('.child-meta')
  const current = meta.getByRole('cell', { name: /^(当前页行数|Current Page Rows)$/ }).locator('+ td')
  const total = meta.getByRole('cell', { name: /^(总行数|Total Rows)$/ }).locator('+ td')
  await expect(container.locator('.child-select')).toContainText(child)
  await expect(current).toHaveText(String(expectedRows.length))
  await expect(total).toHaveText(String(totalRows))
  const table = container.locator('.child-preview .tabular-result-renderer')
  await expect(table).toHaveCount(1)
  const rows = table.locator('.el-table__body-wrapper tr.el-table__row')
  await expect(rows).toHaveCount(expectedRows.length)
  const headerCells = table.locator('.el-table__header-wrapper th .cell')
  for (const name of ['label', 'value']) await expect(headerCells.filter({ hasText: new RegExp('^' + name + '$') })).toBeVisible()
  const headers = (await headerCells.allTextContents()).map(text => text.trim())
  for (const name of ['label', 'value']) expect(headers.filter(header => header === name)).toHaveLength(1)
  const observed = []
  for (let i = 0; i < expectedRows.length; i++) {
    const cells = rows.nth(i).locator('td')
    const row = {}
    for (const name of ['label', 'value']) {
      const cell = cells.nth(headers.indexOf(name)).locator('.scalar-cell')
      await expect(cell).toBeVisible()
      await expect(cell).toHaveText(String(expectedRows[i][name]))
      row[name] = (await cell.innerText()).trim()
    }
    observed.push(row)
  }
  for (const label of previousLabels) await expect(table.getByText(label, { exact: true })).toHaveCount(0)
  return { child, current_rows: Number(await current.innerText()), total_rows: Number(await total.innerText()),
    rows: observed, previous_labels_absent: previousLabels }
}

export async function assertFileGDBPage(container, child, expectedRows, previousLabels, totalRows, page) {
  const observed = await assertFileGDBChild(container, child, expectedRows, previousLabels, totalRows)
  const pager = container.locator('.child-preview .data-pagination')
  const active = pager.locator('.el-pager li.is-active')
  await expect(active).toHaveText(String(page))
  await expect(pager.locator('.el-pagination__sizes')).toContainText('20')
  const lastPage = Math.ceil(totalRows / 20)
  if (page === 1) await expect(pager.locator('.btn-prev')).toBeDisabled()
  else await expect(pager.locator('.btn-prev')).toBeEnabled()
  if (page === lastPage) await expect(pager.locator('.btn-next')).toBeDisabled()
  else await expect(pager.locator('.btn-next')).toBeEnabled()
  return { ...observed, page: Number(await active.innerText()), page_size: 20,
    previous_disabled: await pager.locator('.btn-prev').isDisabled(), next_disabled: await pager.locator('.btn-next').isDisabled() }
}
