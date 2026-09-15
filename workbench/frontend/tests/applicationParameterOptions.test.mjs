import test from 'node:test'
import assert from 'node:assert/strict'
import { applicationParameterOptions, assertApplicationOptionValues } from '../src/utils/applicationParameterOptions.mjs'
import { createNamedParameterDraft, buildComponentConfiguration } from '../src/utils/componentDraft.mjs'
const option = value => ({ value, labels: { 'zh-cn': value, en: value } })
function fixture() {
  const snapshot = { components: ['a', 'b'].map(id => ({id, title:id, contract_fingerprint:'same', query_template:{ named_parameter_bindings:[{name:'mode', parameter_key:'local'}] }})), parameter_bindings: ['a','b'].map(id=>({component_id:id, component_parameter_key:'local', application_parameter_key:'shared'})) }
  const descriptors = Object.fromEntries(['a','b'].map(id=>[id,{contract_fingerprint:'same', input_contract:{named_parameters:[{name:'mode',type:'string',options:[option('total'),option('month')]}]}}]))
  return {snapshot,descriptors}
}
test('one application parameter derives the common service options without copying them',()=>{
 const {snapshot,descriptors}=fixture()
 descriptors.b.input_contract.named_parameters[0].options=[option('month')]
 assert.deepEqual(applicationParameterOptions(snapshot,descriptors,'shared').options,[option('month')])
 assert.doesNotThrow(()=>assertApplicationOptionValues(snapshot,descriptors,{shared:'month'}))
 assert.throws(()=>assertApplicationOptionValues(snapshot,descriptors,{shared:'total'}))
 assert.throws(()=>assertApplicationOptionValues(snapshot,descriptors,{shared:'按月'}))
 const draft=createNamedParameterDraft(descriptors.a.input_contract.named_parameters[0])
 assert.deepEqual(draft.options,[option('total'),option('month')])
 const persisted=buildComponentConfiguration({ref:{}, contract_fingerprint:'same',input_contract:{}},{name:'Example',description:'',columns:['value'],parameters:[{...draft,value:'month'}],rendererType:'table',pageLimit:1},'a')
 assert.equal('options' in persisted.parameter_definitions[0],false)
})
test('missing descriptors, changed fingerprints, empty domains and labels conflicts block inputs',()=>{
 for(const mutate of [d=>delete d.b,d=>{d.b.contract_fingerprint='changed'},d=>{d.b.input_contract.named_parameters[0].options=[option('week')]},d=>{d.b.input_contract.named_parameters[0].options[0].labels.en='different'}]) {
  const {snapshot,descriptors}=fixture();mutate(descriptors)
  assert.equal(applicationParameterOptions(snapshot,descriptors,'shared').ready,false)
  assert.throws(()=>assertApplicationOptionValues(snapshot,descriptors,{shared:'total'}))
 }
})
