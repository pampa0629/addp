import {expect,it} from 'vitest'
import {parseBusinessDeployment} from '../src/utils/businessDeployment'
import {resolveEngineDeploymentsRouteState,resolveHostNodesRouteState} from '../src/utils/routeState'
import {inventory} from '../e2e/engine-deployments.fixture'
it('previews only the bounded Business inventory and rejects extra credential or tenant fields',()=>{
 expect(parseBusinessDeployment(JSON.stringify(inventory))).toEqual(inventory)
 for(const value of [{...inventory,tenant_id:1},{...inventory,source:{...inventory.source,password:'x'}},{...inventory,services:[...inventory.services,...inventory.services]},{...inventory,observed_at:'2026-02-30T00:00:00Z'}, {...inventory,source:{...inventory.source,runtime:'macos'}}]) expect(()=>parseBusinessDeployment(JSON.stringify(value))).toThrow()
 expect(()=>parseBusinessDeployment(' '.repeat(16385))).toThrow()
})
it('retains only deployment pagination and search, independently of host filters',()=>{
 const state=resolveEngineDeploymentsRouteState({search:' pg ',page:'02',page_size:'50',node_kind:'virtual',enabled:'true',tenant_id:'1'})
 expect(state.query).toEqual({search:'pg',page:'2',page_size:'50'});expect(state.changed).toBe(true)
 expect(resolveEngineDeploymentsRouteState(state.query).changed).toBe(false)
 expect(resolveHostNodesRouteState({node_kind:'virtual',enabled:'true',page:'2'}).query).toEqual({node_kind:'virtual',enabled:'true',page:'2'})
})
