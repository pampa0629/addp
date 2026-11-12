import { identity } from './monitoring-targets.fixture'
const executionID = '10000000-0000-4000-8000-000000000001'
export function task(id=7) { return { id, name: `Orders CDC ${id}`, task_type:'sync', status:'running', desired_state:'running', last_execution_id: executionID, config:{runtime:{boundary:'continuous'},load:{change_detection:{type:'cdc'}},source:{locator:`addp://engine/2/path/public/orders_${id}?type=table`},target:{parent_locator:'addp://engine/3/path/sales?type=schema',name:`orders_copy_${id}`}},capture:{generation:1,status:'running',buffer_observation:null,connector_status:'RUNNING',source_status:'ONLINE',last_observed_at:'2026-11-12T02:00:00Z'} } }
export async function cdcTaskBackend(page,{locale='zh-cn',permissions=['transfer.task.read','monitor.execution.read']}={}) {
 let mode='',held=null,release
 const calls=[]
 await page.addInitScript(lang=>localStorage.setItem('addp-lang',lang),locale)
 await page.route('**/module-health/**',r=>r.fulfill({json:{status:'ready'}}))
 await page.route('**/api/v1/**',async route=>{
  const url=new URL(route.request().url()),path=url.pathname
  let body,status=200
  if(path.endsWith('/system/refresh'))body={access_token:'fixture-token',expires_in:3600}
  else if(path.endsWith('/system/users/me'))body={id:9,username:'fixture-user'}
  else if(path.endsWith('/system/auth/context'))body=identity(permissions,{type:'tenant',tenant_id:'1',tenant_membership_id:'1'})
  else {
   calls.push({path,params:Object.fromEntries(url.searchParams)})
   if(mode==='denied'){status=403;body={error_code:'permission_denied'}}
   else if(path.endsWith('/transfer/task-definitions')) body={items:mode==='empty'?[]:[task(7),task(8)],total:mode==='empty'?0:2,page:Number(url.searchParams.get('page')),page_size:20}
   else if(path.includes('/transfer/task-definitions/')){body=task(Number(path.split('/').at(-1)));if(mode.startsWith('buffer-')){const sampled=new Date().toISOString();body.desired_state='paused';body.status='paused';body.capture.buffer_observation={status:mode==='buffer-failed'?'unavailable':'available',sampled_at:sampled,expires_at:new Date(Date.now()+30000).toISOString(),earliest_offset:mode==='buffer-failed'?null:mode==='buffer-lost'?'11':'0',latest_offset:mode==='buffer-failed'?null:mode==='buffer-growth'?'110':'10',committed_offset:mode==='buffer-failed'?null:'10',lag_offsets:mode==='buffer-failed'?null:mode==='buffer-growth'?'100':'0',recovery_state:mode==='buffer-failed'?'unknown':mode==='buffer-lost'?'lost':'available'};if(mode==='buffer-lost'){body.capture.buffer_observation.latest_offset='110';body.capture.buffer_observation.lag_offsets='100'}};if(mode==='not-started'){delete body.capture;delete body.last_execution_id;body.status='idle';body.desired_state='stopped'}}
   else if(mode==='execution-denied' && path.includes('/monitor/executions/')){status=403;body={error_code:'permission_denied'}}
   else if(path.includes('/monitor/executions/by-execution-id/'))body={execution_id:executionID,module:'transfer',task_type:'sync',source_task_id:mode==='foreign'?'8':'7',execution_boundary:'continuous',status:'running',metadata:{continuous:{capture:{generation:1},partitions:{0:{type:'kafka_offset',version:'v1',values:{next_offset:'9'}}},diagnostics:{sampled_at:'2026-11-12T02:00:00Z',partitions:{0:{next_offset:9,latest_offset:12,lag_records:mode==='missing'?null:3}}}}}}
   else throw new Error(`Unexpected task-chain API ${path}`)
   if(held)await held
  }
  await route.fulfill({status,json:body})
 })
 return {calls,setMode:v=>mode=v,hold:()=>{held=new Promise(done=>release=done)},release:()=>{release?.();held=null}}
}
