import { expect, test } from '@playwright/test'

const TIDB_ENGINE = {
  id: 25,
  name: 'Business TiDB',
  engine_type: 'tidb',
  lifecycle_state: 'active',
  connection_status: 'online'
}

const ROOT_LOCATOR = 'addp://engine/25/path/?type=server&node_id=377'
const BUSINESS_LOCATOR = 'addp://engine/25/path/business?type=database&node_id=378'

test('expands an unloaded TiDB database after refreshing the catalog root', async ({ page }) => {
  const backend = await installMockBackend(page)
  await page.goto('/data-explorer')

  await treeNodeContent(page, TIDB_ENGINE.name).click()
  await expect(treeNodeContent(page, 'business')).toBeVisible()

  const rootContent = treeNodeContent(page, TIDB_ENGINE.name)
  await rootContent.getByTitle('基础刷新：重新发现此节点下的新资源').click()

  await expect.poll(() => backend.refreshRequests).toEqual([ROOT_LOCATOR])
  const businessContent = treeNodeContent(page, 'business')
  const expandIcon = businessContent.locator(':scope > .el-tree-node__expand-icon')
  await expect(expandIcon).toHaveClass(/is-leaf/)
  await expect(treeNodeContent(page, 'customers')).toHaveCount(0)

  await expandIcon.click()

  await expect.poll(() => backend.childRequests).toContain(BUSINESS_LOCATOR)
  await expect(treeNodeContent(page, 'addp_engine_probe')).toBeVisible()
  await expect(treeNodeContent(page, 'customers')).toBeVisible()
  await expect(treeNodeContent(page, 'orders')).toBeVisible()
})

function treeNodeContent(scope, label) {
  return scope.locator('.el-tree-node__content').filter({
    hasText: new RegExp(`^\\s*${escapeRegExp(label)}\\s*$`)
  }).first()
}

function escapeRegExp(value) {
  return String(value).replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

async function installMockBackend(page) {
  const state = {
    refreshRequests: [],
    childRequests: []
  }

  await page.addInitScript(() => {
    localStorage.setItem('addp-lang', 'zh-cn')
    localStorage.setItem('theme-mode', 'light')
  })

  await page.route('**/plugins/manifest.json', route => fulfillJSON(route, { scripts: [] }))
  await page.route('**/api/v1/**', async route => {
    const request = route.request()
    const url = new URL(request.url())
    const path = url.pathname

    if (path === '/api/v1/system/refresh') {
      return fulfillJSON(route, { access_token: 'manager-e2e-token', expires_in: 3600 })
    }
    if (path === '/api/v1/system/users/me') {
      return fulfillJSON(route, { id: 1, username: 'manager-e2e' })
    }
    if (path === '/api/v1/system/auth/context') {
      return fulfillJSON(route, {
        context: { type: 'tenant' },
        authorization: { role_assignments: [{ permissions: [] }] }
      })
    }
    if (path === '/api/v1/manager/engines') {
      return fulfillJSON(route, { data: [TIDB_ENGINE] })
    }
    if (path === `/api/v1/meta/resource-tree/${TIDB_ENGINE.id}`) {
      return fulfillJSON(route, shallowTree())
    }
    if (path === `/api/v1/meta/resource-tree/${TIDB_ENGINE.id}/refresh` && request.method() === 'POST') {
      state.refreshRequests.push(url.searchParams.get('locator') || '')
      return fulfillJSON(route, {
        run: {
          execution_id: 'tidb-root-refresh-execution-1',
          status: 'pending',
          progress: 10,
          current_step: 'queued'
        }
      }, 202)
    }
    if (path === '/api/v1/meta/executions/tidb-root-refresh-execution-1') {
      return fulfillJSON(route, {
        execution_id: 'tidb-root-refresh-execution-1',
        status: 'success',
        progress: 100,
        current_step: 'completed'
      })
    }
    if (path === `/api/v1/meta/resource-tree/${TIDB_ENGINE.id}/node`) {
      const locator = url.searchParams.get('locator') || ''
      state.childRequests.push(locator)
      return fulfillJSON(route, locator === BUSINESS_LOCATOR ? businessNode(true) : shallowTree())
    }

    return fulfillJSON(route, {})
  })

  return state
}

function shallowTree() {
  return {
    id: ROOT_LOCATOR,
    locator: ROOT_LOCATOR,
    label: TIDB_ENGINE.name,
    type: 'server',
    hasChildren: true,
    children: [businessNode(false)]
  }
}

function businessNode(withChildren) {
  return {
    id: BUSINESS_LOCATOR,
    locator: BUSINESS_LOCATOR,
    label: 'business',
    type: 'database',
    hasChildren: true,
    metadata: { item_count: 3 },
    children: withChildren
      ? ['addp_engine_probe', 'customers', 'orders'].map((label, index) => ({
          id: `addp://engine/25/path/business/${label}?type=table&item_id=${901 + index}`,
          locator: `addp://engine/25/path/business/${label}?type=table&item_id=${901 + index}`,
          label,
          type: 'table',
          hasChildren: false,
          children: []
        }))
      : []
  }
}

async function fulfillJSON(route, body, status = 200) {
  await route.fulfill({
    status,
    contentType: 'application/json',
    body: JSON.stringify(body)
  })
}
