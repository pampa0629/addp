import { test, expect } from '@playwright/test'

for (const permissions of [['transfer.task.read','monitor.execution.read'],['transfer.task.read'],['monitor.execution.read']]) test(`Tenant CDC entry requires both owner reads: ${permissions.join('+')}`,async({page})=>{
 const reads=[]
 await page.addInitScript(()=>localStorage.setItem('addp-lang','zh-cn'))
 await page.route('**/api/v1/**',route=>{
  const path=new URL(route.request().url()).pathname
  if(path.endsWith('/system/refresh'))return route.fulfill({json:{access_token:'cdc-fixture',expires_in:300}})
  if(path.endsWith('/system/users/me'))return route.fulfill({json:{id:31,username:'cdc-fixture'}})
  if(path.endsWith('/system/auth/context'))return route.fulfill({json:{principal:{type:'user',id:'31'},context:{type:'tenant',tenant_id:'1'},authorization:{role_assignments:[{scope:{type:'tenant',tenant_id:'1'},permissions}]}}})
  reads.push(path);return route.fulfill({status:403,json:{error:'unexpected'}})
 })
 await page.route('**/module-ui/**',route=>route.fulfill({contentType:'text/html',body:'<title>CDC iframe fixture</title>'}))
 await page.goto('/monitor/cdc-resources?view=tasks&task_id=7')
 await expect(page.locator('iframe.module-iframe')).toHaveCount(permissions.length===2?1:0)
 if(permissions.length===2)await expect(page.locator('iframe.module-iframe')).toHaveAttribute('src',/view=tasks.*task_id=7/)
 else await expect(page.locator('.el-result')).toContainText('无权访问')
 expect(reads).toEqual([])
})
