import { expect, test } from '@playwright/test'
import { assertFileGDBChild, assertFileGDBPage } from './online/manager-filegdb-preview.js'

const initial = [{ label: 'initial_points_0', value: 1 }, { label: 'initial_points_1', value: 2 }]
// Test the evidence reader against rendered DOM; this is not real Manager/T4 evidence.
async function render(page, rows, { child = 'initial_points', total = 2, number = 1, columns = ['fid', 'value', 'label'] } = {}) {
  await page.setContent(`<div class="container-preview"><div class="child-select">${child}</div>
    <table class="child-meta"><tr><td>当前页行数</td><td>${rows.length}</td><td>总行数</td><td>${total}</td></tr></table>
    <div class="child-preview"><div class="tabular-result-renderer">
      <table class="el-table__header-wrapper"><tr>${columns.map(key => `<th><div class="cell">${key}</div></th>`).join('')}</tr></table>
      <table class="el-table__body-wrapper"><tbody>${rows.map(row => `<tr class="el-table__row">${columns.map(key => `<td><span class="scalar-cell">${row[key] ?? 1}</span></td>`).join('')}</tr>`).join('')}</tbody></table>
    </div><div class="data-pagination"><span class="el-pagination__sizes">20/page</span>
      <button class="btn-prev" ${number === 1 ? 'disabled' : ''}>Previous</button>
      <ul class="el-pager"><li class="is-active">${number}</li></ul>
      <button class="btn-next" ${number === Math.ceil(total / 20) ? 'disabled' : ''}>Next</button>
    </div></div></div>`)
  return page.locator('.container-preview')
}

test('preview evidence reads actual columns and the switched child without stale rows', async ({ page }) => {
  const container = await render(page, initial)
  expect(await assertFileGDBChild(container, 'initial_points', initial)).toEqual({ child: 'initial_points',
    current_rows: 2, total_rows: 2, rows: initial.map(row => ({ label: row.label, value: String(row.value) })), previous_labels_absent: [] })
  const added = [{ label: 'added_points_0', value: 1 }]
  await render(page, added, { child: 'added_points', total: 1 })
  expect((await assertFileGDBChild(container, 'added_points', added, initial.map(row => row.label))).rows)
    .toEqual([{ label: 'added_points_0', value: '1' }])
})

for (const failure of ['empty rows with index total two', 'wrong value beside correct label', 'stale initial rows after switch']) {
  test(`preview evidence rejects ${failure}`, async ({ page }) => {
    const rows = failure.startsWith('empty') ? [] : failure.startsWith('wrong')
      ? [{ ...initial[0], value: 2 }, initial[1]] : initial
    const switched = failure.startsWith('stale')
    const container = await render(page, rows, { child: switched ? 'added_points' : 'initial_points' })
    await expect(assertFileGDBChild(container, switched ? 'added_points' : 'initial_points',
      switched ? [{ label: 'added_points_0', value: 1 }] : initial)).rejects.toThrow()
  })
}

const pagingRows = Array.from({ length: 23 }, (_, i) => ({ label: `paging_points_${i}`, value: i + 1 }))
test('pagination evidence distinguishes the short second page from total rows and records reset', async ({ page }) => {
  const last = pagingRows.slice(20), old = pagingRows.slice(0, 20).map(row => row.label)
  const container = await render(page, last, { child: 'paging_points', total: 23, number: 2 })
  const observed = await assertFileGDBPage(container, 'paging_points', last, old, 23, 2)
  expect(observed).toMatchObject({ current_rows: 3, total_rows: 23, page: 2, page_size: 20, previous_disabled: false, next_disabled: true })
  const small = [{ label: 'added_points_0', value: 1 }]
  await render(page, small, { child: 'added_points', total: 1 })
  expect(await assertFileGDBPage(container, 'added_points', small, last.map(row => row.label), 1, 1))
    .toMatchObject({ page: 1, previous_disabled: true, next_disabled: true })
})
for (const failure of ['stale first page', 'wrong total', 'page not reset']) {
  test(`pagination evidence rejects ${failure}`, async ({ page }) => {
    const small = failure === 'page not reset'
    const rows = small ? [{ label: 'added_points_0', value: 1 }] : failure === 'stale first page' ? pagingRows.slice(0, 20) : pagingRows.slice(20)
    const child = small ? 'added_points' : 'paging_points'
    const container = await render(page, rows, { child, total: small ? 1 : failure === 'wrong total' ? 3 : 23, number: 2 })
    await expect(assertFileGDBPage(container, child, small ? rows : pagingRows.slice(20), [], small ? 1 : 23, small ? 1 : 2)).rejects.toThrow()
  })
}
