import { expect, test } from '@playwright/test'
import { createHash } from 'node:crypto'
import { mkdtempSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { fixture, resourceIncident } from './platform-alerts.fixture'
import { node, setIdentity, identity, mount } from './node-resources.fixture'

const metric = 'node.filesystem.used_percent'
const keys = ['monitor.platform_alert.read','monitor.platform_alert.update','monitor.resource_observation.read','platform.host_node.read',...['read','create','update','delete'].map(action=>`monitor.platform_resource_rule.${action}`)]
const mountLabel = dimensions => `${dimensions.mountpoint} · ${dimensions.device} · ${dimensions.fstype}`
const writes = state => state.requests.filter(row=>row.path.includes('/resource-alert-rules')&&row.method!=='GET')
const dialog = page => page.getByRole('dialog').filter({has:page.getByTestId('rule-name')})
async function openFilesystem(page, english=false) {
 await page.getByTestId('resource-rule-create').click()
 await dialog(page).getByTestId('rule-name').fill('Data capacity')
 await dialog(page).getByTestId('target-node').click()
 await page.getByRole('option',{name:'节点甲',exact:true}).click()
 await dialog(page).getByTestId('resource-rule-metric').click()
 await page.getByRole('option',{name:english?'Filesystem available-capacity usage':'文件系统可用容量使用率',exact:true}).click()
}
for (const locale of ['zh-cn','en']) test(`filesystem rule selects exact mount, saves disabled, and can stop missing mount (${locale})`,async({page})=>{
 const english=locale==='en'
 const other={...mount,fstype:'xfs'}
 const state=await fixture(page,{keys,locale,resources:true,resourceOptions:{mounts:[mount,other]},path:'/platform-alerts?tab=rules'})
 expect(state.resourceState.reads.filter(row=>row.path.endsWith('/resource_observations'))).toHaveLength(0)
 await openFilesystem(page,english)
 await dialog(page).getByTestId('filesystem-rule-mount').click()
 await page.getByRole('option',{name:mountLabel(other),exact:true}).click()
 await expect(dialog(page)).toContainText(english?'permanently removed':'永久卸载')
 await dialog(page).getByRole('button',{name:english?'Save':'保存',exact:true}).click()
 await expect(dialog(page)).toBeHidden()
 expect(writes(state)[0].body).toMatchObject({metric_key:metric,node_id:node,dimensions:other,enabled:false,trigger_percent:85,recovery_percent:80,trigger_seconds:300,recovery_seconds:120})
 const table=page.getByTestId('resource-rules')
 await expect(table).toContainText(mountLabel(other))
 await table.getByRole('button',{name:english?'Edit':'编辑',exact:true}).click()
 await expect(dialog(page).getByTestId('resource-rule-metric').locator('.el-select__wrapper')).toHaveClass(/is-disabled/)
 await expect(dialog(page).getByTestId('filesystem-rule-mount')).toHaveCount(0)
 await dialog(page).locator('.el-switch').click()
 await dialog(page).getByRole('button',{name:english?'Save':'保存',exact:true}).click()
 await expect(dialog(page)).toBeHidden()
 expect(writes(state)[1].body).toMatchObject({dimensions:other,enabled:true,version:1})
 state.resourceState.filesystemMode='unavailable'
 const before=state.resourceState.reads.filter(row=>row.path.endsWith('/resource_observations')).length
 await table.getByRole('button',{name:english?'Edit':'编辑',exact:true}).click()
 await dialog(page).locator('.el-switch').click()
 await page.setViewportSize({width:620,height:900})
 await expect.poll(async () => (await dialog(page).boundingBox())?.y ?? -1).toBeGreaterThanOrEqual(0)
 await expect.poll(async () => { const box=await dialog(page).boundingBox(); return box ? box.y+box.height : Infinity }).toBeLessThanOrEqual(901)
 const folder=mkdtempSync(join(tmpdir(),'addp-filesystem-rule-'))
 await page.screenshot({path:join(folder,`${locale}.png`),animations:'disabled'});console.log(`Filesystem rule screenshot: ${folder}`)
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
 await dialog(page).getByRole('button',{name:english?'Save':'保存',exact:true}).click()
 await expect(dialog(page)).toBeHidden()
 expect(writes(state)[2].body).toMatchObject({dimensions:other,enabled:false,version:2})
 expect(state.resourceState.reads.filter(row=>row.path.endsWith('/resource_observations'))).toHaveLength(before)
})

test('filesystem unavailable and budget errors never create a guessed mount or rule',async({page})=>{
 const state=await fixture(page,{keys,resources:true,path:'/platform-alerts?tab=rules'})
 state.resourceState.filesystemMode='budget'
 await openFilesystem(page)
 await expect(dialog(page).getByTestId('filesystem-rule-mount-error')).toContainText('查询超过当前预算')
 await dialog(page).getByRole('button',{name:'保存',exact:true}).click()
 expect(writes(state)).toHaveLength(0)
 await dialog(page).getByRole('button',{name:'关闭',exact:true}).click()
 state.resourceState.filesystemMode='filesystem-uncollected'
 await openFilesystem(page)
 await expect(dialog(page).getByTestId('filesystem-rule-mount-error')).toContainText('暂无有效')
 await expect(dialog(page).locator('input:not([readonly])')).not.toHaveCount(0)
 expect(writes(state)).toHaveLength(0)
})

test('late mount options cannot survive switching host and revoking permissions',async({page})=>{
 const state=await fixture(page,{keys,resources:true,resourceOptions:{holdInstant:true},path:'/platform-alerts?tab=rules'})
 await openFilesystem(page)
 await expect.poll(()=>state.resourceState.reads.filter(row=>row.path.endsWith('/resource_observations')).length).toBe(1)
 await dialog(page).getByTestId('target-node').click()
 await page.getByRole('option',{name:'节点乙',exact:true}).click()
 await expect(dialog(page).getByTestId('filesystem-rule-mount')).toBeEnabled()
 state.resourceState.release()
 await setIdentity(page,identity(['monitor.platform_resource_rule.read']))
 await expect(dialog(page)).toHaveCount(0)
 await expect(page.getByTestId('resource-rule-create')).toHaveCount(0)
 expect(writes(state)).toHaveLength(0)
})

for (const locale of ['zh-cn','en']) test(`filesystem evidence keeps frozen capacity and full trend dimensions (${locale})`,async({page})=>{
 const entry=structuredClone(resourceIncident), english=locale==='en'
 entry.name='Data filesystem capacity';entry.resource_evidence.filesystem={dimensions:mount,total_bytes:100*1024**3,used_bytes:90*1024**3,available_bytes:10*1024**3}
 entry.signal=metric+':'+createHash('sha256').update(JSON.stringify(Object.fromEntries(Object.entries(mount).sort()))).digest('hex')
 const state=await fixture(page,{keys,locale,resources:true,entries:[entry],path:'/platform-alerts?status=all'})
 await page.getByRole('button',{name:english?'Trigger evidence':'触发依据',exact:true}).click()
 const evidence=page.getByTestId('resource-alert-evidence')
 await expect(evidence).toContainText(mount.mountpoint)
 await expect(evidence).toContainText('100.00 GiB');await expect(evidence).toContainText('90.00 GiB');await expect(evidence).toContainText('10.00 GiB')
 await evidence.getByRole('button',{name:english?'View trigger-period trend':'查看触发时段趋势',exact:true}).click()
 await expect(page.getByTestId('resource-chart').locator('canvas')).toBeVisible()
 expect(Object.fromEntries(new URL(page.url()).searchParams)).toMatchObject({...mount,metric,refresh:'off'})
 expect(state.resourceState.reads.filter(row=>row.path.endsWith('/resource_trends')).at(-1).query).toMatchObject({...mount,metrics:metric})
})


const shortcutRows = page => page.getByTestId('resource-filesystem-table').locator('.el-table__body-wrapper tbody tr')
async function shortcutSetup(page, options = {}) {
 const state = await fixture(page, { keys, resources:true, resourceOptions:{ mounts:[mount,{...mount,fstype:'xfs'}], ...options.resourceOptions }, ...options, path:'/platform-alerts?tab=rules' })
 await page.goto(options.detail ? `/node-resources/${node}?refresh=off` : '/node-resources?refresh=off&search=keep')
 if (!options.detail) await page.getByTestId(`resource-list-filesystem-${node}`).click()
 await expect(shortcutRows(page)).toHaveCount(options.resourceOptions?.mounts?.length || 2)
 return state
}
for (const locale of ['zh-cn','en']) test(`mount row opens the sole editor with exact identity and explicit disabled creation (${locale})`,async({page})=>{
 const english=locale==='en'
 const state=await shortcutSetup(page,{locale})
 const location=page.url(), before=state.requests.filter(row=>row.method==='GET'&&row.path.includes('/resource-alert-rules')).length
 await shortcutRows(page).nth(1).getByTestId('filesystem-create-alert').click()
 await expect(dialog(page)).toBeVisible()
 await expect(dialog(page).getByTestId('filesystem-rule-fixed-mount')).toHaveValue(mountLabel({...mount,fstype:'xfs'}))
 await expect(dialog(page).getByTestId('resource-rule-metric').locator('.el-select__wrapper')).toHaveClass(/is-disabled/)
 await expect(dialog(page).getByRole('button',{name:english?'Save':'保存',exact:true})).toBeEnabled()
 await expect(dialog(page).getByTestId('resource-rule-fixed-host')).toHaveValue('节点甲')
 expect(writes(state)).toHaveLength(0)
 expect(page.url()).toBe(location)
 await dialog(page).getByRole('button',{name:english?'Close':'关闭',exact:true}).click()
 expect(writes(state)).toHaveLength(0)
 await shortcutRows(page).nth(1).getByTestId('filesystem-create-alert').click()
 await expect(dialog(page).getByRole('button',{name:english?'Save':'保存',exact:true})).toBeEnabled()
 await dialog(page).getByTestId('rule-name').fill('Data capacity')
 await page.setViewportSize({width:620,height:900})
 await expect.poll(async()=>{const b=await dialog(page).boundingBox();return b?b.y+b.height:Infinity}).toBeLessThanOrEqual(901)
 const folder=mkdtempSync(join(tmpdir(),'addp-mount-capacity-shortcut-'))
 await page.screenshot({path:join(folder,locale+'.png'),animations:'disabled'});console.log(`Mount capacity shortcut screenshot: ${folder}`)
 await dialog(page).getByRole('button',{name:english?'Save':'保存',exact:true}).click()
 await expect(dialog(page)).toBeHidden()
 expect(writes(state)).toHaveLength(1)
 expect(writes(state)[0].body).toMatchObject({node_id:node,metric_key:metric,dimensions:{...mount,fstype:'xfs'},enabled:false,trigger_percent:85,recovery_percent:80,trigger_seconds:300,recovery_seconds:120})
 await page.screenshot({path:join(folder,locale+'-row.png'),animations:'disabled'})
 expect(state.requests.filter(row=>row.method==='GET'&&row.path.includes('/resource-alert-rules'))).toHaveLength(before+2)
 expect(page.url()).toBe(location)
 await page.reload()
 await expect(dialog(page)).toHaveCount(0)
 expect(writes(state)).toHaveLength(1)
})

test('host detail uses server presets and accepts a valid zero capacity ratio',async({page})=>{
 const state=await shortcutSetup(page,{detail:true,resourceOptions:{mounts:[mount],observationResponse:body=>{
  for(const series of body.series) if(series.metric_key===metric) series.points[0].value=0
 }}})
 await page.route('**/api/v1/monitor/platform/resource-alert-rules?*',async route=>{
  if(route.request().method()!=='GET') return route.fallback()
  await route.fulfill({json:{data:[],total:0,presets:[{metric_key:metric,policy:{trigger_percent:91,recovery_percent:81,trigger_seconds:45,recovery_seconds:30}}]}})
 })
 await shortcutRows(page).first().getByTestId('filesystem-create-alert').click()
 await expect(dialog(page).getByRole('button',{name:'保存',exact:true})).toBeEnabled()
 await dialog(page).getByTestId('rule-name').fill('Root capacity')
 await dialog(page).getByRole('button',{name:'保存',exact:true}).click()
 await expect(dialog(page)).toBeHidden()
 expect(writes(state)[0].body).toMatchObject({trigger_percent:91,recovery_percent:81,trigger_seconds:45,recovery_seconds:30,enabled:false,dimensions:mount})
})

test('shortcut never substitutes a disappeared mount, a rejected source or a changed node version',async({page})=>{
 let wrongVersion=false
 const state=await shortcutSetup(page,{resourceOptions:{mounts:[mount],observationResponse:body=>{if(wrongVersion)body.node_version++}}})
 for(const mode of ['missing','budget','version']) {
  state.resourceState.filesystemMode=mode==='budget'?'budget':''
  state.resourceState.mounts=mode==='missing'?[{...mount,fstype:'xfs'}]:[mount]
  wrongVersion=mode==='version'
  await shortcutRows(page).first().getByTestId('filesystem-create-alert').click()
  await expect(dialog(page).getByTestId('filesystem-rule-mount-error')).toBeVisible()
  await expect(dialog(page).getByTestId('filesystem-rule-fixed-mount')).toHaveValue(mountLabel(mount))
  await expect(dialog(page).getByRole('button',{name:'保存',exact:true})).toBeDisabled()
  expect(writes(state)).toHaveLength(0)
  await dialog(page).getByRole('button',{name:'关闭',exact:true}).click()
 }
})

test('mount action requires both rule permissions and valid current capacity, without speculative reads',async({page})=>{
 const state=await shortcutSetup(page,{keys:keys.filter(key=>key!=='monitor.platform_resource_rule.create'),resourceOptions:{mounts:[mount]}})
 await expect(page.getByTestId('filesystem-create-alert')).toHaveCount(0)
 const before=state.requests.filter(row=>row.path.includes('/resource-alert-rules')).length
 await setIdentity(page,identity(keys))
 await page.getByTestId(`resource-list-filesystem-${node}`).click()
 await expect(page.getByTestId('filesystem-create-alert')).toBeVisible()
 expect(state.requests.filter(row=>row.path.includes('/resource-alert-rules'))).toHaveLength(before)
 state.resourceState.filesystemMode='no-sample'
 await page.getByTestId('resource-detail-refresh').click()
 await expect(page.getByTestId('filesystem-create-alert')).toBeDisabled()
 await setIdentity(page,identity(keys.filter(key=>key!=='monitor.platform_resource_rule.read')))
 await page.getByTestId(`resource-list-filesystem-${node}`).click()
 await expect(page.getByTestId('filesystem-create-alert')).toHaveCount(0)
 expect(writes(state)).toHaveLength(0)
})

test('closing or revoking an in-flight shortcut cannot repopulate its draft or save a rule',async({page})=>{
 const state=await shortcutSetup(page,{resourceOptions:{mounts:[mount]}})
 state.resourceState.hold()
 await shortcutRows(page).first().getByTestId('filesystem-create-alert').click()
 await expect(dialog(page).getByTestId('filesystem-rule-mount')).toHaveCount(0)
 await expect(dialog(page).getByRole('button',{name:'保存',exact:true})).toBeDisabled()
 await dialog(page).getByRole('button',{name:'关闭',exact:true}).click()
 state.resourceState.release()
 await expect(dialog(page)).toBeHidden()
 await shortcutRows(page).first().getByTestId('filesystem-create-alert').click()
 await expect(dialog(page).getByRole('button',{name:'保存',exact:true})).toBeEnabled()
 await setIdentity(page,identity(keys.filter(key=>key!=='monitor.platform_resource_rule.create')))
 await expect(dialog(page)).toHaveCount(0)
 await expect(page.getByTestId('filesystem-create-alert')).toHaveCount(0)
 expect(writes(state)).toHaveLength(0)
})


test('shortcut rechecks host access and preserves a conflicted draft without automatic writes',async({page})=>{
 const state=await shortcutSetup(page,{ruleConflict:true,resourceOptions:{mounts:[mount]}})
 let denied=true
 await page.route(`**/api/v1/system/platform/host_nodes/${node}`,route=>denied?route.fulfill({status:403,json:{error:'Host access denied'}}):route.fallback())
 await shortcutRows(page).first().getByTestId('filesystem-create-alert').click()
 await expect(dialog(page)).toContainText('Host access denied')
 await expect(dialog(page).getByRole('button',{name:'保存',exact:true})).toBeDisabled()
 expect(writes(state)).toHaveLength(0)
 await dialog(page).getByRole('button',{name:'关闭',exact:true}).click()
 denied=false
 await shortcutRows(page).first().getByTestId('filesystem-create-alert').click()
 await expect(dialog(page).getByRole('button',{name:'保存',exact:true})).toBeEnabled()
 await dialog(page).getByTestId('rule-name').fill('Preserve this name')
 await dialog(page).getByRole('button',{name:'保存',exact:true}).click()
 await expect(dialog(page)).toContainText('当前输入已保留')
 await expect(dialog(page).getByTestId('rule-name')).toHaveValue('Preserve this name')
 expect(writes(state)).toHaveLength(1)
 await dialog(page).getByRole('button',{name:'关闭',exact:true}).click()
 expect(writes(state)).toHaveLength(1)
})

test('an explicit owner denial during host refresh clears an already open capacity draft',async({page})=>{
 await page.clock.install()
 const state=await shortcutSetup(page,{detail:true,resourceOptions:{mounts:[mount]}})
 await page.goto(`/node-resources/${node}?refresh=10`)
 await expect(shortcutRows(page)).toHaveCount(1)
 await shortcutRows(page).first().getByTestId('filesystem-create-alert').click()
 await expect(dialog(page).getByRole('button',{name:'保存',exact:true})).toBeEnabled()
 await dialog(page).getByTestId('rule-name').fill('Must be cleared')
 state.resourceState.mode='denied'
 await page.clock.fastForward(10001)
 await expect(page.getByTestId('resource-error')).toBeVisible()
 await expect(dialog(page)).toBeHidden()
 expect(writes(state)).toHaveLength(0)
})


const lookupKeys = ['platform.host_node.read','monitor.resource_observation.read','monitor.platform_resource_rule.read']
const ruleForMount = (dimensions,name='Exact mount',id='a0e9964c-e2d3-46ba-b151-52249cff528e') => ({id,node_id:node,metric_key:metric,dimensions,name,version:1,enabled:false,trigger_percent:85,recovery_percent:80,trigger_seconds:300,recovery_seconds:120,last_verdict:'paused',last_error:''})
const lookupQuery = dimensions => new URLSearchParams({tab:'rules',node_id:node,metric_key:metric,...dimensions}).toString()
for(const locale of ['zh-cn','en']) test(`view mount rules is exact, read-only, reloadable and returns to the host (${locale})`,async({page})=>{
 const english=locale==='en', other={...mount,fstype:'xfs'}
 const unrelated=Array.from({length:25},(_,i)=>ruleForMount({...mount,device:'/dev/other'+i},'Other mount '+i))
 const state=await shortcutSetup(page,{locale,keys:lookupKeys,rules:[...unrelated,ruleForMount(other,'Exact xfs')]})
 const location=page.url()
 await expect(page.getByTestId('filesystem-create-alert')).toHaveCount(0)
 await shortcutRows(page).nth(1).getByTestId('filesystem-view-rules').click()
 const rules=page.getByTestId('resource-rules')
 await expect(rules).toContainText('Exact xfs')
 await expect(rules).not.toContainText('Other mount')
 await expect(rules.getByRole('button',{name:english?'Edit':'编辑',exact:true})).toHaveCount(0)
 await expect(page.getByTestId('resource-rule-create')).toHaveCount(0)
 expect(state.requests.filter(r=>r.path.includes('/resource-alert-rules')&&r.method==='GET').at(-1).query).toMatchObject({node_id:node,metric_key:metric,...other,page:'1'})
 expect(writes(state)).toHaveLength(0)
 expect(Object.fromEntries(new URL(page.url()).searchParams)).toMatchObject({tab:'rules',node_id:node,metric_key:metric,...other})
 await page.setViewportSize({width:620,height:900})
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
 const folder=mkdtempSync(join(tmpdir(),'addp-mount-rule-view-'))
 await page.screenshot({path:join(folder,locale+'.png'),animations:'disabled'});console.log(`Mount rule view screenshot: ${folder}`)
 await page.reload()
 await expect(page.getByTestId('resource-rules')).toContainText('Exact xfs')
 await page.goBack()
 await expect(page).toHaveURL(location)
 await expect(page.getByTestId('resource-search')).toHaveValue('keep')
})

test('missing capacity samples still allow reading and stopping an existing enabled mount rule',async({page})=>{
 const row={...ruleForMount(mount),enabled:true}
 const state=await shortcutSetup(page,{detail:true,keys:[...lookupKeys,'monitor.platform_resource_rule.update'],rules:[row],resourceOptions:{mounts:[mount],observationResponse:body=>{for(const series of body.series)if(series.metric_key===metric)Object.assign(series.points[0],{data_state:'no_data',value:null,sampled_at:null})}}})
 await shortcutRows(page).first().getByTestId('filesystem-view-rules').click()
 await expect(page.getByTestId('resource-rules')).toContainText(row.name)
 const before=state.resourceState.reads.filter(r=>r.path.endsWith('/resource_observations')).length
 await page.getByTestId('resource-rules').getByRole('button',{name:'编辑',exact:true}).click()
 await dialog(page).locator('.el-switch').click()
 await dialog(page).getByRole('button',{name:'保存',exact:true}).click()
 await expect(dialog(page)).toBeHidden()
 expect(writes(state)).toHaveLength(1)
 expect(writes(state)[0].body).toMatchObject({enabled:false,version:1,dimensions:mount})
 expect(state.resourceState.reads.filter(r=>r.path.endsWith('/resource_observations'))).toHaveLength(before)
})

test('view rules distinguishes no matching rule from a failed query and never creates automatically',async({page})=>{
 const state=await shortcutSetup(page,{keys:lookupKeys,rules:[ruleForMount({...mount,device:'/dev/other'})],resourceOptions:{mounts:[mount]}})
 await shortcutRows(page).first().getByTestId('filesystem-view-rules').click()
 await expect(page.getByTestId('resource-rules')).toContainText('此挂载暂无容量规则')
 await page.route('**/api/v1/monitor/platform/resource-alert-rules?*',route=>route.fulfill({status:503,json:{error:'Rule lookup unavailable'}}))
 await page.reload()
 await expect(page.getByTestId('resource-rules')).toContainText('Rule lookup unavailable')
 await expect(page.getByTestId('resource-rules')).not.toContainText('此挂载暂无容量规则')
 expect(writes(state)).toHaveLength(0)
})

test('invalid or partial mount rule URLs fail closed without querying all rules',async({page})=>{
 const state=await fixture(page,{keys:lookupKeys,resources:true,path:'/platform-alerts?tab=rules&device=x'})
 for(const query of ['tab=rules&device=x',lookupQuery(mount)+'&fstype=xfs',lookupQuery(mount).replace('node.filesystem.used_percent','node.cpu.busy_percent')]) {
  await page.goto('/platform-alerts?'+query)
  await expect(page.getByTestId('resource-rules')).toContainText('挂载规则筛选不完整或无效')
 }
 expect(state.requests.filter(r=>r.path.includes('/resource-alert-rules'))).toHaveLength(0)
})

test('view mount rules hides on rule-read revocation without granting it through create rights',async({page})=>{
 const state=await shortcutSetup(page,{keys,resourceOptions:{mounts:[mount]}})
 await expect(page.getByTestId('filesystem-view-rules')).toBeVisible()
 await setIdentity(page,identity(keys.filter(k=>k!=='monitor.platform_resource_rule.read')))
 await page.getByTestId(`resource-list-filesystem-${node}`).click()
 await expect(page.getByTestId('filesystem-view-rules')).toHaveCount(0)
 expect(writes(state)).toHaveLength(0)
})

test('switching precise mount filters cancels late lookups and revocation cannot restore their rows',async({page})=>{
 const other={...mount,fstype:'xfs'}
 const state=await shortcutSetup(page,{keys:lookupKeys,rules:[ruleForMount(mount,'Old ext4'),ruleForMount(other,'New xfs')]})
 state.hold()
 await shortcutRows(page).first().getByTestId('filesystem-view-rules').click()
 await expect.poll(()=>state.requests.filter(r=>r.path.includes('/resource-alert-rules')&&r.query.device).length).toBe(1)
 await page.goto('/platform-alerts?'+lookupQuery(other))
 await expect.poll(()=>state.requests.filter(r=>r.path.includes('/resource-alert-rules')&&r.query.device).length).toBe(2)
 state.release()
 await expect(page.getByTestId('resource-rules')).toContainText('New xfs')
 await expect(page.getByTestId('resource-rules')).not.toContainText('Old ext4')
 await setIdentity(page,identity(['platform.host_node.read','monitor.resource_observation.read']))
 await expect(page.getByTestId('resource-rules')).toHaveCount(0)
 expect(writes(state)).toHaveLength(0)
})
