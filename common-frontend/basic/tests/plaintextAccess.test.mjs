import {test} from 'node:test'
import assert from 'node:assert/strict'
import {readFileSync} from 'node:fs'
import {plaintextRequestPayload} from '../src/utils/plaintextAccess.mjs'

test('one request explicitly names both approved outlets, without a user-supplied actor',()=>{
  const input={assessmentId:'field',owners:['manager','develop'],expiresAt:new Date(10000),rationale:' check ',maximum:20000}
  const body=plaintextRequestPayload(input,1000)
  assert.deepEqual(body.scopes,[{consumer_owner:'manager',action:'preview'},{consumer_owner:'develop',action:'query'}])
  assert.deepEqual(Object.keys(body).sort(),['assessment_id','rationale','requested_expires_at','scopes'])
  for(const owners of [[],['manager','manager'],['transfer'],['service'],['graph']])assert.throws(()=>plaintextRequestPayload({...input,owners},1000))
  assert.throws(()=>plaintextRequestPayload({...input,expiresAt:new Date(1000)},1000))
  assert.throws(()=>plaintextRequestPayload({...input,expiresAt:new Date(20001)},1000))
})

test('Manager and Develop compose the same request dialog without a second form',()=>{
  const manager=readFileSync(new URL('../../../manager/frontend/src/components/explorer/ItemPanel.vue',import.meta.url),'utf8')
  const develop=readFileSync(new URL('../../../develop/frontend/src/views/QueryEditor.vue',import.meta.url),'utf8')
  assert.match(manager,/<ProtectionAccessRequestDialog/);assert.match(develop,/<ProtectionAccessRequestDialog/)
  assert.doesNotMatch(manager,/const submitAccessRequest|const accessRequestForm/)
})
