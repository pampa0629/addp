import { expect,test } from '@playwright/test'
import { engineBackend,engineID,engineOwner } from './engine-resources.fixture'

for(const locale of ['zh-cn','en'])test(`MinIO uses exact groups, local tips and missing native values (${locale})`,async({page})=>{
 const en=locale==='en', state=await engineBackend(page,{locale,owner:{...engineOwner,engine_type:'minio',display_name:'Business MinIO'}})
 const requests=[];page.on('request',request=>{if(request.url().includes('engine_resource_observations'))requests.push(new URL(request.url()).searchParams.get('metrics').split(','))})
 // A System link can contain only the UUID; the current owner supplies the type.
 await page.goto(`/engine-resources?deployment_id=${engineID}&refresh=off`)
 await expect(page).toHaveURL(/engine_type=minio/)
 await expect(page.getByTestId('engine-metrics').locator('article')).toHaveCount(10)
 await expect(page.getByTestId('engine-value-minioTotal')).toHaveText('1.00 GiB')
 const help=page.getByTestId('engine-metric-minioTotal').getByRole('button',{name:en?/^Storage capacity visible to MinIO/:/^MinIO 可见的存储总容量/})
 await help.focus();await expect(page.getByRole('tooltip')).toContainText(en?'must not be added':'不能相加')
 state.setMode('no-scan');await page.getByRole('button',{name:en?'Refresh':'刷新',exact:true}).click()
 for(const name of ['minioObjectBytes','minioObjects','minioBuckets','minioScanAge'])await expect(page.getByTestId('engine-value-'+name)).toHaveText('—')
 await expect(page.getByTestId('engine-value-minioTotal')).toHaveText('1.00 GiB')
 await page.getByRole('tab',{name:en?'Requests and traffic':'请求与流量',exact:true}).click()
 await expect(page.getByTestId('engine-metrics').locator('article')).toHaveCount(7)
 state.setMode('minio-unpublished');await page.getByRole('button',{name:en?'Refresh':'刷新',exact:true}).click()
 for(const name of ['minioInflight','minio4xx','minio5xx','minioErrorPercent'])await expect(page.getByTestId('engine-value-'+name)).toHaveText('—')
 await expect(page.getByTestId('engine-value-minioRequests')).not.toHaveText('—')
 expect(requests.every(keys=>keys.length<=12&&keys.every(key=>key.startsWith('minio.')))).toBe(true)
 expect(requests.map(keys=>keys.length)).toEqual([10,10,7,7])
 await page.getByRole('button',{name:en?'Back to engines':'返回引擎列表',exact:true}).click()
 await expect(page).toHaveURL(/engine_type=minio/);await expect(page.getByTestId('engine-list')).toBeVisible()
 expect(state.queries.every(query=>query.engine_type==='minio')).toBe(true)
})
