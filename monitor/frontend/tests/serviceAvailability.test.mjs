import test from 'node:test'
import assert from 'node:assert/strict'
import { validAvailabilityPolicy, validateAvailabilityGroups, validateAvailabilityRules, availabilityEvidence } from '../src/utils/serviceAvailability.js'
import { platformAlertState, platformAlertQuery } from '../src/utils/platformAlerts.js'
const id='10000000-0000-4000-8000-000000000001', group_id='b'.repeat(64)
const target={deployment_id:'a'.repeat(64),module_name:'meta',role:'worker',runtime_name:'scan'}
const row={...target,id,group_id,name:'Meta scan',trigger_seconds:60,recovery_seconds:30,version:1,enabled:false,last_verdict:'paused',online_replicas:null}
test('group candidates and rule pages reject incomplete, duplicated, invalid facts',()=>{
 const g={...target,id:group_id,expected_replicas:2,desired_state:'running',state:'observed',online_replicas:1}
 assert.equal(validateAvailabilityGroups({data:[g],total:1})[0].online_replicas,1)
 for(const value of [{data:[g,g],total:2},{data:[g],total:2},{data:[{...g,expected_replicas:0}],total:1},{data:[{...g,role:'container'}],total:1}]) assert.throws(()=>validateAvailabilityGroups(value))
 const page={data:[row],total:1,page:1,page_size:20,total_pages:1}
 assert.deepEqual(validateAvailabilityRules(page,1).data,[row])
 for(const value of [{...page,total:2},{...page,page:2},{...page,data:[{...row,version:0}]},{...page,data:[{...row,recovery_seconds:16}]}]) assert.throws(()=>validateAvailabilityRules(value,1))
 for(const seconds of [0,14,16,301,60.5,'60',null])assert.equal(validAvailabilityPolicy({...row,trigger_seconds:seconds}),false)
})
test('only explicit new category and tab round trip with independent page',()=>{
 const query={tab:'availability-rules',availability_rule_page:'2',category:'service_availability'}
 assert.deepEqual(platformAlertQuery(platformAlertState(query)),query)
 assert.equal(platformAlertState({}).category,'')
})
test('opening evidence is displayed only with complete source identity and ordered times',()=>{
 const e={...target,rule_id:id,rule_version:1,rule_name:'Frozen',expected_replicas:2,online_replicas:0,policy:{trigger_seconds:60,recovery_seconds:30},samples:5,intent_changed_at:'2026-10-25T00:00:00Z',confirmed_at:'2026-10-25T00:01:00Z',observed_from:'2026-10-25T00:02:00Z',observed_to:'2026-10-25T00:03:00Z'}
 const event={category:'service_availability',subject_kind:'service_group',subject_id:group_id,signal:id,availability_evidence:e}
 assert.deepEqual(availabilityEvidence(event),e)
 for(const bad of [{...e,rule_id:'other'},{...e,samples:0},{...e,confirmed_at:'2026-10-25T00:04:00Z'},{...e,policy:{trigger_seconds:14,recovery_seconds:30}}])assert.equal(availabilityEvidence({...event,availability_evidence:bad}),null)
})
