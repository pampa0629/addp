import { expect, test } from '@playwright/test'

const ENGINE = {
  id: 12,
  name: 'Business MinIO',
  engine_type: 'minio',
  lifecycle_state: 'active',
  connection_status: 'online'
}

const ROOT_LOCATOR = 'addp://engine/12/path/?type=root&node_id=200'
const DOC_LOCATOR = 'addp://engine/12/path/addp/doc?type=prefix&node_id=290'
const FILE_NAME = 'er-diagram-outdoor.md'
const FILE_LOCATOR = `addp://engine/12/path/addp/doc/${FILE_NAME}?type=object&item_id=52977`
const MARKDOWN = `# ADDP Entity Relationship Diagram

\`\`\`mermaid
erDiagram
  %% addp:document {"format":"addp.model.er/v2","scope":"domain","domain_code":"outdoor"}
  outdoor {
    string activity_id PK
  }
  member {
    string id PK
  }
  outdoor ||--o{ member : contains
\`\`\`
`

test('renders Mermaid blocks in Manager Markdown preview and keeps raw source opt-in', async ({ page }) => {
  await installMockBackend(page)
  await page.goto(`/data-explorer?locator=${encodeURIComponent(FILE_LOCATOR)}`)

  await expect(page.getByRole('heading', { name: 'ADDP Entity Relationship Diagram', exact: true })).toBeVisible()
  await expect(page.locator('.markdown-body .mermaid-diagram svg')).toBeVisible()
  await expect(page.locator('.markdown-body .mermaid-error')).toHaveCount(0)
  await expect(page.locator('.markdown-body')).not.toContainText('```mermaid')
  await expect(page.locator('.markdown-body')).not.toContainText('%% addp:document')

  await page.getByRole('button', { name: '原始文本', exact: true }).click()
  await expect(page.locator('.markdown-raw')).toContainText('```mermaid')
  await expect(page.locator('.markdown-raw')).toContainText('%% addp:document')
  await expect(page.getByRole('button', { name: 'MD 渲染', exact: true })).toBeVisible()
})

async function installMockBackend(page) {
  const fileNode = {
    id: FILE_LOCATOR,
    locator: FILE_LOCATOR,
    label: FILE_NAME,
    type: 'file',
    path: `addp/doc/${FILE_NAME}`,
    children: [],
    metadata: {
      item_id: 52977,
      data_type: 'document',
      format: 'markdown'
    }
  }
  const docNode = {
    id: DOC_LOCATOR,
    locator: DOC_LOCATOR,
    label: 'doc',
    type: 'prefix',
    path: 'addp/doc',
    hasChildren: true,
    loaded: true,
    children: [fileNode]
  }
  const rootNode = {
    id: ROOT_LOCATOR,
    locator: ROOT_LOCATOR,
    label: ENGINE.name,
    type: 'root',
    hasChildren: true,
    loaded: true,
    children: [docNode]
  }

  await page.addInitScript(() => {
    localStorage.setItem('addp-lang', 'zh-cn')
    localStorage.setItem('theme-mode', 'light')
  })

  await page.route('**/plugins/manifest.json', route => fulfillJSON(route, {
    scripts: ['/plugins/markdown-preview.js']
  }))
  await page.route('**/api/v1/**', async route => {
    const url = new URL(route.request().url())
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
      return fulfillJSON(route, { data: [ENGINE] })
    }
    if (path === `/api/v1/meta/resource-tree/${ENGINE.id}`) {
      return fulfillJSON(route, rootNode)
    }
    if (path === `/api/v1/meta/resource-tree/${ENGINE.id}/ancestors`) {
      return fulfillJSON(route, {
        target_locator: FILE_LOCATOR,
        ancestors: [rootNode, docNode, fileNode]
      })
    }
    if (path === `/api/v1/meta/resource-tree/${ENGINE.id}/node`) {
      return fulfillJSON(route, {
        parent_locator: DOC_LOCATOR,
        children: [fileNode]
      })
    }
    if (path === '/api/v1/manager/preview') {
      return fulfillJSON(route, {
        preview_type: 'object',
        data: {
          mode: 'object',
          object: {
            name: FILE_NAME,
            path: `addp/doc/${FILE_NAME}`,
            extension: 'md',
            size_bytes: Buffer.byteLength(MARKDOWN),
            content: {
              kind: 'markdown',
              frontend_renderer: 'markdown',
              preview_material: 'markdown',
              text: MARKDOWN,
              metadata: { format: 'markdown' }
            }
          }
        }
      })
    }

    return fulfillJSON(route, {})
  })
}

async function fulfillJSON(route, body, status = 200) {
  await route.fulfill({
    status,
    contentType: 'application/json',
    body: JSON.stringify(body)
  })
}
