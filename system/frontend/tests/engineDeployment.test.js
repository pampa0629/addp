import {expect,it} from 'vitest'
import {validateDeploymentInventory,validateDeploymentDiscovery} from '../src/utils/engineDeployment'
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
