import { expect, test } from '@playwright/test'
import { createHash } from 'node:crypto'
import { writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { browserAPIFromResponse, json, login } from './transfer-browser-support.js'

test('ordinary user pages and downloads both single-file ORC sources', async ({ page }) => {
  test.setTimeout(240_000)
  const env = process.env
  const context = JSON.parse(env.ADDP_ONLINE_MANAGER_ORC_JSON || '{}')
  expect(context.run_id).toBe(env.ADDP_ONLINE_TEST_RUN_ID)
  expect(context.cases.map(source => source.source_case)).toEqual(['minio', 'nfs'])
  expect(context.nested_cases.map(source => source.source_case)).toEqual(['minio', 'nfs'])
  const expected = { principalID: context.identity.principal_id, tenantID: context.identity.tenant_id,
    permissions: new Set(context.identity.permissions_verified) }
  expect(expected.permissions.size).toBe(11)
  const outcomes = []
  const nestedOutcomes = []
  let passed = false
  try {
    await login(page, env.ADDP_ONLINE_TEST_USER_USERNAME, env.ADDP_ONLINE_TEST_USER_PASSWORD, '/')
    for (const source of [...context.cases, ...context.nested_cases.map(source => ({ ...source, nested: true }))]) {
      const observed = page.waitForResponse(response => {
        const url = new URL(response.url())
        return url.pathname === '/api/v1/manager/preview' && url.searchParams.get('locator') === source.locator
      })
      observed.catch(() => {})
      await page.goto('/manager/data-explorer?locator=' + encodeURIComponent(source.locator))
      const response = await observed
      expect(response.status()).toBe(200)
      const api = await browserAPIFromResponse(response, env.GATEWAY_URL, expected)
      try {
        const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
        await expect(frame.getByRole('columnheader', { name: 'id', exact: true })).toBeVisible()
        await expect(frame.getByText('9007199254740992', { exact: true }).first()).toBeVisible()
        const next = page.waitForResponse(response => {
          const url = new URL(response.url())
          return url.pathname === '/api/v1/manager/preview' && url.searchParams.get('locator') === source.locator && url.searchParams.get('page') === '2'
        })
        next.catch(() => {})
        await frame.locator('.el-pagination .btn-next').click()
        const second = await json(await next, 'ORC actual page button')
        expect(second.preview_type).toBe('table')
        expect(second.data.page).toBe(2)
        expect(second.data.total).toBe(source.nested ? 43 : 2503)
        const size = second.data.page_size
        if (source.nested) {
          await expect(frame.getByRole('columnheader', { name: 'profile', exact: true })).toBeVisible()
          for (const [offset, row] of second.data.rows.entries()) {
            const i = size + offset
            const number = (9007199254740992n + BigInt(i)).toString()
            const profile = i % 3 === 0 ? null : (i % 5 === 0 ? { counter: null, amount: null, blob: null } : { counter: number, amount: '12345678901234567890.' + String(i).padStart(9, '0'), blob: i % 2 === 0 ? '' : 'AP8=' })
            const samples = i % 7 === 0 ? null : (i % 5 === 0 ? [] : [{ value: number, label: null }, { value: null, label: '嵌套' }])
            const matrix = i % 7 === 0 ? null : (i % 5 === 0 ? [] : [[number, null], [], null])
            expect(row).toEqual({ id: number, profile, samples, matrix })
          }
          const detailIndex = second.data.rows.findIndex(row => row.profile?.counter && row.samples?.length && row.matrix?.length)
          expect(detailIndex).toBeGreaterThanOrEqual(0)
          const detailRow = frame.locator('.el-table__body tr').nth(detailIndex)
          for (const [buttonIndex, field] of ['profile', 'samples', 'matrix'].entries()) {
            await detailRow.locator('button.structured-cell').nth(buttonIndex).click()
            const dialog = frame.getByRole('dialog', { name: field, exact: true })
            await expect(dialog).toBeVisible()
            await expect(dialog.locator('.structured-value-json')).toHaveText(JSON.stringify(second.data.rows[detailIndex][field], null, 2))
            if (field === 'profile') await page.screenshot({ path: resolve(env.ADDP_ONLINE_ARTIFACT_DIR, 'orc-nested-' + source.source_case + '-profile.png') })
            await dialog.locator('.el-dialog__headerbtn').click()
            await expect(dialog).not.toBeVisible()
          }
        }
        expect(second.data.rows[0].id).toBe((9007199254740992n + BigInt(size)).toString())
        await expect(frame.getByText(second.data.rows[0].id, { exact: true }).first()).toBeVisible()
        const downloadReady = page.waitForEvent('download')
        await frame.getByRole('button', { name: /^(下载|Download)$/ }).click()
        const download = await downloadReady
        try {
          expect(download.suggestedFilename().toLowerCase()).toMatch(/\.orc$/)
          const stream = await download.createReadStream()
          const chunks = []
          for await (const chunk of stream) chunks.push(chunk)
          const raw = Buffer.concat(chunks)
          const authenticated = await api.get('/api/v1/manager/downloads/file?locator=' + encodeURIComponent(source.locator))
          expect(authenticated.status()).toBe(200)
          expect(await authenticated.body()).toEqual(raw)
          await page.screenshot({ path: resolve(env.ADDP_ONLINE_ARTIFACT_DIR, 'orc-' + (source.nested ? 'nested-' : '') + source.source_case + '-page2.png') })
          ;(source.nested ? nestedOutcomes : outcomes).push({ nested_values_verified: source.nested === true, structured_dialogs_verified: source.nested === true, source_case: source.source_case, page_button_verified: true, original_button_download_verified: true,
            sha256: createHash('sha256').update(raw).digest('hex'), size_bytes: raw.length })
        } finally { await download.delete() }
      } finally { await api.dispose() }
    }
    passed = true
  } finally {
    writeFileSync(resolve(env.ADDP_ONLINE_ARTIFACT_DIR, 'manager-orc-browser.json'), JSON.stringify({
      schema_version: 'addp.manager-orc-browser/v1', result: passed ? 'passed' : 'failed', run_id: context.run_id, cases: outcomes, nested_cases: nestedOutcomes,
    }, null, 2) + '\n')
  }
})
