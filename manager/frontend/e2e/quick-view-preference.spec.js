import { expect, test } from '@playwright/test'
import { readFileSync } from 'node:fs'
import { managerAuthContext } from './managerAuthContext.js'

const copc = readFileSync(new URL('./fixtures/pointcloud/rgb8.copc.laz', import.meta.url))
const first = 'addp://engine/2/path/first.las?type=file&item_id=8'
const second = 'addp://engine/2/path/second.las?type=file&item_id=9'
const rootLocator = 'addp://engine/2/path/?type=server&node_id=1'
const leaf = (locator, label) => ({ id: locator, locator, label, type: 'file', hasChildren: false })
const children = [leaf(first, 'first.las'), leaf(second, 'second.las')]
const root = { id: rootLocator, locator: rootLocator, label: 'Point source', type: 'server', hasChildren: true, children }

async function fixture(page, locale, existing = false) {
  let releasePreference
  const preferenceCompletion = new Promise(resolve => { releasePreference = resolve })
  let release
  const completion = new Promise(resolve => { release = resolve })
  const state = { ready: existing, preferred: existing ? 'map_quick_view' : 'basic_preview', view: {}, started: false, writes: [], generate: [], holdQuickPreference: false, failQuickPreference: false, executionStatus: 'success' }
  await page.addInitScript(lang => localStorage.setItem('addp-lang', lang), locale)
  await page.route('**/plugins/manifest.json', route => route.fulfill({ json: { scripts: ['/plugins/point-cloud-preview.js'] } }))
  await page.route('**/api/v1/**', async route => {
    const request = route.request(), url = new URL(request.url()), path = url.pathname
    const locator = url.searchParams.get('locator') || first
    const json = value => route.fulfill({ json: value })
    if (path.endsWith('/system/refresh')) return json({ access_token: 'preference-fixture', expires_in: 3600 })
    if (path.endsWith('/system/users/me')) return json({ id: 1, display_name: 'preference-test', local_account: { username: 'preference-test' } })
    if (path.endsWith('/system/auth/context')) return json(managerAuthContext)
    if (path.endsWith('/manager/engines')) return json({ data: [{ id: 2, name: root.label, engine_type: 'nfs', lifecycle_state: 'active', connection_status: 'online' }] })
    if (path.endsWith('/ancestors')) return json({ target_locator: locator, ancestors: [{ ...root, children: [] }, children.find(item => item.locator === locator)] })
    if (path.endsWith('/meta/resource-tree/2') || path.endsWith('/meta/resource-tree/2/node')) return json(root)
    if (path.endsWith('/manager/preview-state/preferred-mode')) {
      const body = request.postDataJSON(); state.writes.push(body)
      if (state.failQuickPreference && body.preferred_mode === 'map_quick_view') return route.fulfill({ status: 500, json: { error: 'preference fixture failure' } })
      if (state.holdQuickPreference && body.preferred_mode === 'map_quick_view') await preferenceCompletion
      if (body.locator === first) state.preferred = body.preferred_mode
      return json({})
    }
    if (path.endsWith('/manager/preview-state/view-state')) {
      const body = request.postDataJSON(); if (body.locator === first) state.view = body.view_state
      return json({ view_state: body.view_state })
    }
    if (path.endsWith('/manager/preview-state')) return json({ preferred_mode: locator === first ? state.preferred : 'basic_preview', view_state: locator === first ? state.view : {} })
    if (path.endsWith('/manager/quick-view/actions')) { state.started = true; state.generate.push(request.postDataJSON()); return json({ execution_id: 'preference-exec' }) }
    if (path.endsWith('/manager/executions/preference-exec')) { await completion; if (state.executionStatus === 'success') state.ready = true; return json({ status: state.executionStatus }) }
    if (path.endsWith('/manager/quick-view/capability')) return json({
      can_use_quick_view: locator === first && state.ready, render_source: 'point_cloud_copc', status: state.ready ? 'available' : 'unavailable',
      available_actions: ['generate_point_cloud_copc', ...(locator === first && state.ready ? [state.preferred === 'map_quick_view' ? 'back_to_basic_preview' : 'switch_quick_view'] : [])], preferred_mode: locator === first ? state.preferred : 'basic_preview', view_state: locator === first ? state.view : {},
      point_cloud: { result_id: 1, last_execution_id: 'fixture-v1', point_count: 441, preview_url: '/api/v1/manager/point_cloud_copc/1/content' }
    })
    if (path.endsWith('/manager/preview')) return json({ preview_type: 'object', data: { mode: 'object', object: { content: { kind: 'unsupported', metadata: { format: 'las' } } } } })
    if (path.endsWith('/point_cloud_copc/1/content')) {
      const range = /bytes=(\d+)-(\d+)/.exec(request.headers().range || '')
      const start = Number(range[1]), end = Math.min(Number(range[2]), copc.length - 1)
      return route.fulfill({ status: 206, headers: { 'Content-Type': 'application/vnd.laszip+copc', 'Content-Range': `bytes ${start}-${end}/${copc.length}`, 'Accept-Ranges': 'bytes' }, body: copc.subarray(start, end + 1) })
    }
    return json({})
  })
  return { state, release, releasePreference }
}

for (const locale of ['zh-cn', 'en']) {
  const labels = locale === 'en'
    ? { generate: 'Generate COPC Quick View', back: 'Back to Basic Preview', switch: 'Use Quick View', color: 'Point cloud coloring', rgb8: 'RGB 8-bit' }
    : { generate: '生成 COPC 快显', back: '返回基础预览', switch: '切换快显', color: '点云着色模式', rgb8: 'RGB 8 位' }
  test(`generation persists quick view and renderer state across reload ${locale}`, async ({ page }) => {
    const { state, release } = await fixture(page, locale)
    await page.goto(`/data-explorer?locator=${encodeURIComponent(first)}`)
    await page.getByRole('button', { name: labels.generate, exact: true }).click()
    release()
    await expect(page.getByRole('button', { name: labels.back, exact: true })).toBeVisible()
    await expect.poll(() => state.preferred).toBe('map_quick_view')
    const color = page.getByRole('combobox', { name: labels.color })
    await color.press('ArrowDown')
    await page.getByRole('option', { name: labels.rgb8, exact: true }).click()
    await expect.poll(() => state.view.quick_view?.scene_3d?.color_mode).toBe('rgb8')
    await page.reload()
    await expect(page.getByRole('button', { name: labels.back, exact: true })).toBeVisible()
    await expect(page.locator('.point-cloud-toolbar')).toContainText(labels.rgb8)
    expect(state.generate).toHaveLength(1)
    expect(state.writes).toEqual([{ locator: first, preferred_mode: 'map_quick_view' }])
  })

  test(`late generation cannot replace an explicit basic preference ${locale}`, async ({ page }) => {
    const { state, release } = await fixture(page, locale, true)
    await page.goto(`/data-explorer?locator=${encodeURIComponent(first)}`)
    await page.getByRole('button', { name: labels.generate, exact: true }).click()
    await expect.poll(() => state.started).toBe(true)
    await page.getByRole('button', { name: labels.back, exact: true }).click()
    await expect.poll(() => state.preferred).toBe('basic_preview')
    release()
    await expect(page.getByRole('button', { name: labels.generate, exact: true })).toBeEnabled()
    expect(state.writes).toEqual([{ locator: first, preferred_mode: 'basic_preview' }])
    await page.reload()
    await expect(page.getByRole('button', { name: labels.switch, exact: true })).toBeVisible()
  })

  test(`a manual choice remains last when automatic preference saving is delayed ${locale}`, async ({ page }) => {
    const { state, release, releasePreference } = await fixture(page, locale, true)
    state.holdQuickPreference = true
    await page.goto(`/data-explorer?locator=${encodeURIComponent(first)}`)
    await page.getByRole('button', { name: labels.generate, exact: true }).click()
    release()
    await expect.poll(() => state.writes.length).toBe(1)
    await page.getByRole('button', { name: labels.back, exact: true }).click()
    await expect(page.getByRole('button', { name: labels.back, exact: true })).toBeDisabled()
    releasePreference()
    await expect.poll(() => state.preferred).toBe('basic_preview')
    await expect(page.getByRole('button', { name: labels.generate, exact: true })).toBeEnabled()
    expect(state.writes.map(body => body.preferred_mode)).toEqual(['map_quick_view', 'basic_preview'])
    await page.reload()
    await expect(page.getByRole('button', { name: labels.switch, exact: true })).toBeVisible()
  })

  test(`preference failure keeps the generated artifact and permits an explicit retry ${locale}`, async ({ page }) => {
    const { state, release } = await fixture(page, locale)
    state.failQuickPreference = true
    await page.goto(`/data-explorer?locator=${encodeURIComponent(first)}`)
    await page.getByRole('button', { name: labels.generate, exact: true }).click()
    release()
    await expect(page.getByText(locale === 'en' ? 'Failed to switch quick view' : '切换快显失败', { exact: true })).toBeVisible()
    expect(state.ready).toBe(true)
    expect(state.preferred).toBe('basic_preview')
    state.failQuickPreference = false
    await page.getByRole('button', { name: labels.switch, exact: true }).click()
    await expect.poll(() => state.preferred).toBe('map_quick_view')
    await page.reload()
    await expect(page.getByRole('button', { name: labels.back, exact: true })).toBeVisible()
    expect(state.generate).toHaveLength(1)
  })

  test(`failed generation with an old ready artifact cannot change the preferred mode ${locale}`, async ({ page }) => {
    const { state, release } = await fixture(page, locale, true)
    state.preferred = 'basic_preview'
    state.executionStatus = 'failed'
    await page.goto(`/data-explorer?locator=${encodeURIComponent(first)}`)
    await page.getByRole('button', { name: labels.generate, exact: true }).click()
    release()
    await expect.poll(() => state.started).toBe(true)
    await expect(page.getByRole('button', { name: labels.generate, exact: true })).toBeEnabled()
    expect(state.preferred).toBe('basic_preview')
    expect(state.writes).toEqual([])
    await expect(page.getByRole('button', { name: labels.switch, exact: true })).toBeVisible()
  })

  test(`late generation cannot change a newly selected item ${locale}`, async ({ page }) => {
    const { state, release } = await fixture(page, locale)
    await page.goto(`/data-explorer?locator=${encodeURIComponent(first)}`)
    await page.getByRole('button', { name: labels.generate, exact: true }).click()
    await expect.poll(() => state.started).toBe(true)
    await page.getByRole('treeitem', { name: 'second.las', exact: true }).click()
    release()
    await expect(page.getByRole('button', { name: labels.generate, exact: true })).toBeEnabled()
    expect(new URL(page.url()).searchParams.get('locator')).toBe(second)
    expect(state.writes).toEqual([])
    await expect(page.getByRole('button', { name: labels.back, exact: true })).toHaveCount(0)
  })
}
