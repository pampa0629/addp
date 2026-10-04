import { expect, test } from '@playwright/test'
import { managerAuthContext } from './managerAuthContext.js'

const locator = 'addp://engine/9/path/public/current?type=table&item_id=3'
const node = id => ({ kind: 'data_item', item_id: id, name: id === 3 ? 'current' : `source_${id}`, full_name: `public.table_${id}`, engine_id: 9, engine_name: 'Lineage PostgreSQL', item_type: 'table' })
const edge = (source, target) => ({ source: node(source), target: node(target), relation_kind: 'derive', granularity: 'item' })

test('lineage fills the viewport, controls query depth and survives resizing and empty results', async ({ page }) => {
  const requests = []
  const errors = []
  page.on('pageerror', error => errors.push(error.message))
  await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  await page.route('**/plugins/manifest.json', route => json(route, { scripts: [] }))
  await page.route('**/api/v1/**', async route => {
    const url = new URL(route.request().url())
    const path = url.pathname
    if (path.endsWith('/system/refresh')) return json(route, { access_token: 'lineage-e2e-token', expires_in: 3600 })
    if (path.endsWith('/system/users/me')) return json(route, { id: 1, username: 'lineage-e2e' })
    if (path.endsWith('/system/auth/context')) return json(route, managerAuthContext)
    if (path.endsWith('/manager/engines')) return json(route, { data: [{ id: 9, name: 'Lineage PostgreSQL', engine_type: 'postgresql', lifecycle_state: 'active', connection_status: 'online' }] })
    if (path.endsWith('/ancestors')) return json(route, { target_locator: locator, ancestors: [{ id: locator, locator, label: 'current', type: 'table', metadata: { item_id: 3 } }] })
    if (path.endsWith('/meta/lineage/graph')) {
      const depth = Number(url.searchParams.get('depth'))
      requests.push({ depth, item: url.searchParams.get('item_id'), direction: url.searchParams.get('direction') })
      if (depth === 20) return json(route, { subject: node(3), nodes: [node(3)], edges: [], truncated: true })
      return json(route, { subject: node(3), nodes: [node(1), node(2), node(3), node(4)], edges: [edge(1, 3), edge(2, 3), edge(3, 4)] })
    }
    return json(route, {})
  })
  await page.goto(`/data-explorer?locator=${encodeURIComponent(locator)}&tab=lineage`)
  await expect(page.locator('.lineage-canvas canvas')).toBeVisible()
  expect(requests.at(-1)).toEqual({ depth: 2, item: '3', direction: 'both' })
  const checkHeight = async () => {
    const box = await page.locator('.lineage-canvas').boundingBox()
    expect(box.height).toBeGreaterThan(page.viewportSize().height * 0.6)
    expect(page.viewportSize().height - box.y - box.height).toBeLessThan(60)
    const canvas = await page.locator('.lineage-canvas canvas').boundingBox()
    expect(Math.abs(canvas.height - box.height)).toBeLessThan(2)
  }
  await checkHeight()
  await page.setViewportSize({ width: 1600, height: 1100 })
  await expect.poll(async () => (await page.locator('.lineage-canvas canvas').boundingBox()).height).toBeGreaterThan(800)
  await checkHeight()
  await page.locator('.lineage-depth .el-select__wrapper').click()
  await page.getByRole('option', { name: '3 层', exact: true }).click()
  await expect.poll(() => requests.at(-1).depth).toBe(3)
  await page.getByRole('button', { name: '适应窗口' }).click()
  await page.getByRole('button', { name: '放大', exact: true }).click()
  await page.locator('.lineage-depth .el-select__wrapper').click()
  await page.getByRole('option', { name: '20 层', exact: true }).click()
  await expect(page.getByRole('status')).toContainText('已达到显示上限')
  await expect(page.locator('.lineage-summary')).toContainText('1 个节点 · 0 条关系')
  await page.locator('.lineage-depth .el-select__wrapper').click()
  await page.getByRole('option', { name: '1 层', exact: true }).click()
  await expect(page.locator('.lineage-summary')).toContainText('4 个节点 · 3 条关系')
  expect(errors).toEqual([])
})

function json(route, body) {
  return route.fulfill({ contentType: 'application/json', body: JSON.stringify(body) })
}

test('shows all table fields in one query and focuses fields without fetching again', async ({ page }) => {
  const requests = []
  const errors = []
  page.on('pageerror', error => errors.push(error.message))
  await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  await page.route('**/plugins/manifest.json', route => json(route, { scripts: [] }))
  const field = (id, name, status) => ({ ...node(id), kind: 'field_ref', field_name: name, schema_snapshot_hash: `sha256:table-${id}`, field_lineage_status: status })
  const roots = [field(3, 'client.id', 'complete'), field(3, 'generated', 'complete'), field(3, 'missing', 'unavailable')]
  const source = field(1, 'nested.id')
  await page.route('**/api/v1/**', route => {
    const url = new URL(route.request().url())
    const path = url.pathname
    if (path.endsWith('/system/refresh')) return json(route, { access_token: 'lineage-e2e-token', expires_in: 3600 })
    if (path.endsWith('/system/users/me')) return json(route, { id: 1, username: 'lineage-e2e' })
    if (path.endsWith('/system/auth/context')) return json(route, managerAuthContext)
    if (path.endsWith('/manager/engines')) return json(route, { data: [{ id: 9, name: 'Lineage PostgreSQL', engine_type: 'postgresql', lifecycle_state: 'active', connection_status: 'online' }] })
    if (path.endsWith('/ancestors')) return json(route, { target_locator: locator, ancestors: [{ id: locator, locator, label: 'current', type: 'table', metadata: { item_id: 3 } }] })
    if (path.endsWith('/meta/items/3')) return json(route, { ...node(3), attributes: { type_info: { table: { fields: roots.map(field => ({ name: field.field_name, type: 'string' })) } } } })
    if (path.endsWith('/meta/lineage/graph')) {
      requests.push(Object.fromEntries(url.searchParams))
      if (url.searchParams.get('granularity') !== 'field') return json(route, { granularity: 'item', subject: node(3), nodes: [node(3)], edges: [] })
      return json(route, { granularity: 'field', subject: { ...node(3), schema_snapshot_hash: 'sha256:table-3' }, nodes: [...roots, source], edges: [{ source, target: roots[0], granularity: 'field', relation_kind: 'derive', transformation: 'direct' }], field_lineage_status: 'unavailable' })
    }
    return json(route, {})
  })
  await page.goto(`/data-explorer?locator=${encodeURIComponent(locator)}&tab=lineage`)
  await page.getByText('字段级', { exact: true }).click()
  await expect(page.locator('.lineage-fields')).toBeVisible()
  await expect(page.locator('.lineage-canvas canvas')).toBeVisible()
  expect(requests.at(-1)).toMatchObject({ subject_kind: 'data_item', granularity: 'field' })
  expect(requests.at(-1).field_name).toBeUndefined()
  const count = requests.length
  await page.getByRole('button', { name: 'client.id', exact: true }).click()
  await expect(page.locator('.lineage-inspector strong')).toHaveText('client.id')
  await expect(page.locator('.lineage-inspector')).toContainText('sha256:table-3')
  const heading = await page.locator('.lineage-inspector-heading').boundingBox()
  const details = await page.locator('.lineage-inspector-fields').boundingBox()
  expect(details.x).toBeGreaterThan(heading.x + heading.width)
  expect(details.y).toBeLessThan(heading.y + heading.height)
  await page.getByRole('button', { name: 'generated', exact: true }).click()
  await expect(page.getByRole('status')).toContainText('已记录的字段血缘中没有关联字段')
  await page.getByRole('button', { name: 'missing', exact: true }).click()
  await expect(page.getByRole('status')).toContainText('当前字段尚无可用血缘证据')
  await page.getByRole('button', { name: '全部字段', exact: true }).click()
  await expect(page.locator('.lineage-inspector')).toBeHidden()
  expect(requests.length).toBe(count)
  await page.screenshot({ path: '/tmp/addp-field-overview-e2e.png' })
  await page.getByText('数据项级', { exact: true }).click()
  await expect.poll(() => requests.at(-1).granularity).toBe('item')
  expect(errors).toEqual([])
})

test('expands a single direction from a node while keeping the current table and viewport', async ({ page }) => {
 const queries=[]
 await page.addInitScript(()=>localStorage.setItem('addp-lang','zh-cn'))
 await page.route('**/plugins/manifest.json',route=>json(route,{scripts:[]}))
 await page.route('**/api/v1/**',async route=>{
  const url=new URL(route.request().url()); const path=url.pathname
  if(path.endsWith('/system/refresh')) return json(route,{access_token:'lineage-e2e-token',expires_in:3600})
  if(path.endsWith('/system/users/me')) return json(route,{id:1,username:'lineage-e2e'})
  if(path.endsWith('/system/auth/context')) return json(route,managerAuthContext)
  if(path.endsWith('/manager/engines')) return json(route,{data:[{id:9,name:'Lineage PostgreSQL',engine_type:'postgresql',lifecycle_state:'active',connection_status:'online'}]})
  if(path.endsWith('/ancestors')) return json(route,{target_locator:locator,ancestors:[{id:locator,locator,label:'current',type:'table',metadata:{item_id:3}}]})
  if(path.endsWith('/meta/lineage/graph')) {
   queries.push(Object.fromEntries(url.searchParams)); const expanded=url.searchParams.get('expand_upstream')==='3'
   const root={...node(3),hidden_upstream_count:expanded?0:2}
   return json(route,{subject:root,nodes:expanded?[node(1),node(2),root]:[root],edges:expanded?[edge(1,3),edge(2,3)]:[]})
  }
  return json(route,{})
 })
 await page.goto(`/data-explorer?locator=${encodeURIComponent(locator)}&tab=lineage`)
 const canvas=page.locator('.lineage-canvas canvas');await expect(canvas).toBeVisible()
 await expect(page.locator('.lineage-summary')).toContainText('1 个节点')
 const box=await canvas.boundingBox()
 await canvas.click({position:{x:box.width/2,y:box.height/2}})
 await expect(page.locator('.lineage-inspector strong')).toHaveText('current')
 await page.getByRole('button',{name:'上游 +2',exact:true}).click()
 await expect(page.locator('.lineage-summary')).toContainText('3 个节点 · 2 条关系')
 expect(queries.at(-1)).toMatchObject({item_id:'3',depth:'2',expand_upstream:'3'})
 expect(queries.at(-1).expand_downstream).toBeUndefined()
 await expect(page.locator('.lineage-inspector strong')).toHaveText('current')
 await expect(page.getByRole('button',{name:'上游 +2',exact:true})).toBeHidden()
 // A depth change deliberately resets local expansion.
 await page.locator('.lineage-depth .el-select__wrapper').click()
 await page.getByRole('option',{name:'3 层',exact:true}).click()
 await expect.poll(()=>queries.at(-1).depth).toBe('3')
 expect(queries.at(-1).expand_upstream).toBeUndefined()
 await expect(page.locator('.lineage-summary')).toContainText('1 个节点')
 const resetBox=await canvas.boundingBox()
 // Hit the drawn upstream badge; after expansion the root keeps its screen point.
 await canvas.click({position:{x:resetBox.width/2-142,y:resetBox.height/2}})
 await expect(page.locator('.lineage-summary')).toContainText('3 个节点')
 await canvas.click({position:{x:resetBox.width/2,y:resetBox.height/2}})
 await expect(page.locator('.lineage-inspector strong')).toHaveText('current')
})

test('edge evidence opens the source execution in Monitor', async ({ page }) => {
 await page.addInitScript(()=>localStorage.setItem('addp-lang','zh-cn'))
 await page.route('**/plugins/manifest.json',route=>json(route,{scripts:[]}))
 await page.route('**/api/v1/**',async route=>{
  const path=new URL(route.request().url()).pathname
  if(path.endsWith('/system/refresh')) return json(route,{access_token:'lineage-e2e-token',expires_in:3600})
  if(path.endsWith('/system/users/me')) return json(route,{id:1,username:'lineage-e2e'})
  if(path.endsWith('/system/auth/context')) return json(route,managerAuthContext)
  if(path.endsWith('/manager/engines')) return json(route,{data:[{id:9,name:'Lineage PostgreSQL',engine_type:'postgresql',lifecycle_state:'active',connection_status:'online'}]})
  if(path.endsWith('/ancestors')) return json(route,{target_locator:locator,ancestors:[{id:locator,locator,label:'current',type:'table',metadata:{item_id:3}}]})
  if(path.endsWith('/meta/lineage/graph')) return json(route,{subject:node(3),nodes:[node(1),node(3)],edges:[{...edge(1,3),evidence:{execution_id:'lineage-source-execution'}}]})
  return json(route,{})
 })
 await page.goto(`/data-explorer?locator=${encodeURIComponent(locator)}&tab=lineage`)
 const canvas=page.locator('.lineage-canvas canvas');await expect(canvas).toBeVisible()
 const box=await canvas.boundingBox()
 await canvas.click({position:{x:box.width/2,y:box.height/2}})
 await expect(page.locator('.lineage-inspector')).toContainText('lineage-source-execution')
 const popupPromise=page.waitForEvent('popup')
 await page.getByRole('button',{name:'查看来源执行'}).click()
 const popup=await popupPromise
 await expect(popup).toHaveURL(/\/monitor\/executions\?execution_id=lineage-source-execution/)
 await popup.close()
})

for (const catalogRead of [false, true]) {
  test(`lineage respects Catalog summary read permission (${catalogRead})`, async ({ page }) => {
    const catalogRequests = []
    const context = structuredClone(managerAuthContext)
    if (catalogRead) context.authorization.role_assignments[0].permissions.push('catalog.entry.read')
    await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
    await page.route('**/plugins/manifest.json', route => json(route, { scripts: [] }))
    await page.route('**/api/v1/**', route => {
      const url = new URL(route.request().url())
      const path = url.pathname
      if (path.endsWith('/system/refresh')) return json(route, { access_token: 'lineage-e2e-token', expires_in: 3600 })
      if (path.endsWith('/system/users/me')) return json(route, { id: 1, username: 'lineage-e2e' })
      if (path.endsWith('/system/auth/context')) return json(route, context)
      if (path.endsWith('/manager/engines')) return json(route, { data: [{ id: 9, name: 'Lineage PostgreSQL', engine_type: 'postgresql', lifecycle_state: 'active', connection_status: 'online' }] })
      if (path.endsWith('/ancestors')) return json(route, { target_locator: locator, ancestors: [{ id: locator, locator, label: 'current', type: 'table', metadata: { item_id: 3 } }] })
      if (path.endsWith('/meta/items/3')) return json(route, { ...node(3), fingerprint: 'sha256:catalog-permission-item' })
      if (path.endsWith('/catalog/entries')) {
        catalogRequests.push(Object.fromEntries(url.searchParams))
        return json(route, { data: [{ id: 42, display_name: 'Current resource', governance_status: 'discovered', source_status: 'active' }] })
      }
      if (path.endsWith('/meta/lineage/graph')) return json(route, { subject: node(3), nodes: [node(1), node(3)], edges: [edge(1, 3)] })
      return json(route, {})
    })
    await page.goto(`/data-explorer?locator=${encodeURIComponent(locator)}&tab=lineage`)
    await expect(page.locator('.lineage-summary')).toContainText('2 个节点 · 1 条关系')
    await expect(page.locator('.lineage-canvas canvas')).toBeVisible()
    const catalog = page.locator('.resource-governance-summary__catalog')
    if (catalogRead) {
      await expect(catalog).toContainText('Current resource')
      expect(catalogRequests).toEqual([{ source_identity: 'sha256:catalog-permission-item', page: '1', page_size: '1' }])
    } else {
      await expect(catalog).toBeHidden()
      expect(catalogRequests).toEqual([])
    }
  })
}
