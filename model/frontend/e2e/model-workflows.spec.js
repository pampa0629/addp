import { expect, test } from '@playwright/test'

const DOMAINS = [
  { id: 1, name: '客户域' },
  { id: 2, name: '户外域' }
]

const ENTITIES = [
  {
    id: 7,
    tenant_id: 1,
    domain_id: 2,
    name: '活动',
    code: 'outdoor',
    description: '',
    status: 'draft',
    version: 1,
    created_at: '2026-08-11T10:50:11+08:00'
  },
  {
    id: 8,
    tenant_id: 1,
    domain_id: 1,
    name: '客户',
    code: 'customer',
    description: '',
    status: 'draft',
    version: 1,
    created_at: '2026-08-11T11:00:00+08:00'
  }
]

const LOGICAL_TABLE = {
  id: 2,
  tenant_id: 1,
  domain_id: 1,
  name: '省份',
  code: 'dwd_province',
  table_type: 'dimension',
  layer: 'dwd',
  status: 'approved',
  scd_type: 0,
  description: '',
  version: 1,
  materialization: {
    schema_name: 'public',
    table_name: 'dwd_province',
    partition_by: '',
    partition_type: 'range'
  }
}

const DEFAULT_PERMISSIONS = [
  'model.entity.read',
  'model.logical_model.read'
]

test('shows an explicit permission error instead of an empty entity page after a 403', async ({ page }) => {
  const backend = await installMockBackend(page, { forbidEntityList: true })

  await page.goto('/entities')

  const permissionAlert = page.getByRole('alert').filter({
    hasText: '当前账号没有访问权限，请联系租户管理员分配对应角色或权限后重试。'
  })
  await expect(permissionAlert).toBeVisible()
  await expect(permissionAlert.getByRole('button', { name: '重试', exact: true })).toBeVisible()
  await expect(page.getByRole('table')).toHaveCount(0)
  await expect.poll(() => backend.getEntityListRequests()).toBe(1)
})

test('preserves the business-domain URL state across entity detail navigation', async ({ page }) => {
  await installMockBackend(page)

  await page.goto('/entities?domain_id=2')
  await expect(page).toHaveURL(/\/entities\?domain_id=2$/)
  await expect(page.getByText('户外域', { exact: true }).first()).toBeVisible()
  await expect(page.getByRole('cell', { name: '活动', exact: true })).toBeVisible()
  await expect(page.getByRole('cell', { name: '客户', exact: true })).toHaveCount(0)
  await expect(page.locator('.el-pagination').getByText('20条/页', { exact: true })).toBeVisible()

  await page.getByRole('button', { name: '设计', exact: true }).click()
  await expect(page).toHaveURL(/\/entities\/7\?domain_id=2$/)
  await expect(page.getByRole('textbox', { name: '实体名称', exact: true })).toHaveValue('活动')

  await page.getByRole('button', { name: '返回', exact: true }).click()
  await expect(page).toHaveURL(/\/entities\?domain_id=2$/)
  await expect(page.getByText('户外域', { exact: true }).first()).toBeVisible()
  await expect(page.getByRole('cell', { name: '活动', exact: true })).toBeVisible()
  await expect(page.getByRole('cell', { name: '客户', exact: true })).toHaveCount(0)
})

test.describe('DDL preview', () => {
  test.use({ viewport: { width: 620, height: 560 }, colorScheme: 'dark' })

  test('renders generated DDL in a themed dialog that stays inside a narrow viewport', async ({ page }) => {
    const backend = await installMockBackend(page, { theme: 'dark' })

    await page.goto('/logical-tables/2?domain_id=1')
    await expect(page.getByRole('button', { name: '预览 DDL', exact: true })).toBeVisible()
    await page.getByRole('button', { name: '预览 DDL', exact: true }).click()

    const dialog = page.locator('.el-dialog.addp-dialog:visible')
    await expect(dialog).toHaveCount(1)
    await expect(dialog.getByText('CREATE TABLE "public"."dwd_province"', { exact: false })).toBeVisible()
    await expect(dialog.locator('.ddl-wrapper')).toHaveCSS('background-color', 'rgb(20, 20, 20)')
    await expectDialogWithinViewport(page, dialog)
    await expect.poll(() => backend.getDDLRequests()).toEqual([{
      materialization: {
        schema_name: 'public',
        table_name: 'dwd_province',
        partition_by: '',
        partition_type: 'range'
      }
    }])
  })
})

async function installMockBackend(page, options = {}) {
  let entityListRequests = 0
  const ddlRequests = []
  const permissions = options.permissions || DEFAULT_PERMISSIONS

  await page.addInitScript(({ theme }) => {
    localStorage.setItem('addp-lang', 'zh-cn')
    localStorage.setItem('theme-mode', theme || 'light')
  }, { theme: options.theme })

  await page.route('**/api/v1/**', async route => {
    const request = route.request()
    const url = new URL(request.url())
    const path = url.pathname

    if (path === '/api/v1/system/refresh') {
      return fulfillJSON(route, { access_token: 'model-e2e-token', expires_in: 3600 })
    }
    if (path === '/api/v1/system/users/me') {
      return fulfillJSON(route, { id: 1, username: 'model-e2e' })
    }
    if (path === '/api/v1/system/auth/context') {
      return fulfillJSON(route, {
        context: { type: 'tenant' },
        authorization: { role_assignments: [{ permissions }] }
      })
    }
    if (path === '/api/v1/standard/domains') return fulfillJSON(route, DOMAINS)
    if (path === '/api/v1/standard/elements') return fulfillJSON(route, { data: [], total: 0 })
    if (path === '/api/v1/model/dw-layers') {
      return fulfillJSON(route, [{ layer_code: 'dwd', layer_name: '明细层' }])
    }
    if (path === '/api/v1/model/entities/7/attributes') return fulfillJSON(route, [])
    if (path === '/api/v1/model/entities/7') return fulfillJSON(route, ENTITIES[0])
    if (path === '/api/v1/model/entity-relations') return fulfillJSON(route, [])
    if (path === '/api/v1/model/entities' && request.method() === 'GET') {
      entityListRequests += 1
      if (options.forbidEntityList) {
        return fulfillJSON(route, {
          error: '当前账号没有访问权限',
          error_code: 'permission_denied'
        }, 403)
      }
      const domainID = Number(url.searchParams.get('domain_id'))
      const data = domainID ? ENTITIES.filter(entity => entity.domain_id === domainID) : ENTITIES
      return fulfillJSON(route, { data, total: data.length })
    }
    if (path === '/api/v1/model/logical-tables/2/fields') return fulfillJSON(route, [])
    if (path === '/api/v1/model/logical-tables/2/preview-ddl' && request.method() === 'POST') {
      ddlRequests.push(request.postDataJSON())
      return fulfillJSON(route, {
        ddl: 'CREATE TABLE "public"."dwd_province" (\n  "province" TEXT,\n  "code" TEXT,\n  PRIMARY KEY ("code")\n);'
      })
    }
    if (path === '/api/v1/model/logical-tables/2') return fulfillJSON(route, LOGICAL_TABLE)

    return fulfillJSON(route, {
      error: `Unexpected E2E request: ${request.method()} ${path}`
    }, 404)
  })

  return {
    getDDLRequests: () => structuredClone(ddlRequests),
    getEntityListRequests: () => entityListRequests
  }
}

async function fulfillJSON(route, body, status = 200) {
  await route.fulfill({
    status,
    contentType: 'application/json',
    body: JSON.stringify(body)
  })
}

async function expectDialogWithinViewport(page, dialog) {
  await expect(dialog).toBeVisible()
  await expect.poll(() => dialog.evaluate(element => {
    const animations = []
    let current = element
    while (current && current !== document.body) {
      animations.push(...current.getAnimations({ subtree: false }))
      current = current.parentElement
    }
    return animations.every(animation => animation.playState === 'finished')
  })).toBe(true)

  const box = await dialog.boundingBox()
  const viewport = page.viewportSize()
  expect(box).not.toBeNull()
  expect(viewport).not.toBeNull()
  expect(box.x).toBeGreaterThanOrEqual(12)
  expect(box.y).toBeGreaterThanOrEqual(0)
  expect(box.x + box.width).toBeLessThanOrEqual(viewport.width - 12)
  expect(box.y + box.height).toBeLessThanOrEqual(viewport.height)
  expect(await page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth)).toBe(false)
}
