import { expect, test } from '@playwright/test'

const profile = {
  id: 'profile-visible',
  name: '验收向量模型',
  upstream_model: 'embedding-example-v1'
}

const task = {
  id: 7,
  name: '模型标签验收任务',
  enabled: true,
  target: {
    engine_id: 9,
    scope: 'item',
    item_id: 42,
    locator: 'addp://engine/9/path/public/sample?type=table&item_id=42'
  },
  config: {
    embedding: {
      model_profile_id: profile.id,
      profile_version: 1,
      dimension: 1536
    }
  }
}

test('read-only vectorization page displays a model label without model management access', async ({ page }) => {
  const requests = []
  const pageErrors = []
  page.on('pageerror', error => pageErrors.push(error.message))
  await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  await page.route('**/plugins/manifest.json', route => json(route, { scripts: [] }))
  await page.route('**/api/v1/**', route => {
    const path = new URL(route.request().url()).pathname
    requests.push(path)
    if (path === '/api/v1/system/refresh') return json(route, { access_token: 'manager-label-e2e-token', expires_in: 3600 })
    if (path === '/api/v1/system/users/me') return json(route, { id: 32, username: 'label-reader' })
    if (path === '/api/v1/system/auth/context') {
      return json(route, {
        context: { type: 'tenant', tenant_id: '1' },
        authorization: {
          role_assignments: [{
            scope: { type: 'tenant', tenant_id: '1' },
            permissions: ['manager.derived_artifact.read', 'manager.data_item.read', 'inference.model_label.read']
          }]
        }
      })
    }
    if (path === '/api/v1/manager/engines') {
      return json(route, { data: [{ id: 9, name: 'Business PostgreSQL', engine_type: 'postgresql' }] })
    }
    if (path === '/api/v1/inference/model-labels') return json(route, [profile])
    if (path === '/api/v1/manager/embedding_tasks') return json(route, { data: [task], total: 1, page: 1, page_size: 20 })
    if (path === '/api/v1/manager/embeddings') return json(route, { data: [], total: 0, page: 1, page_size: 20 })
    return json(route, {}, 404)
  })

  await page.goto('/tasks/embedding')
  const taskRow = page.getByRole('row', { name: /模型标签验收任务/ })
  await expect(taskRow).toBeVisible()
  await taskRow.getByRole('button', { name: '详情' }).click()
  await expect(page.getByRole('dialog')).toContainText('验收向量模型 · embedding-example-v1')
  await expect(page.getByRole('button', { name: '创建任务' })).toHaveCount(0)
  expect(requests).toContain('/api/v1/inference/model-labels')
  expect(requests).not.toContain('/api/v1/inference/model-profiles')
  expect(requests).not.toContain('/api/v1/inference/model-deployments')
  expect(pageErrors).toEqual([])
})

function json(route, body, status = 200) {
  return route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) })
}
