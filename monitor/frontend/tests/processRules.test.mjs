import test from 'node:test'
import assert from 'node:assert/strict'
import { processRuleScale, validProcessPolicy, validateProcessRules, processAlertEvidence } from '../src/utils/processRules.js'
import { platformAlertQuery, platformAlertState } from '../src/utils/platformAlerts.js'
import { readFileSync, existsSync } from 'node:fs'
const policy = { trigger_value: 4 * 1024 ** 3, recovery_value: 2 * 1024 ** 3, trigger_seconds: 30, recovery_seconds: 60 }
const id='10000000-0000-4000-8000-000000000019'
const subject = { anchor_instance_id:1, module_name:'meta', role:'worker', runtime_instance_id:'start-one', process_started_at:'2026-10-20T00:00:00Z' }
test('process rule page and category retain independent navigation',()=>{
 const query={ tab:'process-rules',category:'process_resource',process_rule_page:'2' }
 assert.deepEqual(platformAlertQuery(platformAlertState(query)),query)
 assert.equal(processRuleScale('process.memory.resident_bytes'),1024**3)
 assert.equal(processRuleScale('process.cpu.core_equivalents'),1)
})
test('process thresholds use absolute units and explicitly bounded values',()=>{
 assert.ok(validProcessPolicy(policy,'process.memory.resident_bytes'))
 for(const p of [{recovery_value:0},{trigger_value:NaN},{trigger_value:Infinity},{trigger_seconds:16},{recovery_seconds:null},{recovery_value:policy.trigger_value}])assert.equal(validProcessPolicy({...policy,...p},'process.memory.resident_bytes'),false)
 assert.equal(validProcessPolicy(policy,'node.memory.used_percent'),false)
})
test('process evidence requires exact immutable identity and valid time boundaries',()=>{
 const row={ category:'process_resource',subject_kind:'module_instance',subject_id:'1',instance_id:'start-one',signal:id,process_evidence:{...subject,rule_id:id,rule_version:1,metric_key:'process.memory.resident_bytes',policy,query_policy_version:0,samples:3,minimum:5e9,maximum:6e9,latest:5.5e9,sampled_from:'2026-10-20T00:02:00Z',sampled_to:'2026-10-20T00:02:30Z',evaluated_at:'2026-10-20T00:02:31Z'} }
 assert.equal(processAlertEvidence(row),row.process_evidence)
 for(const patch of [{subject_id:'2'},{instance_id:'restart-two'},{category:'resource'},{process_evidence:{...row.process_evidence,process_started_at:'2026-10-20T00:03:00Z'}}])assert.equal(processAlertEvidence({...row,...patch}),null)
 const rule={...subject,...policy,id,version:1,name:'RSS',metric_key:'process.memory.resident_bytes',enabled:false,last_verdict:'paused',last_value:null}
 assert.equal(validateProcessRules({data:[rule],total:1,page:1,page_size:20},1).data[0],rule)
 assert.throws(()=>validateProcessRules({data:[rule,{...rule}],total:2,page:1,page_size:20},1))
})
test('instance rule management has one component implementation',()=>{
 const base=new URL('../src/components/',import.meta.url)
 assert.equal(existsSync(new URL('HTTPAlertRules.vue',base)),false)
 const view=readFileSync(new URL('../src/views/PlatformAlerts.vue',import.meta.url),'utf8')
 assert.match(view,/<InstanceAlertRules kind="http"/)
 assert.match(view,/<InstanceAlertRules kind="process"/)
})
