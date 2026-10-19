import {expect,it} from 'vitest'
import {validateDeploymentInventory,validateDeploymentDiscovery,resolveDeploymentSource} from '../src/utils/engineDeployment'
import {resolveEngineDeploymentsRouteState,resolveHostNodesRouteState} from '../src/utils/routeState'
import {inventory,discoveryFixture} from '../e2e/engine-deployments.fixture'
it('validates only the canonical declaration and rejects extra credential or tenant fields',()=>{
 expect(validateDeploymentInventory(inventory)).toEqual(inventory)
 for(const value of [{...inventory,tenant_id:1},{...inventory,source:{...inventory.source,password:'x'}},{...inventory,services:[...inventory.services,...inventory.services]},{...inventory,observed_at:'2026-02-30T00:00:00Z'}, {...inventory,source:{...inventory.source,runtime:'macos'}}]) expect(()=>validateDeploymentInventory(value)).toThrow()
 expect(()=>validateDeploymentInventory({...inventory,unexpected:'x'.repeat(16385)})).toThrow()
})
it('retains only deployment pagination and search, independently of host filters',()=>{
 const state=resolveEngineDeploymentsRouteState({search:' pg ',page:'02',page_size:'50',node_kind:'virtual',enabled:'true',tenant_id:'1'})
 expect(state.query).toEqual({search:'pg',page:'2',page_size:'50'});expect(state.changed).toBe(true)
 expect(resolveEngineDeploymentsRouteState(state.query).changed).toBe(false)
 expect(resolveHostNodesRouteState({node_kind:'virtual',enabled:'true',page:'2'}).query).toEqual({node_kind:'virtual',enabled:'true',page:'2'})
})

it('rejects incomplete discovery and duplicate candidates before selection',()=>{
 const value=discoveryFixture();expect(validateDeploymentDiscovery(value)).toBe(value)
 for(const change of [v=>{v.scopes=[]},v=>{v.candidates.push(v.candidates[0])},v=>{v.candidates[0].fingerprint='bad'},v=>{v.candidates[1].reason='unknown'},v=>{v.scopes[0].state='stale'}]){const invalid=structuredClone(value);change(invalid);expect(()=>validateDeploymentDiscovery(invalid)).toThrow()}
})

it('matches a deployment by physical source identity and shows current container facts without changing its declaration',()=>{
 const original=structuredClone(inventory),discovery=discoveryFixture(),now=Date.parse(discovery.scopes[1].observed_at)
 const live=discovery.scopes[1].services[0].containers[0];live.container_id='c'.repeat(64);live.started_at='2026-10-19T01:00:00Z';live.restart_count=3
 const result=resolveDeploymentSource(inventory,discovery,now)
 expect(result).toMatchObject({state:'running',changed:true,container:{container_id:'c'.repeat(64),restart_count:3}})
 expect(inventory).toEqual(original)
 live.status='exited';Object.assign(discovery.candidates.find(item=>item.scope==='business'&&item.service==='postgres'),{eligible:false,reason:'not_running'});expect(resolveDeploymentSource(inventory,discovery,now).state).toBe('exited')
})
it('never substitutes admission facts or another Docker engine when a current source cannot be matched',()=>{
 for(const [change,state] of [
  [v=>{v.scopes[1].state='stale';v.scopes[1].services=[];v.candidates=v.candidates.filter(c=>c.scope!=='business')},'stale'],
  [v=>{v.scopes[1].source.docker_engine_id='another-daemon'},'source_mismatch'],
  [v=>{v.scopes[1].source.runtime='linux_docker'},'source_mismatch'],
  [v=>{v.scopes[1].services=[];v.candidates=v.candidates.filter(c=>c.scope!=='business')},'not_found'],
  [v=>{v.scopes[1].observed_at='2000-01-01T00:00:00Z'},'stale'],
 ]){
  const value=discoveryFixture();change(value)
  expect(resolveDeploymentSource(inventory,value)).toMatchObject({state,container:null,changed:false})
 }
})
