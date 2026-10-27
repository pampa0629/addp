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
