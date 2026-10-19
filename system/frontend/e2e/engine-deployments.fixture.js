import { HOST_NODE_ID, mockHostNodesAPI } from './host-nodes.fixture'
export const DEPLOYMENT_ID='20000000-0000-4000-8000-000000000001'
export const inventory={schema:'addp.business-deployment-inventory/v1',observed_at:'2026-10-19T00:00:00Z',source:{type:'docker_compose',runtime:'desktop_linux_vm',docker_engine_id:'fixture-daemon',project:'business'},services:[{service:'postgres',engine_type:'postgresql',containers:[{container_id:'a'.repeat(64),started_at:'2026-10-18T00:00:00.123456789Z',restart_count:0,published_ports:[{address:'::',port:5433,container_port:5432,protocol:'tcp'}]}]}]}
export const deploymentPermissions=['platform.engine_deployment.read','platform.engine_deployment.create','platform.engine_deployment.update','platform.host_node.read']
export async function mockDeployments(page,{permissions=deploymentPermissions,scope='platform',conflict=false,unknown=false,language='zh-cn'}={}){
 const hosts=await mockHostNodesAPI(page,{permissions,scope,language}),calls=[],writes=[]
 await page.route('**/api/v1/system/auth/context',route=>route.fulfill({json:{principal:{id:'1',type:'user'},context:{type:scope,...(scope==='tenant'?{tenant_id:'3',tenant_membership_id:'4'}:{})},authorization:{role_assignments:[{scope:{type:scope,...(scope==='tenant'?{tenant_id:'3'}:{})},permissions}]}}}))
 let record={deployment_id:DEPLOYMENT_ID,display_name:'Business PostgreSQL',engine_type:'postgresql',enabled:true,inventory,declared_node_id:HOST_NODE_ID,node_id:HOST_NODE_ID,host_binding_state:'bound',host_binding_reason:'',version:7,created_at:'2026-10-19T00:00:00Z',updated_at:'2026-10-19T00:00:00Z'}
 await page.route('**/api/v1/system/platform/engine_deployments**',async route=>{
  const request=route.request(),url=new URL(request.url());calls.push({method:request.method(),path:url.pathname,query:Object.fromEntries(url.searchParams)})
  if(request.method()==='POST'||request.method()==='PUT'){
   const body=request.postDataJSON();writes.push(body)
   if(unknown)return route.abort('failed')
   if(conflict)return route.fulfill({status:409,json:{error_code:'resource_version_conflict'}})
   record={...record,...body,version:request.method()==='POST'?1:body.version+1};return route.fulfill({status:request.method()==='POST'?201:200,json:record})
  }
  if(url.pathname.endsWith('/engine_deployments'))return route.fulfill({json:{data:[record],total:1,page:Number(url.searchParams.get('page')),page_size:Number(url.searchParams.get('page_size')),total_pages:1}})
  return route.fulfill({json:record})
 })
 return {calls,writes,hosts}
}
export const sourceFile=source=>({name:'deployment.json',mimeType:'application/json',buffer:Buffer.from(JSON.stringify(source))})
