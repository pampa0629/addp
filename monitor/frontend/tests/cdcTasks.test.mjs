import test from 'node:test'
import assert from 'node:assert/strict'
import { cdcView, cdcEndpoints, resolveCDCTaskRoute, validateCDCTask, validateCDCTaskPage, validateCDCTaskExecution, validateCDCBuffer, validateCDCPostgreSQL, validateCDCDelay, cdcObservationState } from '../src/utils/cdcTasks.js'
export const task = () => ({ id: 7, name: 'Orders CDC', task_type: 'sync', status: 'running', desired_state: 'running', config: { runtime: { boundary: 'continuous' }, load: { change_detection: { type: 'cdc' } }, source: { locator: 'addp://engine/2/path/public/orders?type=table' }, target: { parent_locator: 'addp://engine/3/path/sales?type=schema', name: 'orders_copy' } }, capture: { generation: 1, status: 'running', buffer_observation: null, source_type:'postgresql', postgresql_observation:null, delay_observation:null }, last_execution_id: '10000000-0000-4000-8000-000000000001' })
test('CDC task view uses context defaults and canonical bounded URL state', () => {
 assert.equal(cdcView({},'tenant'),'tasks'); assert.equal(cdcView({},'platform'),'resources')
 assert.equal(cdcView({view:'tasks'},'platform'),'tasks')
 const state = resolveCDCTaskRoute({task_id:'7',page:'2',refresh:'off',scope:'infra',deployment_id:'foreign'})
 assert.deepEqual(state.query,{view:'tasks',page:'2',task_id:'7',refresh:'off'})
 assert.equal(resolveCDCTaskRoute(state.query).changed,false)
 assert.deepEqual(resolveCDCTaskRoute({task_id:'-7',page:'0',refresh:'1'}).query,{view:'tasks'})
})
test('CDC endpoints come only from task locators and task reads reject foreign/bounded data', () => {
 assert.deepEqual(cdcEndpoints(task()),{source:{engineID:2,path:'public.orders'},target:{engineID:3,path:'sales.orders_copy'}})
 assert.equal(validateCDCTask(task(),'7').id,7)
 for (const mutate of [t=>t.id=8,t=>t.config.runtime.boundary='bounded',t=>t.config.load.change_detection.type='watermark',t=>t.config.source.locator='https://foreign',t=>t.last_execution_id='bad']) {
  const value=task();mutate(value);assert.throws(()=>validateCDCTask(value,'7'),/invalid_resource_response/)
 }
 const value={items:[task()],total:1,page:1,page_size:20};assert.equal(validateCDCTaskPage(value,1).total,1)
 assert.throws(()=>validateCDCTaskPage({...value,items:[task(),task()]},1),/invalid_resource_response/)
})
test('Task chain accepts only its latest execution and same capture generation', () => {
 const value={execution_id:task().last_execution_id,module:'transfer',task_type:'sync',source_task_id:'7',execution_boundary:'continuous',metadata:{continuous:{capture:{generation:1}}}}
 assert.equal(validateCDCTaskExecution(value,task()),value)
 for (const changed of [{source_task_id:'8'},{module:'meta'},{execution_boundary:'bounded'},{execution_id:'foreign'},{metadata:{continuous:{capture:{generation:2}}}}]) assert.throws(()=>validateCDCTaskExecution({...value,...changed},task()),/invalid_resource_response/)
})

test('Task buffer preserves exact positions and rejects inconsistent or reused failure values', () => {
 const value={status:'available',sampled_at:'2026-11-12T00:00:00Z',expires_at:'2026-11-12T00:01:00Z',earliest_offset:'0',latest_offset:'9007199254740994',committed_offset:'9007199254740993',lag_offsets:'1',recovery_state:'available'}
 assert.equal(validateCDCBuffer(value).committed_offset,'9007199254740993')
 for (const changed of [{lag_offsets:'0'},{committed_offset:9},{status:'unavailable'},{recovery_state:'lost'},{expires_at:value.sampled_at}]) assert.throws(()=>validateCDCBuffer({...value,...changed}),/invalid_resource_response/)
 assert.equal(validateCDCBuffer(null),null)
})

test('Task source rejects partial/foreign data and preserves large WAL strings and native delay',()=>{
 const value={status:'available',sampled_at:'2026-11-12T00:00:00Z',expires_at:'2026-11-12T00:01:00Z',slot_active:true,wal_status:'reserved',retained_wal_bytes:'9007199254740993',unacknowledged_wal_bytes:null,safe_wal_size_bytes:null,wal_keep_unlimited:true,risk_state:'reserved'}
 assert.equal(validateCDCPostgreSQL(value).retained_wal_bytes,'9007199254740993')
 for(const change of [{status:'unavailable'},{risk_state:'lost'},{retained_wal_bytes:9007199254740993},{retained_wal_bytes:'-1'},{wal_status:'wrong'}])assert.throws(()=>validateCDCPostgreSQL({...value,...change}))
 assert.equal(cdcObservationState(value,Date.parse(value.expires_at)),'stale')
 const delay={status:'available',sampled_at:value.sampled_at,expires_at:value.expires_at,connected:true,delay_seconds:-.002}
 assert.equal(validateCDCDelay(delay).delay_seconds,-.002)
 for(const change of [{status:'no_sample'},{connected:false},{delay_seconds:Infinity}])assert.throws(()=>validateCDCDelay({...delay,...change}))
})
