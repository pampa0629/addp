import { expect, test } from '@playwright/test'

const settingsPath = '/api/v1/manager/settings'
const policyPath = `${settingsPath}/embedding`
const embeddingBinding = `${settingsPath}/inference-bindings/semantic_search_embedding`
const segmentBinding = `${settingsPath}/inference-bindings/image_segmentation`
const policy = { version: 7, max_distance: 0.42, max_file_size_mb: 24, batch_concurrency: 3 }
const profiles = [
  { id: 'embedding-profile', name: 'Embedding Profile', code: 'embedding', version: 1, status: 'active', model_deployment_id: 'embedding-deployment' },
  { id: 'segment-profile', name: 'Segment Profile', status: 'active', model_deployment_id: 'segment-deployment' }
]
const deployments = [
  { id: 'embedding-deployment', status: 'active', dimension: 2560, operations: ['embedding'], modalities: ['text', 'image'] },
  { id: 'segment-deployment', status: 'active', operations: ['segmentation'], modalities: ['image'] }
]

async function openSettings(page, { writable = false, failRead = false } = {}) {
  const context = { type: 'platform' }
  const permissions = ['manager.configuration.read', 'inference.profile.read', 'inference.deployment.read']
  if (writable) permissions.push('manager.configuration.update')
  const reads = new Map([
    [policyPath, policy],
    [embeddingBinding, { version: 4, model_profile_id: profiles[0].id }],
    [segmentBinding, { version: 9, model_profile_id: profiles[1].id }],
    ['/api/v1/inference/model-profiles', { data: profiles }],
    ['/api/v1/inference/model-deployments', { data: deployments }]
  ])
  const writes = []
  await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  await page.route('**/api/v1/**', route => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    if (path === '/api/v1/system/refresh') return route.fulfill({ json: { access_token: 'manager-settings-token', expires_in: 3600 } })
    if (path === '/api/v1/system/users/me') return route.fulfill({ json: { id: '31', display_name: 'settings-user' } })
    if (path === '/api/v1/system/auth/context') {
      return route.fulfill({ json: { context, authorization: { role_assignments: [{ scope: context, permissions }] } } })
    }
    if (request.method() === 'GET' && reads.has(path)) {
      if (path === policyPath && failRead) return route.fulfill({ status: 503, json: { error: 'settings_unavailable' } })
      return route.fulfill({ json: reads.get(path) })
    }
    if (path.startsWith(`${settingsPath}/`) && request.method() !== 'GET') {
      const payload = request.postDataJSON()
      writes.push({ path, method: request.method(), payload })
      if (writable && request.method() === 'PUT' && reads.has(path)) {
        const saved = { ...payload, version: payload.version + 1 }
        reads.set(path, saved)
        return route.fulfill({ json: saved })
      }
    }
    return route.fulfill({ status: 403, json: { error: 'unexpected_request' } })
  })
  await page.goto('/settings/embedding')
  return writes
}

test('platform reader can view both settings tabs with no editable fields or writes', async ({ page }) => {
  const writes = await openSettings(page)
  const panel = page.getByRole('tabpanel')
  const save = panel.getByRole('button', { name: '保存配置', exact: true })
  const distance = panel.getByRole('spinbutton', { name: '最大余弦距离' })
  await expect(distance).toHaveValue('0.42')
  await expect(distance).toBeDisabled()
  await expect(panel.getByText('Embedding Profile · embedding · v1', { exact: true })).toBeVisible()
  await expect(panel.getByRole('combobox')).toHaveCount(0)
  await expect(save).toBeDisabled()

  await page.getByRole('tab', { name: '影像分割', exact: true }).click()
  await expect(panel.getByText('Segment Profile', { exact: true })).toBeVisible()
  await expect(panel.getByRole('combobox')).toHaveCount(0)
  await expect(save).toBeDisabled()
  expect(writes).toEqual([])
})

test('platform editor can save embedding policy and both scenario bindings after reading', async ({ page }) => {
  const writes = await openSettings(page, { writable: true })
  const panel = page.getByRole('tabpanel')
  const save = panel.getByRole('button', { name: '保存配置', exact: true })
  const distance = panel.getByRole('spinbutton', { name: '最大余弦距离' })
  await expect(distance).toHaveValue('0.42')
  await distance.fill('0.63')
  await save.click()
  await expect(distance).toHaveValue('0.63')
  await expect(page.getByText('绑定版本 5', { exact: true })).toBeVisible()
  expect(writes).toHaveLength(2)
  expect(writes).toEqual(expect.arrayContaining([
    { path: policyPath, method: 'PUT', payload: { ...policy, max_distance: 0.63 } },
    { path: embeddingBinding, method: 'PUT', payload: { version: 4, model_profile_id: profiles[0].id } }
  ]))

  await page.getByRole('tab', { name: '影像分割', exact: true }).click()
  await expect(panel.getByRole('combobox')).toBeEnabled()
  await save.click()
  await expect.poll(() => writes.slice(2)).toEqual([
    { path: segmentBinding, method: 'PUT', payload: { version: 9, model_profile_id: profiles[1].id } }
  ])
})

test('a failed settings read prevents a platform editor from saving either tab', async ({ page }) => {
  const writes = await openSettings(page, { writable: true, failRead: true })
  const save = page.getByRole('button', { name: '保存配置', exact: true })
  await expect(page.getByText('加载向量化配置失败', { exact: true })).toBeVisible()
  await expect(page.getByText(/^绑定版本 /)).toHaveCount(0)
  await expect(page.getByRole('spinbutton')).toHaveCount(0)
  await expect(save).toHaveCount(0)
  await page.getByRole('tab', { name: '影像分割', exact: true }).click()
  await expect(page.getByRole('combobox')).toHaveCount(0)
  await expect(save).toHaveCount(0)
  expect(writes).toEqual([])
})
