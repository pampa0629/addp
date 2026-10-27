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
  const expected = { principalID: context.identity.principal_id, tenantID: context.identity.tenant_id,
    permissions: new Set(context.identity.permissions_verified) }
  expect(expected.permissions.size).toBe(11)
  const outcomes = []
  let passed = false
  try {
    await login(page, env.ADDP_ONLINE_TEST_USER_USERNAME, env.ADDP_ONLINE_TEST_USER_PASSWORD, '/')
    for (const source of context.cases) {
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
        expect(second.data.total).toBe(2503)
        const size = second.data.page_size
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
          await page.screenshot({ path: resolve(env.ADDP_ONLINE_ARTIFACT_DIR, 'orc-' + source.source_case + '-page2.png') })
          outcomes.push({ source_case: source.source_case, page_button_verified: true, original_button_download_verified: true,
            sha256: createHash('sha256').update(raw).digest('hex'), size_bytes: raw.length })
        } finally { await download.delete() }
      } finally { await api.dispose() }
    }
    passed = true
  } finally {
    writeFileSync(resolve(env.ADDP_ONLINE_ARTIFACT_DIR, 'manager-orc-browser.json'), JSON.stringify({
      schema_version: 'addp.manager-orc-browser/v1', result: passed ? 'passed' : 'failed', run_id: context.run_id, cases: outcomes,
    }, null, 2) + '\n')
  }
})
