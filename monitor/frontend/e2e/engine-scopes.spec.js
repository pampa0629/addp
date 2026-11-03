import { test, expect } from '@playwright/test'
import { engineBackend, engineOwner } from './engine-resources.fixture'

const id = n => `20000000-0000-4000-8000-${String(n).padStart(12,'0')}`
const row = (n, scope, kind, name) => ({...engineOwner,deployment_id:id(n),scope,engine_type:kind,display_name:name})
const mixed = [
 row(1,'infra','postgresql','Business-looking PostgreSQL'),
 row(2,'infra','redis','Infra cache'), row(3,'infra','minio','Infra objects'),
 row(4,'business','postgresql','Infra-looking PostgreSQL'), row(5,'business','redis','Business cache')
]

for (const locale of ['zh-cn','en']) test(`provenance tabs show mixed engine cards with one summary request (${locale})`,async({page})=>{
 const state=await engineBackend(page,{locale,owners:structuredClone(mixed)})
 const requests=[];page.on('request',r=>{if(r.url().includes('engine_resource_summaries'))requests.push(new URL(r.url()).searchParams.get('deployment_ids').split(','))})
 await page.setViewportSize({width:620,height:900})
 await page.goto('/engine-resources?refresh=off')
 const list=page.getByTestId('engine-list')
 await expect(list.locator('article')).toHaveCount(3)
 for (const kind of ['PostgreSQL','Redis','MinIO'])await expect(list.getByText(kind,{exact:true})).toBeVisible()
 await expect(page.getByRole('combobox',{name:locale==='en'?'Engine type':'引擎类型',exact:true})).toHaveCount(0)
 await expect(list.getByText('Business-looking PostgreSQL',{exact:true})).toBeVisible()
 await expect(list.getByText('Infra-looking PostgreSQL',{exact:true})).toHaveCount(0)
 await expect(list.locator('.engine-summary dd')).toHaveCount(12)
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
 expect(requests).toEqual([[id(1),id(2),id(3)]])
 expect(state.queries).toEqual([{scope:'infra',page:'1',page_size:'20'}])
 await page.setViewportSize({width:1280,height:900})
 await page.screenshot({path:test.info().outputPath('infra-engine-cards.png'),fullPage:true})
 await page.getByRole('tab',{name:locale==='en'?'Business engines':'Business 业务引擎',exact:true}).click()
 await expect(list.locator('article')).toHaveCount(2)
 await expect(list.getByText('Infra-looking PostgreSQL',{exact:true})).toBeVisible()
 await expect(list.getByText('Business-looking PostgreSQL',{exact:true})).toHaveCount(0)
 await expect(list.locator('.engine-summary dd')).toHaveCount(8)
 expect(requests[1]).toEqual([id(4),id(5)])
 expect(state.queries.at(-1)).toEqual({scope:'business',page:'1',page_size:'20'})
 await page.getByTestId(`engine-card-${id(5)}`).getByRole('button',{name:locale==='en'?'View monitoring':'查看监控',exact:true}).click()
 await expect(page.getByTestId('engine-value-redisAllocated')).toHaveText('1.00 GiB')
 await expect(page).toHaveURL(url=>url.searchParams.get('scope')==='business'&&url.searchParams.get('engine_type')==='redis')
 await page.getByRole('button',{name:locale==='en'?'Back to engines':'返回引擎列表',exact:true}).click()
 await expect(list.locator('article')).toHaveCount(2)
 await expect(page).toHaveURL(url=>url.searchParams.get('scope')==='business'&&!url.searchParams.has('engine_type'))
})

test('scope pagination and search stay on the server and detail return retains the page',async({page})=>{
 const owners=Array.from({length:21},(_,n)=>row(n+1,'infra',['postgresql','redis','minio'][n%3],`Engine ${String(n).padStart(2,'0')}`))
 owners.push(row(30,'business','postgresql','Business database'))
 const state=await engineBackend(page,{owners})
 await page.goto('/engine-resources?page=2&refresh=off')
 await expect(page.getByTestId('engine-list').locator('article')).toHaveCount(1)
 expect(state.queries[0]).toEqual({scope:'infra',page:'2',page_size:'20'})
 await page.getByRole('button',{name:'查看监控',exact:true}).click()
 await expect(page.getByTestId('engine-metrics')).toBeVisible()
 await page.getByRole('button',{name:'返回引擎列表',exact:true}).click()
 await expect(page).toHaveURL(url=>url.searchParams.get('page')==='2')
 await page.getByRole('textbox',{name:'搜索部署名称',exact:true}).fill('Engine 20')
 await expect.poll(()=>state.queries.at(-1)).toEqual({scope:'infra',page:'1',page_size:'20',search:'Engine 20'})
 await expect(page.getByTestId('engine-list').locator('article')).toHaveCount(1)
 await page.getByRole('tab',{name:'Business 业务引擎',exact:true}).click()
 await expect(page.getByText('此范围内暂无已纳管引擎',{exact:true})).toBeVisible()
 expect(state.queries.at(-1)).toEqual({scope:'business',page:'1',page_size:'20',search:'Engine 20'})
})

test('direct business owner determines provenance instead of its display name',async({page})=>{
 await engineBackend(page,{owners:structuredClone(mixed)})
 await page.goto(`/engine-resources?deployment_id=${id(4)}&refresh=off`)
 await expect(page.getByTestId('engine-value-capacity')).toHaveText('1.00 GiB')
 await expect(page).toHaveURL(url=>url.searchParams.get('scope')==='business')
 await page.getByRole('button',{name:'返回引擎列表',exact:true}).click()
 await expect(page.getByRole('tab',{name:'Business 业务引擎',exact:true})).toHaveAttribute('aria-selected','true')
})

test('late old-scope observations cannot refill the new scope cards',async({page})=>{
 const state=await engineBackend(page,{owners:structuredClone(mixed)})
 await page.goto('/engine-resources?refresh=off')
 await expect(page.getByTestId('engine-list').locator('article')).toHaveCount(3)
 state.hold();await page.getByRole('button',{name:'刷新',exact:true}).click()
 await page.getByRole('tab',{name:'Business 业务引擎',exact:true}).click()
 await expect(page.getByTestId('engine-list')).not.toContainText('Business-looking PostgreSQL')
 state.release()
 await expect(page.getByTestId('engine-list').locator('article')).toHaveCount(2)
 await expect(page.getByTestId('engine-list')).toContainText('Infra-looking PostgreSQL')
 await expect(page.getByTestId('engine-list')).not.toContainText('Business-looking PostgreSQL')
})

test('foreign-scope rows are rejected before reading summaries',async({page})=>{
 const state=await engineBackend(page)
 await page.route('**/api/v1/system/platform/engine_deployments?**',route=>route.fulfill({json:{data:[mixed[3]],total:1,page:1,page_size:20}}))
 await page.goto('/engine-resources?refresh=off')
 await expect(page.getByTestId('engine-error')).toBeVisible()
 await expect(page.getByTestId('engine-list').locator('article')).toHaveCount(0)
 expect(state.calls.filter(path=>path.endsWith('_summaries'))).toHaveLength(0)
})
