import { expect,test } from '@playwright/test'
import { HOST_NODE_ID } from './host-nodes.fixture'
import { DEPLOYMENT_ID,inventory,mockDeployments,sourceFile } from './engine-deployments.fixture'

test('Business source preview requires explicit host confirmation and creates a reloadable deployment identity',async({page})=>{
 const {writes}=await mockDeployments(page)
 await page.goto('/engine-deployments?search=Business&page_size=50')
 await page.getByTestId('deployment-create').click()
 const dialog=page.getByRole('dialog')
 await expect(page.getByTestId('deployment-name')).toBeFocused()
 await page.getByTestId('deployment-name').fill('My PostgreSQL')
 await page.getByTestId('deployment-file').setInputFiles(sourceFile(inventory))
 await expect(page.getByTestId('deployment-source')).toContainText('fixture-daemon')
 await expect(dialog).toContainText('不代表当前在线或已开始采集')
 await expect(page.getByTestId('deployment-save')).toBeDisabled()
 expect(writes).toEqual([])
 await dialog.getByRole('combobox',{name:'承载主机',exact:true}).press('Enter')
 await page.getByRole('option',{name:'Node A',exact:true}).click()
 await page.getByTestId('deployment-save').click()
 await expect.poll(()=>writes.length).toBe(1)
 expect(writes[0]).toEqual({display_name:'My PostgreSQL',enabled:true,inventory,declared_node_id:HOST_NODE_ID})
 await expect(page).toHaveURL(new RegExp(`/engine-deployments/${DEPLOYMENT_ID}\\?search=Business&page_size=50$`))
 await expect(dialog).toContainText('当前版本：1')
 await page.reload()
 await expect(page.getByTestId('deployment-name')).toHaveValue('My PostgreSQL')
 await page.getByRole('dialog').getByRole('button',{name:'关闭',exact:true}).click()
 await expect(page).toHaveURL(/\/engine-deployments\?search=Business&page_size=50$/)
})

test('credential-bearing and tenant source files are rejected without writes or rendered secrets',async({page})=>{
 const {writes}=await mockDeployments(page)
 await page.goto('/engine-deployments');await page.getByTestId('deployment-create').click()
 for(const value of [{...inventory,tenant_id:'9'},{...inventory,source:{...inventory.source,password:'never-render-this'}}]){
  await page.getByTestId('deployment-file').setInputFiles(sourceFile(value))
  await expect(page.getByTestId('deployment-error')).toContainText('来源文件无效')
  await expect(page.getByTestId('deployment-source')).toHaveCount(0)
  await expect(page.getByRole('dialog')).not.toContainText('never-render-this')
 }
 expect(writes).toEqual([])
})

test('a stale update preserves the draft and blocks repeated saves until explicit reread',async({page})=>{
 const {writes}=await mockDeployments(page,{conflict:true})
 await page.goto(`/engine-deployments/${DEPLOYMENT_ID}`)
 await page.getByTestId('deployment-name').fill('Keep this draft')
 await page.getByTestId('deployment-save').click()
 await expect(page.getByTestId('deployment-error')).toContainText('已停止继续提交')
 await expect(page.getByTestId('deployment-name')).toHaveValue('Keep this draft')
 await expect(page.getByTestId('deployment-save')).toBeDisabled()
 expect(writes).toHaveLength(1);expect(writes[0].version).toBe(7)
 await page.getByRole('button',{name:'重新读取并核对',exact:true}).click()
 await expect(page.getByTestId('deployment-name')).toHaveValue('Business PostgreSQL')
 await expect(page.getByTestId('deployment-save')).toBeEnabled()
})

test('unknown creation result blocks further submission and does not invent a deployment identity',async({page})=>{
 const {writes}=await mockDeployments(page,{unknown:true})
 await page.goto('/engine-deployments');await page.getByTestId('deployment-create').click()
 await page.getByTestId('deployment-name').fill('Unknown result')
 await page.getByTestId('deployment-file').setInputFiles(sourceFile(inventory))
 await page.getByRole('dialog').getByRole('combobox',{name:'承载主机',exact:true}).press('Enter')
 await page.getByRole('option',{name:'Node A',exact:true}).click()
 await page.getByTestId('deployment-save').click()
 await expect(page.getByTestId('deployment-error')).toContainText('已停止继续提交')
 await expect(page.getByTestId('deployment-save')).toBeDisabled()
 expect(writes).toHaveLength(1)
 await expect(page).toHaveURL(/\/engine-deployments$/)
})

test('read-only platform viewers do not fetch host choices or receive write controls',async({page})=>{
 const {hosts,writes}=await mockDeployments(page,{permissions:['platform.engine_deployment.read']})
 await page.goto(`/engine-deployments/${DEPLOYMENT_ID}`)
 await expect(page.getByRole('dialog')).toContainText('关联有效（不代表在线）')
 await expect(page.getByTestId('deployment-create')).toHaveCount(0)
 await expect(page.getByTestId('deployment-save')).toHaveCount(0)
 await expect(page.getByRole('dialog').getByRole('button',{name:'关闭',exact:true})).toBeFocused()
 expect(hosts.calls).toEqual([]);expect(writes).toEqual([])
})

test('Tenant context cannot enter platform deployments even with matching permission text',async({page})=>{
 const {calls,writes}=await mockDeployments(page,{scope:'tenant'})
 await page.goto('/engine-deployments')
 await expect(page.getByTestId('engine-deployments')).toHaveCount(0)
 expect(calls).toEqual([]);expect(writes).toEqual([])
})

test('English source details fit a narrow viewport and retain translated context',async({page})=>{
 await mockDeployments(page,{language:'en'})
 await page.setViewportSize({width:390,height:844})
 await page.goto(`/engine-deployments/${DEPLOYMENT_ID}`)
 const dialog=page.getByRole('dialog')
 await expect(dialog).toContainText('This snapshot describes the export time')
 await expect(dialog).not.toContainText('system.engineDeployments.')
 expect(await dialog.evaluate(element=>element.scrollWidth<=element.clientWidth)).toBe(true)
})
