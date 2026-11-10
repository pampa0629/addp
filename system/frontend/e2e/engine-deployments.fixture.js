import { HOST_NODE_ID, mockHostNodesAPI } from './host-nodes.fixture'
export const DEPLOYMENT_ID='20000000-0000-4000-8000-000000000001'
export const INFRA_DEPLOYMENT_ID='20000000-0000-4000-8000-000000000002'
export const inventory={schema:'addp.engine-deployment-inventory/v1',observed_at:'2026-10-19T00:00:00Z',source:{type:'docker_compose',runtime:'desktop_linux_vm',docker_engine_id:'fixture-daemon',project:'business'},services:[{service:'postgres',engine_type:'postgresql',containers:[{container_id:'a'.repeat(64),started_at:'2026-10-18T00:00:00.123456789Z',restart_count:0,published_ports:[{address:'::',port:5433,container_port:5432,protocol:'tcp'}]}]}]}
export const deploymentPermissions=['platform.engine_deployment.read','platform.engine_deployment.create','platform.engine_deployment.update','platform.host_node.read']
export function discoveryFixture(registered=false,redis=false,minio=false) {
 const scopes=['infra','business'].map(scope=>({schema:'addp.deployment-discovery/v2',scope,state:'available',observed_at:new Date().toISOString().replace(/\.\d{3}Z$/u,'Z'),source:{...inventory.source,project:scope==='infra'?'addp-infra':'business'},services:[{...inventory.services[0],containers:inventory.services[0].containers.map(container=>({...container,status:'running'}))},{service:minio?'minio':redis?'redis':'falkordb',engine_type:minio?'minio':redis?'redis':'falkordb',containers:(redis||minio)?inventory.services[0].containers.map(c=>({...c,container_id:(scope==='infra'?'d':'e').repeat(64),status:'running',published_ports:[{address:'127.0.0.1',port:16379,container_port:minio?9000:6379,protocol:'tcp'}]})):[]}]}))
 const candidates=scopes.flatMap(scope=>scope.services.map(service=>({scope:scope.scope,service:service.service,fingerprint:(scope.scope==='infra'?'b':'a').repeat(64),eligible:service.service==='postgres'||((redis&&service.service==='redis')||(minio&&service.service==='minio')),reason:service.service==='postgres'||((redis&&service.service==='redis')||(minio&&service.service==='minio'))?'':'unsupported_engine',...(registered&&scope.scope==='business'&&service.service==='postgres'?{deployment_id:DEPLOYMENT_ID}:{})})))
 return {scopes,candidates}
}
export async function mockDeployments(page,{permissions=deploymentPermissions,scope='platform',conflict=false,unknown=false,language='zh-cn',empty=false,sourceChanged=false,sourceUnavailable=false,redisSources=false,minioSources=false}={}){
 const hosts=await mockHostNodesAPI(page,{permissions,scope,language}),calls=[],writes=[]
 await page.route('**/api/v1/system/auth/context',route=>route.fulfill({json:{principal:{id:'1',type:'user'},context:{type:scope,...(scope==='tenant'?{tenant_id:'3',tenant_membership_id:'4'}:{})},authorization:{role_assignments:[{scope:{type:scope,...(scope==='tenant'?{tenant_id:'3'}:{})},permissions}]}}}))
 const record=(id=DEPLOYMENT_ID,source=inventory)=>({deployment_id:id,scope:source.source.project==='business'?'business':'infra',display_name:'Business PostgreSQL',engine_type:source.services[0].engine_type,enabled:true,inventory:source,declared_node_id:HOST_NODE_ID,node_id:HOST_NODE_ID,host_binding_state:'bound',host_binding_reason:'',version:7,created_at:'2026-10-19T00:00:00Z',updated_at:'2026-10-19T00:00:00Z'})
 let records=empty?[]:[record()]
 await page.route('**/api/v1/system/platform/engine_deployments**',async route=>{
  const request=route.request(),url=new URL(request.url());calls.push({method:request.method(),path:url.pathname,query:Object.fromEntries(url.searchParams)})
  if(request.method()==='POST'||request.method()==='PUT'){
   const body=request.postDataJSON();writes.push(body)
   if(unknown)return route.abort('failed')
   if(conflict||sourceChanged)return route.fulfill({status:409,json:{error_code:sourceChanged?'engine_deployment_source_changed':'resource_version_conflict'}})
   if(request.method()==='POST'){
    const data=body.items.map(item=>{
      const scope=discoveryFixture(false,redisSources,minioSources).scopes.find(s=>s.scope===item.scope)
      const service=scope.services.find(s=>s.service===item.service)
      const declaration={...inventory,source:scope.source,services:[{...service,containers:service.containers.map(({status,...c})=>c)}]}
      const id=(item.service==='redis'||item.service==='minio')?`20000000-0000-4000-8000-00000000000${item.scope==='infra'?'3':'4'}`:item.scope==='infra'?INFRA_DEPLOYMENT_ID:DEPLOYMENT_ID
      return {...record(id,declaration),display_name:item.display_name,declared_node_id:item.declared_node_id,enabled:item.enabled,version:1}
    })
    records.push(...data);return route.fulfill({status:201,json:{data}})
   }
   const index=records.findIndex(value=>url.pathname.endsWith('/'+value.deployment_id));records[index]={...records[index],...body,version:body.version+1};return route.fulfill({json:records[index]})
  }
  if(url.pathname.endsWith('/discovery')){
   const discovery=discoveryFixture(records.some(row=>row.deployment_id===DEPLOYMENT_ID),redisSources,minioSources)
   if(sourceUnavailable){discovery.scopes.forEach(item=>{item.state='unavailable';delete item.source;item.services=[]});discovery.candidates=[]}
   for(const item of discovery.candidates)if(item.scope==='infra'&&records.some(row=>row.deployment_id===INFRA_DEPLOYMENT_ID)&&item.service==='postgres')item.deployment_id=INFRA_DEPLOYMENT_ID
   return route.fulfill({json:discovery})
  }
  if(url.pathname.endsWith('/engine_deployments')){
   const selectedScope=url.searchParams.get('scope')
   const filtered=records.filter(row=>row.scope===selectedScope)
   return route.fulfill({json:{data:filtered,total:filtered.length,page:Number(url.searchParams.get('page')),page_size:Number(url.searchParams.get('page_size')),total_pages:filtered.length?1:0}})
  }
  return route.fulfill({json:records.find(value=>url.pathname.endsWith('/'+value.deployment_id))})
 })
 return {calls,writes,hosts}
}
