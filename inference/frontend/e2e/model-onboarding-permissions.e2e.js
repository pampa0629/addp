import { expect, test } from '@playwright/test'

const provider = {
  id: 'fixture-provider', name: 'Fixture provider', endpoint: 'https://provider.example.test/v1',
  scope_type: 'tenant', adapter_type: 'openai_compatible', status: 'active', credential: { configured: false }
}
const template = {
  code: 'custom-openai-compatible', category: 'custom', adapter_type: 'openai_compatible',
  model_discovery: 'openai_models', credential_mode: 'optional', suggested_models: []
}
const modelID = 'manual-chat-model'

async function openSettings(page, canDiscover) {
  const calls = []
  const unexpected = []
  const deployments = []
  const profiles = []
  const context = { type: 'tenant', tenant_id: '3' }
  const permissions = [
    'inference.provider.read', 'inference.deployment.read', 'inference.profile.read',
    'inference.deployment.create', 'inference.profile.create'
  ]
  if (canDiscover) permissions.push('inference.deployment.execute')
  await page.addInitScript(() => localStorage.setItem('addp-lang', 'en'))
  await page.route('**/api/v1/**', route => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    const method = request.method()
    const reply = json => route.fulfill({ json })
    if (path === '/api/v1/system/refresh' && method === 'POST') return reply({ access_token: 'isolated-inference-token', expires_in: 300 })
    if (path === '/api/v1/system/users/me' && method === 'GET') return reply({ id: '32', display_name: 'Fixture user' })
    if (path === '/api/v1/system/auth/context' && method === 'GET') {
      return reply({ context, authorization: { role_assignments: [{ scope: context, permissions }] } })
    }
    if (path === '/api/v1/inference/provider-templates' && method === 'GET') return reply([template])
    if (path === '/api/v1/inference/provider-connections' && method === 'GET') return reply({ data: [provider] })
    if (path === '/api/v1/inference/model-deployments' && method === 'GET') return reply({ data: deployments })
    if (path === '/api/v1/inference/model-profiles' && method === 'GET') return reply({ data: profiles })
    if (method === 'POST' && path.startsWith('/api/v1/inference/')) {
      calls.push({ path, data: request.postDataJSON() })
      if (path === '/api/v1/inference/provider-connections/fixture-provider/discover-models' && canDiscover) {
        return reply({ models: [{ id: 'discovered-chat-model' }] })
      }
      if (path === '/api/v1/inference/model-deployments') {
        const deployment = { ...request.postDataJSON(), id: 'fixture-deployment' }
        deployments.push(deployment)
        return reply(deployment)
      }
      if (path === '/api/v1/inference/model-profiles') {
        const profile = { ...request.postDataJSON(), id: 'fixture-profile' }
        profiles.push(profile)
        return reply(profile)
      }
    }
    unexpected.push({ method, path })
    return route.fulfill({ status: 403, json: { error: 'unexpected_request' } })
  })
  await page.goto('/module-ui/inference/settings/models')
  await page.getByRole('button', { name: 'Add model', exact: true }).click()
  return { calls, unexpected, dialog: page.getByRole('dialog') }
}

test('a tenant model creator without execution permission manually creates a deployment and profile without discovery', async ({ page }) => {
  const { calls, unexpected, dialog } = await openSettings(page, false)
  await expect(dialog.getByText('Select a suggested model or enter an upstream model identifier.')).toBeVisible()
  const upstream = dialog.getByRole('combobox').first()
  await upstream.fill(modelID)
  await page.getByRole('option', { name: modelID, exact: true }).click()
  await dialog.getByRole('textbox', { name: 'Model profile code' }).fill('manual-chat')
  await dialog.getByRole('button', { name: 'Add to enabled models' }).click()
  await expect(dialog.getByRole('row').filter({ hasText: 'manual-chat' })).toBeVisible()
  await dialog.getByRole('button', { name: 'Create models and profiles' }).click()
  await expect(dialog.getByText('Model service connected', { exact: true })).toBeVisible()
  expect(calls).toEqual([
    { path: '/api/v1/inference/model-deployments', data: {
      provider_connection_id: provider.id, name: modelID, upstream_model: modelID, operations: ['chat'],
      modalities: ['text'], dimension: 0, chat_max_output_tokens_parameter: 'max_tokens',
      chat_temperature_mode: 'configurable', chat_thinking_mode: 'upstream_default', status: 'active'
    } },
    { path: '/api/v1/inference/model-profiles', data: {
      name: modelID + ' - Default chat', code: 'manual-chat', scope_type: 'tenant',
      model_deployment_id: 'fixture-deployment', status: 'active'
    } }
  ])
  expect(unexpected).toEqual([])
})

test('the same discoverable provider requests its model list with execution permission', async ({ page }) => {
  const { calls, unexpected, dialog } = await openSettings(page, true)
  await expect(dialog.getByText('Connected and discovered 1 models', { exact: true })).toBeVisible()
  await dialog.getByRole('combobox').first().fill('discovered')
  await expect(page.getByRole('option', { name: 'discovered-chat-model', exact: true })).toBeVisible()
  expect(calls.map(call => call.path)).toEqual(['/api/v1/inference/provider-connections/fixture-provider/discover-models'])
  expect(unexpected).toEqual([])
})
