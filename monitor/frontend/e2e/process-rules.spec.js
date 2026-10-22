import { test, expect } from '@playwright/test'
import { backend as authentication } from './process-resources.fixture'
import { identity, setIdentity } from './monitoring-targets.fixture'
const permissions=['platform.module.read',...['read','create','update','delete'].map(k=>`monitor.platform_resource_rule.${k}`)]
async function fixture(page,opts={}) {
 await authentication(page,{ identity: opts.identity || identity(permissions), locale:opts.locale || 'zh-cn' })
 let rule=null, conflict=false
 const writes=[],reads=[]
 await page.route('**/api/v1/system/platform/module-instances?**',async route=>{
 const p=Object.fromEntries(new URL(route.request().url()).searchParams);reads.push(p)
 await route.fulfill({json:{data:[{id:1,module_name:'meta',instance_id:'process-one',role:p.role,status:'up',node_id:'',process_metrics_declared:true,process_started_at:'2026-10-20T00:00:00Z'}],total:1,page:Number(p.page),page_size:Number(p.page_size)}})
 })
 await page.route('**/api/v1/monitor/platform/process-resource-alert-rules**',async route=>{
 const req=route.request(),method=req.method();if(method==='GET'){await route.fulfill({json:{data:rule?[rule]:[],total:rule?1:0,page:1,page_size:20}});return}
 const input=req.postDataJSON();writes.push({method,input})
 if(conflict){await route.fulfill({status:409,json:{error_code:'resource_version_conflict'}});return}
 rule={id:'10000000-0000-4000-8000-000000000019',...input,version:1,module_name:'meta',role:'backend',anchor_instance_id:1,runtime_instance_id:'process-one',process_started_at:'2026-10-20T00:00:00Z',last_verdict:'paused',last_error:'',last_value:null}
 await route.fulfill({status:method==='POST'?201:200,json:rule})
 })
 return {writes,reads,conflict:()=>{conflict=true}}
}
test('process memory rule converts GiB, freezes instance and retains input on conflict',async({page},testInfo)=>{
 const state=await fixture(page);await page.goto('/platform-alerts?tab=process-rules')
 await page.getByRole('button',{name:'创建服务实例规则',exact:true}).click()
 const dialog=page.getByRole('dialog');await dialog.getByRole('textbox',{name:'名称',exact:true}).fill('RSS limit')
 await page.locator('.el-select').filter({has:page.getByRole('combobox',{name:'选择服务实例',exact:true})}).click()
 await page.getByRole('option',{name:'meta · process-one',exact:true}).click()
 await page.locator('.el-select').filter({has:page.getByRole('combobox',{name:'监控指标',exact:true})}).click()
 await page.getByRole('option',{name:'内存占用（RSS）',exact:true}).click()
 await expect(dialog).toContainText('单位：GiB')
 await page.getByRole('spinbutton',{name:'触发阈值',exact:true}).fill('2')
 await page.getByRole('spinbutton',{name:'恢复阈值',exact:true}).fill('1.5')
 await page.getByRole('button',{name:'保存',exact:true}).click()
 await expect.poll(()=>state.writes.length).toBe(1)
 expect(state.writes[0].input).toMatchObject({instance_id:1,metric_key:'process.memory.resident_bytes',trigger_value:2*1024**3,recovery_value:1.5*1024**3,enabled:false})
 expect(state.writes[0].input).not.toHaveProperty('route')
 expect(state.reads[0]).toMatchObject({status:'up',process_metrics_declared:'true'})
 await expect(page.getByTestId('process-rules')).toContainText('2 GiB')
 await page.getByTestId('process-rules').getByRole('button',{name:'编辑',exact:true}).click()
 await expect(dialog.locator('.el-select').filter({hasText:'meta · process-one'}).locator('.el-select__wrapper')).toHaveClass(/is-disabled/)
 state.conflict();await page.getByRole('spinbutton',{name:'触发阈值',exact:true}).fill('3')
 await page.getByRole('button',{name:'保存',exact:true}).click()
 await expect(dialog).toContainText('规则已变化')
 await expect(page.getByRole('spinbutton',{name:'触发阈值',exact:true})).toHaveValue('3')
 expect(state.writes[1].input.version).toBe(1)
 await page.screenshot({path:testInfo.outputPath('process-rule-cas.png'),fullPage:true})
 await setIdentity(page,identity([]));await expect(dialog).not.toBeVisible();await expect(page.getByTestId('process-rules')).toHaveCount(0)
})
for(const [name,ctx,principal] of [['tenant',{type:'tenant',tenant_id:1},'user'],['machine',{type:'platform'},'service_principal']])test(`process rules deny ${name}`,async({page})=>{
 const state=await fixture(page,{identity:identity(permissions,ctx,principal)});await page.goto('/platform-alerts?tab=process-rules');await expect(page.getByTestId('process-rules')).toHaveCount(0);expect(state.reads).toEqual([])
})
test('English CPU threshold explanation is beside its input',async({page})=>{
 await fixture(page,{locale:'en'});await page.goto('/platform-alerts?tab=process-rules');await page.getByRole('button',{name:'Create instance rule',exact:true}).click();await expect(page.getByRole('dialog')).toContainText('Unit: cores.');await expect(page.getByRole('dialog')).not.toContainText('monitor.processRules.')
})

for (const [metric, label, latest, unit] of [['process.cpu.core_equivalents','CPU 占用核数（1 分钟）',3,'核'],['process.memory.resident_bytes','内存占用（RSS）',3*1024**3,'GiB']]) test(`process incident shows frozen ${metric} evidence without reading current rules`,async({page})=>{
 const { fixture: alertFixture } = await import('./platform-alerts.fixture')
 const rule='10000000-0000-4000-8000-000000000019',scale=unit==='GiB'?1024**3:1
 const evidence={rule_id:rule,rule_version:1,anchor_instance_id:1,module_name:'meta',role:'worker',runtime_instance_id:'process-one',process_started_at:'2026-10-20T00:00:00Z',metric_key:metric,policy:{trigger_value:2*scale,recovery_value:scale,trigger_seconds:30,recovery_seconds:30},query_policy_version:0,samples:3,minimum:latest,maximum:latest,latest,sampled_from:'2026-10-20T00:02:00Z',sampled_to:'2026-10-20T00:02:30Z',evaluated_at:'2026-10-20T00:02:31Z'}
 const state=await alertFixture(page,{keys:['monitor.platform_alert.read'],entries:[{id:9,version:1,category:'process_resource',subject_kind:'module_instance',subject_id:'1',instance_id:'process-one',signal:rule,severity:'warning',status:'open',opened_at:evidence.evaluated_at,process_evidence:evidence}]})
 await expect(page.getByTestId('pipeline-incidents')).toContainText('meta · 任务进程 · process-one')
 await expect(page.getByTestId('pipeline-incidents')).toContainText(label)
 await page.getByTestId('pipeline-incidents').getByRole('button').click()
 const dialog=page.getByRole('dialog');await expect(dialog).toContainText(`3.00 ${unit}`);await expect(dialog).toContainText(`2.00 ${unit}`)
 expect(state.requests.some(r=>r.path.includes('alert-rules'))).toBe(false)
})

test('process notifications require an explicit new category selection',async({page})=>{
 const { fixture: alertFixture } = await import('./platform-alerts.fixture')
 const state=await alertFixture(page,{keys:['monitor.platform_alert_notification.read','monitor.platform_alert_notification.update'],path:'/platform-alerts?tab=notifications'})
 await page.getByRole('button',{name:'新增通知目标',exact:true}).click()
 const dialog=page.getByRole('dialog');const category=dialog.getByRole('checkbox',{name:'服务实例资源',exact:true})
 await expect(category).not.toBeChecked();await dialog.locator('label.el-checkbox').filter({hasText:'服务实例资源'}).click();await expect(category).toBeChecked()
 await dialog.getByRole('textbox').nth(0).fill('Process receiver');await dialog.getByRole('textbox').nth(1).fill('https://notify.example.test/hook')
 await dialog.getByRole('button',{name:'保存',exact:true}).click()
 await expect.poll(()=>state.requests.filter(r=>r.method==='POST').length).toBe(1)
 expect(state.requests.find(r=>r.method==='POST').body.categories).toEqual(['process_resource'])
})
