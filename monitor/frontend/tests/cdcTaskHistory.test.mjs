import test from 'node:test'
import assert from 'node:assert/strict'
import { cdcTaskHistoryParams, validateCDCTaskHistory, cdcTaskHistoryValue, cdcTaskHistoryPrecisionLimited } from '../src/utils/cdcTaskHistory.js'
import { resolveCDCTaskRoute } from '../src/utils/cdcTasks.js'

const params = cdcTaskHistoryParams(1,'1h',Date.parse('2026-11-15T10:00:17Z'))
function history() {
 return { task_id:7,generation:1,start:params.start,end:params.end,queried_at:params.end,step_seconds:60,points:Array.from({length:60},(_,i)=>({timestamp:new Date(Date.parse(params.start)+i*60000).toISOString(),recorded_at:null,consumption_state:'',buffer_observation:null,postgresql_observation:null,delay_observation:null})) }
}
function buffer(value='10') {
 return {status:'available',sampled_at:'2026-11-15T09:00:02Z',expires_at:'2026-11-15T09:00:32Z',earliest_offset:'0',latest_offset:value,committed_offset:'0',lag_offsets:value,recovery_state:'available'}
}
test('Task history binds task, generation and a complete bounded time grid',()=>{
 const value=history();assert.equal(validateCDCTaskHistory(value,7,params),value)
 for(const mutate of [v=>v.task_id=8,v=>v.generation=2,v=>v.points.pop(),v=>v.step_seconds=1,v=>v.points[0].timestamp=params.end,v=>v.points[0].buffer_observation=buffer()]) {const v=history();mutate(v);assert.throws(()=>validateCDCTaskHistory(v,7,params))}
 const seven=cdcTaskHistoryParams(2,'7d',Date.parse(params.end));assert.equal(Date.parse(seven.end)-Date.parse(seven.start),604800000)
 const state=resolveCDCTaskRoute({view:'tasks',task_id:'7',task_range:'7d',task_metric:'delay_seconds'})
 assert.equal(state.historyRange,'7d');assert.equal(resolveCDCTaskRoute(state.query).changed,false)
})
test('History distinguishes pause, actual zero, missing, stale and exact integer overflow',()=>{
 const value=history(),point=value.points[0];Object.assign(point,{recorded_at:'2026-11-15T09:00:05Z',consumption_state:'paused',buffer_observation:buffer()})
 validateCDCTaskHistory(value,7,params)
 assert.equal(cdcTaskHistoryValue(point,'lag_offsets'),10)
 point.buffer_observation=buffer('0');assert.equal(cdcTaskHistoryValue(point,'lag_offsets'),0)
 assert.equal(cdcTaskHistoryValue(value.points[1],'lag_offsets'),null)
 point.buffer_observation=buffer('9007199254740993');assert.equal(cdcTaskHistoryValue(point,'lag_offsets'),null);assert.equal(cdcTaskHistoryPrecisionLimited(point,'lag_offsets'),true)
 point.recorded_at='2026-11-15T09:00:40Z';assert.equal(cdcTaskHistoryValue(point,'lag_offsets'),null)
 point.delay_observation={status:'available',sampled_at:'2026-11-15T09:00:39Z',expires_at:'2026-11-15T09:01:09Z',connected:true,delay_seconds:-.002}
 assert.equal(cdcTaskHistoryValue(point,'delay_seconds'),-.002)
})
