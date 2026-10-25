import { test, expect } from '@playwright/test'
import { backend } from './monitoring-targets.fixture'
import { identity, setIdentity } from './monitoring-targets.fixture'
const keys=['platform.module.read',...['read','create','update','delete'].map(k=>`monitor.platform_service_availability_rule.${k}`)]
const id='10000000-0000-4000-8000-000000000021', group='b'.repeat(64)
const target={deployment_id:'a'.repeat(64),module_name:'meta',role:'worker',runtime_name:'scan'}
const candidate={...target,id:group,expected_replicas:2,desired_state:'running',state:'observed',reason:'',online_replicas:1}
const saved={...target,id,group_id:group,name:'Meta group',trigger_seconds:60,recovery_seconds:30,version:1,enabled:false,last_verdict:'paused',last_error:'',online_replicas:null}
async function fixture(page,{locale='zh-cn',rows=[],permissions=keys}={}){
 await backend(page,{identity:identity(permissions),locale})
 await page.route('**/api/v1/monitor/platform/alerts**',route=>route.fulfill({json:{data:[],total:0,page:1,page_size:20}}))
 const writes=[],reads=[];let conflict=false, held=false, release
 const barrier=new Promise(resolve=>{release=resolve})
 await page.route('**/api/v1/monitor/platform/service-availability-alert-rules**',async route=>{
  const req=route.request(),url=new URL(req.url()),method=req.method()
  if(method==='GET'){
   reads.push(url.pathname+url.search)
   if(url.pathname.endsWith('/groups')){if(held)await barrier;return route.fulfill({json:{data:[candidate],total:1}})}
   const p=Number(url.searchParams.get('page'));return route.fulfill({json:{data:rows.slice((p-1)*20,p*20),total:rows.length,page:p,page_size:20,total_pages:Math.ceil(rows.length/20)}})
  }
  const body=req.postDataJSON();writes.push({method,body})
  if(conflict)return route.fulfill({status:409,json:{error_code:'resource_version_conflict'}})
  if(method==='DELETE')rows=[]
  else rows=[{...saved,...body,version:method==='POST'?1:body.version+1}]
  await route.fulfill({status:method==='POST'?201:200,json:rows[0]||{}})
 })
 return {writes,reads,conflict:()=>{conflict=true},hold:()=>{held=true},release}
}
test('group admission chooses readable source, creates disabled and edits with CAS',async({page})=>{
 const state=await fixture(page);await page.goto('/platform-alerts?tab=availability-rules')
 await page.getByRole('button',{name:'创建服务失联规则',exact:true}).click()
 const dialog=page.getByRole('dialog');await expect(dialog).toBeVisible()
 await dialog.getByRole('textbox').fill('Meta scan group')
 const select=dialog.getByRole('combobox',{name:'服务组',exact:true}); await select.focus();await select.press('ArrowDown')
 await expect(page.getByRole('option',{name:'meta · 任务进程 · 元数据扫描',exact:true})).toBeVisible();await page.getByRole('option',{name:'meta · 任务进程 · 元数据扫描',exact:true}).click()
 await expect(dialog.locator('.el-switch')).toHaveClass(/is-disabled/)
 await dialog.getByRole('button',{name:'保存',exact:true}).click();await expect(dialog).toBeHidden()
 expect(state.writes[0]).toEqual({method:'POST',body:{name:'Meta scan group',group_id:group,trigger_seconds:60,recovery_seconds:30,enabled:false}})
 const panel=page.getByTestId('availability-rules');await expect(panel).toContainText('Meta scan group')
 await panel.getByRole('button',{name:'编辑',exact:true}).click();await expect(dialog.locator('.el-select__wrapper')).toHaveClass(/is-disabled/)
 await dialog.locator('.el-switch').click();state.conflict()
 await dialog.getByRole('button',{name:'保存',exact:true}).click();await expect(dialog).toBeVisible();await expect(dialog).toContainText('变化')
 expect(state.writes.at(-1).body.version).toBe(1);expect(state.writes.at(-1).body.enabled).toBe(true)
})
test('current confirmation is explained beside latest evaluation; bilingual rendering',async({page})=>{
 await fixture(page,{locale:'en',rows:[{...saved,enabled:true,last_verdict:'unknown',last_error:'waiting_confirmation',last_evaluated_at:'2026-10-25T00:00:00Z'}]});await page.goto('/platform-alerts?tab=availability-rules')
 const panel=page.getByTestId('availability-rules');await expect(panel).toContainText('Awaiting confirmed operation');await expect(panel).toContainText('Meta group');await expect(panel).not.toContainText('monitor.availability')
})
test('module read is required and identity change clears a pending candidate dialog',async({page})=>{
 const state=await fixture(page);state.hold();await page.goto('/platform-alerts?tab=availability-rules')
 await page.getByRole('button',{name:'创建服务失联规则',exact:true}).click();await expect(page.getByRole('dialog')).toBeVisible()
 await setIdentity(page,identity(['monitor.platform_alert.read']));state.release()
 await expect(page.getByRole('dialog')).toBeHidden();await expect(page.getByTestId('availability-rules')).toBeHidden()
 expect(state.writes).toHaveLength(0)
})
test('notification category requires an explicit selection',async({page})=>{
 const {fixture:alerts}=await import('./platform-alerts.fixture')
 const state=await alerts(page,{keys:['monitor.platform_alert_notification.read','monitor.platform_alert_notification.update'],path:'/platform-alerts?tab=notifications'})
 await page.getByRole('button',{name:'新增通知目标',exact:true}).click();const dialog=page.getByRole('dialog')
 const checkbox=dialog.getByRole('checkbox',{name:'服务组失联',exact:true});await expect(checkbox).not.toBeChecked()
 await dialog.locator('label.el-checkbox').filter({hasText:'服务组失联'}).click();await expect(checkbox).toBeChecked()
 await dialog.getByRole('textbox').nth(0).fill('Group receiver');await dialog.getByRole('textbox').nth(1).fill('https://notify.example.test/hook');await dialog.getByRole('button',{name:'保存',exact:true}).click()
 await expect.poll(()=>state.requests.filter(r=>r.method==='POST').length).toBe(1);expect(state.requests.find(r=>r.method==='POST').body.categories).toEqual(['service_availability'])
})

test('no System module read means no group-management requests',async({page})=>{
 const state=await fixture(page,{permissions:['monitor.platform_alert.read','monitor.platform_service_availability_rule.read']});await page.goto('/platform-alerts?tab=availability-rules')
 await expect(page.getByTestId('pipeline-incidents')).toBeVisible();await expect(page.getByTestId('availability-rules')).toBeHidden();expect(state.reads).toHaveLength(0)
})
test('availability opening evidence stays readable without current rule access',async({page})=>{
 const {fixture:alerts}=await import('./platform-alerts.fixture')
 const evidence={...target,rule_id:id,rule_name:'Original group rule',rule_version:2,policy:{trigger_seconds:60,recovery_seconds:30},expected_replicas:2,online_replicas:0,intent_changed_at:'2026-10-25T00:00:00Z',confirmed_at:'2026-10-25T00:01:00Z',observed_from:'2026-10-25T00:02:00Z',observed_to:'2026-10-25T00:03:00Z',samples:5}
 await alerts(page,{keys:['monitor.platform_alert.read'],entries:[{id:42,version:1,name:'Original group rule',category:'service_availability',subject_kind:'service_group',subject_id:group,signal:id,severity:'warning',status:'open',opened_at:'2026-10-25T00:03:00Z',availability_evidence:evidence}]})
 const table=page.getByTestId('pipeline-incidents');await expect(table).toContainText('meta · 任务进程 · 元数据扫描')
 await table.getByRole('button',{name:'触发依据',exact:true}).click();const dialog=page.getByRole('dialog');await expect(dialog.getByTestId('availability-evidence')).toContainText('全部副本失联持续 60 秒');await expect(dialog).not.toContainText('没有完整的触发依据')
})
