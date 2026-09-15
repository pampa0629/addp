import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { effect, reactive, stop } from 'vue'
import { createLatestRequestCoordinator } from '../../../common-frontend/basic/src/utils/latestRequest.js'
import { commitLatestComponentDescriptorState } from '../src/utils/dataApplicationRuntime.mjs'
import { applicationParameterOptions, assertApplicationOptionValues } from '../src/utils/applicationParameterOptions.mjs'
import { createNamedParameterDraft, buildComponentConfiguration } from '../src/utils/componentDraft.mjs'
const option = value => ({ value, labels: { 'zh-cn': value, en: value } })

test('runtime parameter controls react when asynchronous descriptors become available', async () => {
  const source = readFileSync(new URL('../src/components/DataApplicationCanvas.vue', import.meta.url), 'utf8')
  // Exercise the actual canvas loader without mounting unrelated chart/map renderers.
  const stateFactory = source.slice(source.indexOf('function createComponentState()'), source.indexOf('function runtimeDescriptors()'))
  const loader = source.slice(source.indexOf('async function loadDescriptor(item)'), source.indexOf('async function queryComponent('))
  const componentStates = reactive({})
  const { snapshot, descriptors } = fixture()
  const resolvers = []
  const loadDescriptor = new Function('componentStates', 'createLatestRequestCoordinator', 'commitLatestComponentDescriptorState', 'getConsumerDescriptor', 't', `${stateFactory}\n${loader}\nreturn loadDescriptor`)(
    componentStates, createLatestRequestCoordinator, commitLatestComponentDescriptorState,
    () => new Promise(resolve => resolvers.push(resolve)), key => key,
  )
  let renderedDomain
  const render = effect(() => {
    const currentDescriptors = Object.fromEntries(Object.entries(componentStates).map(([id, state]) => [id, state.descriptor]))
    renderedDomain = applicationParameterOptions(snapshot, currentDescriptors, 'shared')
  })
  try {
    const pending = snapshot.components.map(component => loadDescriptor(component))
    assert.equal(renderedDomain.ready, false)
    resolvers[0]({ data: descriptors.a })
    await pending[0]
    assert.equal(renderedDomain.ready, false, 'all shared targets must be ready')
    resolvers[1]({ data: descriptors.b })
    await pending[1]
    assert.equal(renderedDomain.ready, true, 'controls must enable without an unrelated render or user action')
    assert.deepEqual(renderedDomain.options, [option('total'), option('month')])
  } finally {
    stop(render)
  }
})
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
