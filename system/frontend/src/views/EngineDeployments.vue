<template>
  <section v-if="canRead" class="engine-deployments" data-testid="engine-deployments">
    <header class="toolbar"><h2>{{ t('system.engineDeployments.title') }}</h2>
      <el-button v-if="canCreate" type="primary" data-testid="deployment-create" @click="openCreate">{{ t('system.engineDeployments.create') }}</el-button>
    </header>
    <form class="toolbar" @submit.prevent="search">
      <el-input ref="searchInput" v-model="searchDraft" maxlength="255" clearable :aria-label="t('system.engineDeployments.search')" :placeholder="t('system.engineDeployments.search')" />
      <el-button native-type="submit">{{ t('common.search') }}</el-button>
      <el-button :loading="listLoading" @click="loadList">{{ t('system.module.refresh') }}</el-button>
    </form>
    <el-alert v-if="listError" :title="listError" type="error" :closable="false" />
    <el-table :data="rows" v-loading="listLoading" border stripe>
      <el-table-column prop="display_name" :label="t('system.engineDeployments.name')" min-width="180" show-overflow-tooltip />
      <el-table-column prop="engine_type" :label="t('system.engineDeployments.engine')" width="130" />
      <el-table-column :label="t('system.engineDeployments.state')" min-width="170"><template #default="{ row }">{{ t(`system.engineDeployments.states.${bindingState(row)}`) }}</template></el-table-column>
      <el-table-column :label="t('system.engineDeployments.observed')" min-width="190"><template #default="{ row }">{{ formatDateTime(row.inventory.observed_at) }}</template></el-table-column>
      <el-table-column :label="t('system.engineDeployments.detailAction')" width="110" fixed="right"><template #default="{ row }"><el-button text type="primary" @click="event => openDetail(row.deployment_id, event)">{{ t('system.engineDeployments.detailAction') }}</el-button></template></el-table-column>
      <template #empty><el-empty :description="t(listLoaded ? 'system.engineDeployments.empty' : 'system.hostNodes.notLoaded')" :image-size="72" /></template>
    </el-table>
    <el-pagination v-if="listLoaded" :current-page="state.page" :page-size="state.pageSize" :total="total" :page-sizes="[10,20,50,100]" layout="total, sizes, prev, pager, next" @current-change="page => setQuery({page:String(page)})" @size-change="size => setQuery({page:'1',page_size:String(size)})" />
    <el-dialog :model-value="dialogOpen" :title="t(creating ? 'system.engineDeployments.create' : 'system.engineDeployments.details')" class="addp-dialog" width="min(760px, calc(100vw - 24px))"
      :close-on-click-modal="!saving" :close-on-press-escape="!saving" :show-close="!saving" @update:model-value="value => !value && close()" @open-auto-focus="focusForm" @closed="restoreFocus">
      <div v-loading="detailLoading">
        <el-alert v-if="detailError || saveError" :title="detailError || saveError" type="error" :closable="false" data-testid="deployment-error" />
        <el-form v-if="formReady" label-position="top" :disabled="saving || !canWrite" @submit.prevent>
          <el-form-item :label="t('system.engineDeployments.name')"><el-input ref="nameInput" v-model="form.display_name" maxlength="255" data-testid="deployment-name" /></el-form-item>
          <el-form-item :label="t('system.engineDeployments.inventory')">
            <input ref="fileInput" type="file" accept="application/json,.json" hidden data-testid="deployment-file" :disabled="saving || !canWrite" @change="importFile" />
            <el-button :disabled="reading || blocked" @click="fileInput?.click()">{{ t('system.engineDeployments.import') }}</el-button>
            <p class="hint">{{ t('system.engineDeployments.fileHint') }}</p>
          </el-form-item>
          <template v-if="form.inventory">
            <el-alert :title="t('system.engineDeployments.snapshotHint')" type="info" :closable="false" />
            <dl data-testid="deployment-source">
              <dt>{{ t('system.engineDeployments.runtime') }}</dt><dd>{{ t(`system.engineDeployments.runtimes.${form.inventory.source.runtime}`) }}</dd>
              <dt>{{ t('system.engineDeployments.observed') }}</dt><dd>{{ formatDateTime(form.inventory.observed_at) }}</dd>
              <dt>{{ t('system.engineDeployments.docker') }}</dt><dd>{{ form.inventory.source.docker_engine_id }}</dd>
              <dt>{{ t('system.engineDeployments.container') }}</dt><dd>{{ container.container_id }}</dd>
              <dt>{{ t('system.engineDeployments.started') }}</dt><dd>{{ formatDateTime(container.started_at) }}</dd>
              <dt>{{ t('system.engineDeployments.ports') }}</dt><dd>{{ container.published_ports.map(port => `${port.address.includes(':') ? `[${port.address}]` : port.address}:${port.port} → ${port.container_port}/${port.protocol}`).join(', ') }}</dd>
            </dl>
          </template>
          <el-form-item :label="t('system.engineDeployments.host')">
            <HostNodeSelect v-if="canReadHosts" v-model="form.declared_node_id" :selected="selectedHost" :allowed="canReadHosts" :identity="identity" :disabled="blocked || saving" :api="pickerAPI"
              :label="t('system.engineDeployments.host')" :placeholder="t('system.hostAdmission.search')" :more-label="t('system.hostAdmission.more')" :error-message="t('system.hostAdmission.readFailed')" @selected="selectedHost = $event" />
            <el-input v-else :model-value="form.declared_node_id" readonly />
            <p class="hint">{{ t('system.engineDeployments.hostHint') }}</p>
          </el-form-item>
          <el-form-item :label="t('system.hostNodes.state')"><el-switch v-model="form.enabled" :aria-label="t('system.hostNodes.state')" :active-text="t('system.hostNodes.enabled')" :inactive-text="t('system.hostNodes.disabled')" data-testid="deployment-enabled" /></el-form-item>
          <p v-if="record" class="hint">{{ t('system.hostNodes.version', {version:record.version}) }} · {{ t(`system.engineDeployments.states.${bindingState(record)}`) }}</p>
        </el-form>
      </div>
      <template #footer>
        <el-button ref="closeButton" :disabled="saving" @click="close">{{ t('common.close') }}</el-button>
        <el-button v-if="record || deploymentID" :disabled="saving || detailLoading" @click="loadDetail">{{ t('system.engineDeployments.reload') }}</el-button>
        <el-button v-if="canWrite && formReady" type="primary" :disabled="reading || blocked || !form.inventory || !form.declared_node_id" :loading="saving" data-testid="deployment-save" @click="save">{{ t('system.engineDeployments.confirm') }}</el-button>
      </template>
    </el-dialog>
    <StatusAnnouncer :message="saving ? t('system.hostNodes.saving') : ''" />
  </section>
</template>
<script setup>
import { computed, nextTick, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { HostNodeSelect, StatusAnnouncer, useConsolePageDescriptor } from '@common-ui'
import { formatDateTime } from '../../../../common-frontend/basic/src/utils/formatters'
import { createLatestRequestScope } from '../../../../common-frontend/basic/src/utils/latestRequestScope.mjs'
import { useAuthStore } from '../store/auth'
import { hostNodesAPI } from '../api/hostNodes'
import { engineDeploymentsAPI } from '../api/engineDeployments'
import { isDeploymentUUID, parseBusinessDeployment } from '../utils/businessDeployment'
import { resolveEngineDeploymentsRouteState } from '../utils/routeState'
import { navigateSystemRoute } from '../utils/moduleNavigation'
const auth=useAuthStore(), route=useRoute(), router=useRouter(), {t}=useI18n()
const identity=computed(()=>JSON.stringify([auth.authContext?.principal,auth.authContext?.context,auth.authContext?.delegation,auth.permissions]))
const canRead=computed(()=>auth.contextType==='platform' && auth.authContext?.principal?.type==='user' && !auth.authContext?.delegation && auth.hasPermission('platform.engine_deployment.read'))
const canReadHosts=computed(()=>canRead.value && auth.hasPermission('platform.host_node.read'))
const canCreate=computed(()=>canReadHosts.value && auth.hasPermission('platform.engine_deployment.create'))
const canUpdate=computed(()=>canReadHosts.value && auth.hasPermission('platform.engine_deployment.update'))
const deploymentID=computed(()=>typeof route.params.deployment_id==='string'?route.params.deployment_id:'')
const state=computed(()=>resolveEngineDeploymentsRouteState(route.query))
const rows=ref([]),total=ref(0),listLoading=ref(false),listLoaded=ref(false),listError=ref(''),searchDraft=ref('')
const record=ref(null),creating=ref(false),formReady=ref(false),detailLoading=ref(false),detailError=ref(''),saveError=ref(''),saving=ref(false),reading=ref(false),blocked=ref(false)
const form=reactive({display_name:'',enabled:true,inventory:null,declared_node_id:''}),selectedHost=ref(null)
const fileInput=ref(null),nameInput=ref(null),searchInput=ref(null),closeButton=ref(null)
const dialogOpen=computed(()=>creating.value || Boolean(deploymentID.value)),canWrite=computed(()=>creating.value?canCreate.value:canUpdate.value)
const container=computed(()=>form.inventory?.services[0].containers[0])
const pickerAPI={nodes:hostNodesAPI.list,node:hostNodesAPI.get}
const listRequests=createLatestRequestScope(),detailRequests=createLatestRequestScope()
let epoch=0,trigger=null
const navigate=(path,query=state.value.query,history='push')=>navigateSystemRoute(router,{path,query},{history})
const bindingState=row=>row.enabled?row.host_binding_state==='bound'?'bound':row.host_binding_reason==='node_kind_mismatch'?'node_kind_mismatch':'node_disabled':'deployment_disabled'
useConsolePageDescriptor(router,'system',{title:computed(()=>t('system.engineDeployments.details')),subject:computed(()=>record.value?.display_name||''),ready:computed(()=>Boolean(record.value)&&!creating.value)})
function validRecord(value,id='') {
  if (!isDeploymentUUID(value?.deployment_id) || (id && value.deployment_id!==id) || !Number.isSafeInteger(value.version) || value.version<1 || typeof value.display_name!=='string' || typeof value.enabled!=='boolean' || !isDeploymentUUID(value.declared_node_id) || !['bound','rejected'].includes(value.host_binding_state)) throw new Error('invalid_deployment')
  parseBusinessDeployment(JSON.stringify(value.inventory));return value
}
function populate(value) {
  record.value=value;Object.assign(form,{display_name:value.display_name,enabled:value.enabled,inventory:value.inventory,declared_node_id:value.declared_node_id});selectedHost.value=null;formReady.value=true
}
async function loadList() {
  if(!canRead.value)return
  const ticket=listRequests.invalidate();rows.value=[];listLoaded.value=false;listError.value='';listLoading.value=true
  try {
    const result=await listRequests.request(config=>engineDeploymentsAPI.list({page:state.value.page,page_size:state.value.pageSize,...(state.value.search?{search:state.value.search}:{})},config))
    if(!listRequests.current(ticket))return
    if(!Array.isArray(result.data)||!Number.isSafeInteger(result.total)||result.total<0)throw new Error('invalid_page')
    result.data.forEach(value=>validRecord(value));rows.value=result.data;total.value=result.total;listLoaded.value=true
  }catch{if(listRequests.current(ticket))listError.value=t('system.engineDeployments.readFailed')}
  finally{if(listRequests.current(ticket))listLoading.value=false}
}
async function loadDetail() {
  if(!canRead.value||!deploymentID.value)return
  const id=deploymentID.value,ticket=detailRequests.invalidate();formReady.value=false;record.value=null;detailError.value='';saveError.value='';blocked.value=false;detailLoading.value=true
  try {
    if(!isDeploymentUUID(id))throw new Error('invalid_id')
    const value=validRecord(await detailRequests.request(config=>engineDeploymentsAPI.get(id,config)),id)
    if(detailRequests.current(ticket)){populate(value);await focusForm()}
  }catch{if(detailRequests.current(ticket))detailError.value=t('system.engineDeployments.readFailed')}
  finally{if(detailRequests.current(ticket))detailLoading.value=false}
}
async function importFile(event) {
  if(!canWrite.value||saving.value||blocked.value||reading.value)return
  const own=epoch,file=event.target.files?.[0];form.inventory=null;saveError.value='';reading.value=true
  try{if(!file||file.size>16384)throw new Error('invalid_file');const inventory=parseBusinessDeployment(await file.text());if(own===epoch)form.inventory=inventory}
  catch{if(own===epoch)saveError.value=t('system.engineDeployments.invalidFile')}
  finally{event.target.value='';if(own===epoch)reading.value=false}
}
function openCreate(event){
  if(!canCreate.value)return
  trigger=event?.currentTarget;detailRequests.invalidate();record.value=null;selectedHost.value=null;saveError.value='';detailError.value='';blocked.value=false;reading.value=false
  Object.assign(form,{display_name:'',enabled:true,inventory:null,declared_node_id:''});formReady.value=true;creating.value=true
}
function openDetail(id,event){trigger=event?.currentTarget;return navigate(`/engine-deployments/${id}`)}
function close(){if(saving.value)return;epoch++;creating.value=false;reading.value=false;if(deploymentID.value)return navigate('/engine-deployments',state.value.query,'replace')}
async function focusForm(event){event?.preventDefault?.();await nextTick();if(canWrite.value && formReady.value)nameInput.value?.focus();else closeButton.value?.$el?.focus()}
async function restoreFocus(){await nextTick();if(trigger?.isConnected)trigger.focus();else searchInput.value?.focus();trigger=null}
function setQuery(patch){return navigate('/engine-deployments',{...state.value.query,...patch},'replace')}
function search(){return setQuery({search:searchDraft.value.trim()||undefined,page:undefined})}
async function save(){
  if(!canWrite.value||saving.value||blocked.value||reading.value)return
  const name=form.display_name.trim()
  if(!name||[...name].length>255||/[\u0000-\u001f\u007f]/u.test(name)||!form.inventory||!isDeploymentUUID(form.declared_node_id)){saveError.value=t('system.engineDeployments.invalidForm');return}
  if(selectedHost.value && (selectedHost.value.node_id!==form.declared_node_id || (form.enabled && !selectedHost.value.enabled) || (form.inventory.source.runtime==='desktop_linux_vm' && selectedHost.value.node_kind!=='virtual'))){saveError.value=t('system.engineDeployments.hostMismatch');return}
  const own=epoch,isCreate=creating.value,id=deploymentID.value
  const body={display_name:name,enabled:form.enabled,inventory:form.inventory,declared_node_id:form.declared_node_id,...(!isCreate?{version:record.value.version}:{})}
  saving.value=true;saveError.value=''
  try{
    const value=validRecord(await (isCreate?engineDeploymentsAPI.create(body):engineDeploymentsAPI.update(id,body)),isCreate?'':id)
    if(own!==epoch)return
    ElMessage.success(t('system.engineDeployments.saved'))
    if(isCreate){creating.value=false;await navigate(`/engine-deployments/${value.deployment_id}`,state.value.query,'replace')}
    else{populate(value);await loadList()}
  }catch(error){
    if(own!==epoch)return
    const status=error.response?.status,code=error.response?.data?.error_code
    blocked.value=!status||status>=500||code==='resource_version_conflict'
    saveError.value=t(code==='engine_deployment_source_occupied'?'system.engineDeployments.occupied':code==='engine_deployment_host_unavailable'?'system.engineDeployments.hostMismatch':blocked.value?'system.engineDeployments.unknownWrite':'system.engineDeployments.saveFailed')
  }finally{if(own===epoch)saving.value=false}
}
watch([()=>route.fullPath,identity],async()=>{
  epoch++;listRequests.invalidate();detailRequests.invalidate();rows.value=[];record.value=null;selectedHost.value=null;creating.value=false;formReady.value=false;saving.value=false;reading.value=false;blocked.value=false;detailLoading.value=false;detailError.value='';saveError.value='';listError.value='';listLoading.value=false;listLoaded.value=false;Object.assign(form,{inventory:null,declared_node_id:'',display_name:'',enabled:true});searchDraft.value=state.value.search
  if(!canRead.value)return
  if(state.value.changed){await navigate(route.path,state.value.query,'replace');return}
  await Promise.all([loadList(),loadDetail()])
},{immediate:true})
onBeforeUnmount(()=>{epoch++;listRequests.dispose();detailRequests.dispose()})
</script>
<style scoped>
.engine-deployments{padding:20px;min-width:0}.toolbar{display:flex;align-items:center;gap:12px;margin-bottom:16px;flex-wrap:wrap}.toolbar h2{flex:1;margin:0}.toolbar .el-input{max-width:420px}.el-pagination{margin-top:16px;overflow-x:auto}.hint{color:var(--addp-text-secondary);margin:8px 0;width:100%}dl{display:grid;grid-template-columns:140px minmax(0,1fr);gap:10px}dd{margin:0;overflow-wrap:anywhere}dt{color:var(--addp-text-secondary)}@media(max-width:600px){.engine-deployments{padding:12px}.toolbar .el-input{max-width:none;flex-basis:100%}dl{grid-template-columns:1fr;gap:6px}dd{margin-bottom:8px}}
</style>
