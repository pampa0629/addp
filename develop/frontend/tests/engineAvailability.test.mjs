import assert from 'node:assert/strict'
import {
  engineSelectionState,
  isEngineSelectable
} from '../../../common-frontend/basic/src/utils/engineAvailability.js'

const online = { lifecycle_state: 'active', connection_status: 'online' }
assert.equal(isEngineSelectable(online), true)
assert.equal(engineSelectionState(online), 'available')

for (const [connectionStatus, state] of [
  ['offline', 'offline'],
  ['unknown', 'unknown'],
  ['checking', 'checking']
]) {
  const engine = { lifecycle_state: 'active', connection_status: connectionStatus }
  assert.equal(isEngineSelectable(engine), false)
  assert.equal(engineSelectionState(engine), state)
}

assert.equal(isEngineSelectable({ lifecycle_state: 'disabled', connection_status: 'online' }), false)
assert.equal(engineSelectionState({ lifecycle_state: 'disabled', connection_status: 'online' }), 'disabled')
assert.equal(engineSelectionState(null), 'missing')



// Execute the real editor methods with controlled responses; no parallel state implementation.
const {readFile}=await import('node:fs/promises')
const vm=await import('node:vm')
const {test}=await import('node:test')
const source=await readFile(new URL('../src/views/QueryEditor.vue',import.meta.url),'utf8')
function visibilityHarness(list){
 const ref=value=>({value})
 const state={engineListRequestSequence:0,fieldRequestSequence:0,catalogFactsSequence:0,catalogRequestSequence:0,catalogEngineRequestSequence:0,plaintextTargetSequence:0,
  maskingRequestSequence:0,exportMaskingSequence:0,engines:ref([{id:11}]),selectedQueryTarget:ref('engine:11'),queryContent:ref('SELECT secret FROM saved_target'),targetLocator:ref('saved-locator'),
  relationDefaultPickerVisible:ref(true),activeRelationDefaultParameter:ref({name:'old'}),relationDefaultSelection:ref({identity:{locator:'old'}}),catalogVersion:ref(0),catalogSelection:ref({identity:{locator:'old'}}),initialCatalogLocator:ref('old'),catalogCompletions:ref([{label:'old_table'}]),fieldCompletions:ref([{label:'secret'}]),
  fieldCompletionCache:new Map([['old',['secret']]]),queryReferenceFacts:new Map([['old',{}]]),sampleRequests:{invalidate(){}},catalogSourceEngines:ref([{id:11}]),catalogEngineIds:ref([11]),catalogEnginesLoading:ref(false),loadingSampleQuery:ref(false),
  maskingDrawerVisible:ref(false),maskingFields:ref(['secret']),exportMaskingFields:ref(['secret']),plaintextTargetIdentity:ref('old'),plaintextRequestVisible:ref(true),
  listEngines:list,ElMessage:{error(){}},t:k=>k}
 state.queryTargets={get value(){return state.engines.value.map(e=>({value:`engine:${e.id}`,available:true}))}}
 const start=source.includes('const invalidateCatalogVisibility =')?source.indexOf('const invalidateCatalogVisibility ='):source.indexOf('const loadEngines = async')
 const methods=source.slice(start,source.indexOf('const handleQueryEngineDropdownVisible'))
 const load=vm.runInContext(`${methods}\nloadEngines`,vm.createContext(state));return {state,load}
}
test('lost source visibility removes catalog facts but preserves SQL and saved binding',async()=>{
 const {state,load}=visibilityHarness(async()=>[]);await load()
 assert.equal(state.relationDefaultPickerVisible.value,false);assert.equal(state.activeRelationDefaultParameter.value,null);assert.equal(state.relationDefaultSelection.value,null)
 assert.equal(state.catalogSelection.value,null);assert.equal(state.fieldCompletions.value.length,0);assert.equal(state.catalogCompletions.value.length,0)
 assert.equal(state.fieldCompletionCache.size,0);assert.equal(state.queryReferenceFacts.size,0)
 assert.equal(state.queryContent.value,'SELECT secret FROM saved_target');assert.equal(state.targetLocator.value,'saved-locator');assert.equal(state.selectedQueryTarget.value,'engine:11')
 assert.equal(state.engines.value.length,0)
})
test('a stale engine response cannot restore a source after a newer refusal',async()=>{
 const pending=[];const {state,load}=visibilityHarness(()=>new Promise(resolve=>pending.push(resolve)))
 const old=load(),current=load();pending[1]([]);await current;pending[0]([{id:11}]);await old
 assert.equal(state.engines.value.length,0);assert.equal(state.fieldCompletions.value.length,0)
})
test('dependency failure removes previous catalog disclosure without changing the draft',async()=>{
 const {state,load}=visibilityHarness(async()=>{throw new Error('unavailable')});await load()
 assert.equal(state.engines.value.length,0);assert.equal(state.catalogSelection.value,null);assert.equal(state.queryContent.value,'SELECT secret FROM saved_target')
})

test('an authorized offline saved target keeps metadata navigation, a filtered target does not', () => {
  const block = source.slice(source.indexOf('const catalogTreeEngineIds ='), source.indexOf('const catalogTreeEngineMultiple ='))
  const context = vm.createContext({
    computed: getter => ({get value() {return getter()}}),
    selectedRegisteredTarget: {value: {available: false}},
    selectedEngineId: {value: 11}, federatedQuery: {value: false}, catalogEngineIds: {value: []}
  })
  const navigation = vm.runInContext(`${block};\n({ids: catalogTreeEngineIds, selected: catalogTreeEngineValue})`, context)
  assert.deepEqual(Array.from(navigation.ids.value), [11])
  assert.equal(navigation.selected.value, 11)
  context.selectedRegisteredTarget.value = null
  assert.deepEqual(Array.from(navigation.ids.value), [])
  assert.equal(navigation.selected.value, null)
})
