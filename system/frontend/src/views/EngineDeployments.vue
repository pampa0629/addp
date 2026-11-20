<template>
  <section v-if="canRead" class="engine-deployments" data-testid="engine-deployments">
    <header class="toolbar"><h2>{{ t('system.engineDeployments.title') }}</h2>
      <el-button link type="primary" @click="openCollection">{{ t('system.engineDeployments.manageCollection') }}</el-button>
      <el-button v-if="canCreate" type="primary" data-testid="deployment-create" @click="openConfiguration">{{ t('system.engineDeployments.create') }}</el-button>
    </header>
    <p class="hint">{{ t('system.engineDeployments.purpose') }}</p>
    <el-tabs :model-value="state.scope" @update:model-value="changeScope">
      <el-tab-pane name="infra" :label="t('system.engineDeployments.scopes.infra')" />
      <el-tab-pane name="business" :label="t('system.engineDeployments.scopes.business')" />
    </el-tabs>
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
      <el-table-column :label="t('system.engineDeployments.actions')" :width="canObserve ? 210 : 110" fixed="right">
        <template #default="{ row }">
          <el-button v-if="canObserve" link type="primary" @click="openMonitoring(row.deployment_id)">{{ t('system.engineDeployments.monitor') }}</el-button>
          <el-button link type="primary" :data-testid="`deployment-detail-${row.deployment_id}`" @click="event => openDetail(row.deployment_id, event)">{{ t('system.engineDeployments.detailAction') }}</el-button>
        </template>
      </el-table-column>
      <template #empty><el-empty :description="t(listLoaded ? 'system.engineDeployments.empty' : 'system.hostNodes.notLoaded')" :image-size="72" /></template>
    </el-table>
    <el-pagination v-if="listLoaded" :current-page="state.page" :page-size="state.pageSize" :total="total" :page-sizes="[10,20,50,100]" layout="total, sizes, prev, pager, next" @current-change="page => setQuery({page:String(page)})" @size-change="size => setQuery({page:'1',page_size:String(size)})" />
    <el-dialog :model-value="dialogOpen" :title="t(configuring ? 'system.engineDeployments.create' : 'system.engineDeployments.details')" class="addp-dialog" width="min(760px, calc(100vw - 24px))"
      :close-on-click-modal="!saving" :close-on-press-escape="!saving" :show-close="!saving" @update:model-value="value => !value && close()" @open-auto-focus="focusForm" @closed="restoreFocus">
      <div v-loading="detailLoading">
        <el-alert v-if="detailError || saveError" :title="detailError || saveError" type="error" :closable="false" data-testid="deployment-error" />
        <section v-if="record && !configuring" class="current-source" data-testid="deployment-current-source" :aria-busy="sourceLoading">
          <div class="toolbar"><h3>{{ t('system.engineDeployments.currentSource') }}</h3>
            <el-button :loading="sourceLoading" @click="loadCurrentSource">{{ t('system.engineDeployments.refreshDiscovery') }}</el-button>
          </div>
          <p role="status">{{ t(`system.engineDeployments.sourceStates.${sourceLoading ? 'loading' : sourceError ? 'read_failed' : currentSource?.state || 'read_failed'}`) }}</p>
          <template v-if="!sourceLoading && !sourceError && currentSource">
            <p v-if="currentSource.observedAt" class="hint">{{ t('system.engineDeployments.observed') }}: {{ formatDateTime(currentSource.observedAt) }}</p>
            <dl v-if="currentSource.container" data-testid="deployment-current-container">
              <dt>{{ t('system.engineDeployments.container') }}</dt><dd>{{ currentSource.container.container_id }}</dd>
              <dt>{{ t('system.engineDeployments.started') }}</dt><dd>{{ currentSource.container.started_at ? formatDateTime(currentSource.container.started_at) : '—' }}</dd>
              <dt>{{ t('system.engineDeployments.restarts') }}</dt><dd>{{ currentSource.container.restart_count }}</dd>
            </dl>
            <p v-if="currentSource.changed" class="hint">{{ t('system.engineDeployments.runtimeChanged') }}</p>
          </template>
          <p class="hint">{{ t('system.engineDeployments.currentSourceHint') }}</p>
        </section>
        <el-form v-if="formReady" label-position="top" :disabled="saving || !canWrite" @submit.prevent>
          <template v-if="configuring">
            <div class="toolbar"><el-button :loading="reading" :disabled="saving" @click="loadDiscovery">{{ t('system.engineDeployments.reloadConfiguration') }}</el-button></div>
            <el-tabs v-model="discoveryScope" data-testid="deployment-discovery-scopes">
              <el-tab-pane name="infra" :label="t('system.engineDeployments.scopes.infra')" />
              <el-tab-pane name="business" :label="t('system.engineDeployments.scopes.business')" />
            </el-tabs>
            <el-alert v-if="discoveryError || activeDiscoveryScope" :title="t(`system.engineDeployments.discoveryStates.${discoveryError ? 'unavailable' : activeDiscoveryScope.state}`)" :type="activeDiscoveryScope?.state === 'available' ? 'info' : 'warning'" :closable="false" />
            <el-table :data="visibleCandidates" row-key="key" border max-height="320" data-testid="deployment-candidates" :row-class-name="({row}) => !row.deployment && !row.eligible ? 'candidate-unavailable' : ''">
              <el-table-column width="52" align="center">
                <template #default="{row}">
                  <el-icon v-if="!row.deployment && !row.eligible" class="candidate-unavailable-icon" role="img" :aria-label="t(`system.engineDeployments.candidateStates.${row.reason}`)" data-testid="candidate-unavailable"><CircleCloseFilled /></el-icon>
                  <el-checkbox v-else :model-value="draftEnabled[row.key] ?? row.deployment?.enabled ?? false"
                    :disabled="reading || blocked || Boolean(row.deployment) || !canCreate"
                    :aria-label="row.deployment?.display_name ?? `${row.scope} ${row.service}`" @change="checked => setMonitoring(row, checked)" />
                </template>
              </el-table-column>
              <el-table-column :label="t('system.engineDeployments.service')" min-width="106">
                <template #default="{row}">
                  <div>{{ row.deployment?.display_name ?? `${row.scope === 'infra' ? 'Infra' : 'Business'} / ${row.service}` }}</div>
                  <el-input v-if="!row.deployment && row.eligible && draftEnabled[row.key]" v-model="row.display_name" :disabled="blocked" class="candidate-name" maxlength="255" :aria-label="`${row.scope} ${row.service}`" />
                </template>
              </el-table-column>
              <el-table-column :label="t('system.engineDeployments.discoveryStatus')" min-width="84">
                <template #default="{row}">
                  <el-tag class="candidate-status" :type="row.deployment?.enabled ? 'success' : !row.deployment && row.eligible ? 'primary' : 'info'">{{ t(`system.engineDeployments.${row.deployment ? (row.deployment.enabled ? 'monitoringOn' : 'monitoringOff') : `candidateStates.${row.eligible ? 'ready' : row.reason}`}`) }}</el-tag>
                </template>
              </el-table-column>
              <el-table-column v-if="canObserve" :label="t('system.engineDeployments.collectionStatus')" min-width="93">
                <template #default="{row}">
                  <template v-if="row.deployment">
                    <span :data-testid="`collection-${row.deployment.deployment_id}`">{{ t(`system.engineDeployments.collection.${collections[row.key]?.state ?? (row.deployment.enabled && row.deployment.host_binding_state === 'bound' ? 'unknown' : 'not_active')}`) }}</span>
                    <div v-if="collections[row.key]?.sampled_at" class="collection-time">{{ formatDateTime(collections[row.key].sampled_at) }}</div>
                  </template>
                  <span v-else>—</span>
                </template>
              </el-table-column>
              <template #empty><el-empty :description="t(reading ? 'system.hostNodes.notLoaded' : 'system.engineDeployments.noCandidates')" :image-size="56" /></template>
            </el-table>
            <p class="hint">{{ t('system.engineDeployments.discoveryHint') }}</p>
            <div v-if="hasChanges" role="status" class="change-preview" data-testid="monitoring-changes">
              <p v-if="enableNames.length">{{ t('system.engineDeployments.enableChanges', { names: enableNames.join(', ') }) }}</p>
            </div>
            <el-alert v-if="configurationNotice" :title="t(`system.engineDeployments.${configurationNotice}`)" type="info" :closable="false" role="status" data-testid="admission-save-guidance" />
          </template>
          <el-form-item v-else :label="t('system.engineDeployments.name')"><el-input ref="nameInput" v-model="form.display_name" maxlength="255" data-testid="deployment-name" /></el-form-item>
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
          <template v-if="!configuring || selectedCandidates.length">
          <el-form-item :label="t('system.engineDeployments.host')">
            <HostNodeSelect v-if="canReadHosts" v-model="form.declared_node_id" :selected="selectedHost" :allowed="canReadHosts" :identity="identity" :disabled="blocked || saving" :api="pickerAPI"
              :label="t('system.engineDeployments.host')" :placeholder="t('system.hostAdmission.search')" :more-label="t('system.hostAdmission.more')" :error-message="t('system.hostAdmission.readFailed')" @selected="selectedHost = $event" />
            <el-input v-else :model-value="form.declared_node_id" readonly />
            <p class="hint">{{ t('system.engineDeployments.hostHint') }}</p>
          </el-form-item>
          </template>
          <p v-if="record" class="hint" data-testid="deployment-monitoring-state">{{ t(record.enabled ? 'system.engineDeployments.monitoringOn' : 'system.engineDeployments.monitoringOff') }}</p>
          <p v-if="record" class="hint">{{ t('system.hostNodes.version', {version:record.version}) }} · {{ t(`system.engineDeployments.states.${bindingState(record)}`) }}</p>
        </el-form>
      </div>
      <template #footer>
        <el-button ref="closeButton" :disabled="saving" @click="close">{{ t('common.close') }}</el-button>
        <el-button v-if="record || deploymentID" :disabled="saving || detailLoading" @click="loadDetail">{{ t('system.engineDeployments.reload') }}</el-button>
        <el-button v-if="canWrite && formReady" type="primary" :disabled="reading || blocked || (configuring ? !hasChanges || (selectedCandidates.length > 0 && !form.declared_node_id) : !form.inventory || !form.declared_node_id)" :loading="saving" data-testid="deployment-save" @click="save">{{ t('system.engineDeployments.confirm') }}</el-button>
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
import { CircleCloseFilled } from '@element-plus/icons-vue'
import { HostNodeSelect, StatusAnnouncer, useConsolePageDescriptor, openConsoleRoute, buildMonitorEngineResourcesRoute } from '@common-ui'
import { formatDateTime } from '../../../../common-frontend/basic/src/utils/formatters'
import { validateEngineCollection } from '../../../../common-frontend/basic/src/utils/engineCollection.mjs'
import { createLatestRequestScope } from '../../../../common-frontend/basic/src/utils/latestRequestScope.mjs'
import { useAuthStore } from '../store/auth'
import { hostNodesAPI } from '../api/hostNodes'
import { engineDeploymentsAPI } from '../api/engineDeployments'
import { isDeploymentUUID, validateDeploymentInventory, validateDeploymentDiscovery, resolveDeploymentSource } from '../utils/engineDeployment'
import { resolveEngineDeploymentsRouteState } from '../utils/routeState'
import { navigateSystemRoute } from '../utils/moduleNavigation'
const auth=useAuthStore(), route=useRoute(), router=useRouter(), {t}=useI18n()
const identity=computed(()=>JSON.stringify([auth.authContext?.principal,auth.authContext?.context,auth.authContext?.delegation,auth.permissions]))
const canRead=computed(()=>auth.contextType==='platform' && auth.authContext?.principal?.type==='user' && !auth.authContext?.delegation && auth.hasPermission('platform.engine_deployment.read'))
const canReadHosts=computed(()=>canRead.value && auth.hasPermission('platform.host_node.read'))
const canObserve=computed(()=>canRead.value && auth.hasPermission('monitor.resource_observation.read'))
const openMonitoring=id=>openConsoleRoute(buildMonitorEngineResourcesRoute({deployment_id:id}))
const canCreate=computed(()=>canReadHosts.value && auth.hasPermission('platform.engine_deployment.create'))
const canUpdate=computed(()=>canReadHosts.value && auth.hasPermission('platform.engine_deployment.update'))
const deploymentID=computed(()=>typeof route.params.deployment_id==='string'?route.params.deployment_id:'')
const state=computed(()=>resolveEngineDeploymentsRouteState(route.query))
const rows=ref([]),total=ref(0),listLoading=ref(false),listLoaded=ref(false),listError=ref(''),searchDraft=ref('')
const record=ref(null),configuring=ref(false),formReady=ref(false),detailLoading=ref(false),detailError=ref(''),saveError=ref(''),saving=ref(false),reading=ref(false),blocked=ref(false)
const form=reactive({display_name:'',inventory:null,declared_node_id:''}),selectedHost=ref(null)
const nameInput=ref(null),searchInput=ref(null),closeButton=ref(null)
const dialogOpen=computed(()=>configuring.value || Boolean(deploymentID.value)),canWrite=computed(()=>configuring.value ? canCreate.value : canUpdate.value)
const container=computed(()=>form.inventory?.services[0].containers[0])
const pickerAPI={nodes:hostNodesAPI.list,node:hostNodesAPI.get}
const listRequests=createLatestRequestScope(),detailRequests=createLatestRequestScope(),discoveryRequests=createLatestRequestScope()
const sourceRequests=createLatestRequestScope(),currentSource=ref(null),sourceLoading=ref(false),sourceError=ref(false)
const candidates = ref([])
const registered = ref([])
const draftEnabled = ref({})
const collections = ref({})
const discoveryError = ref(false)
const discoveryScopes = ref([])
const discoveryScope = ref('infra')
const activeDiscoveryScope = computed(() => discoveryScopes.value.find(item => item.scope === discoveryScope.value))
const configurationRows = computed(() => [
  ...registered.value.map(deployment => ({ key: deployment.deployment_id, scope: deployment.scope, deployment })),
  ...candidates.value.filter(item => !item.deployment_id)
])
const visibleCandidates = computed(() => configurationRows.value.filter(item => item.scope === discoveryScope.value))
const selectedCandidates = computed(() => candidates.value.filter(item => !item.deployment_id && draftEnabled.value[item.key]))
const hasChanges = computed(() => selectedCandidates.value.length > 0)
const enableNames = computed(() => selectedCandidates.value.map(row => row.display_name))
const configurationNotice = computed(() => {
  if (!configuring.value || reading.value || blocked.value || saveError.value) return ''
  if (!hasChanges.value) return 'noChanges'
  return selectedCandidates.value.length && !form.declared_node_id ? 'selectAdmissionHost' : ''
})
function setMonitoring(row, checked) {
  if (row.deployment || !canCreate.value) return
  if (checked) draftEnabled.value[row.key] = true
  else delete draftEnabled.value[row.key]
}

let epoch=0,trigger=null,sourceExpiry=null
const navigate=(path,query=state.value.query,history='push')=>navigateSystemRoute(router,{path,query},{history})
const openCollection = () => openConsoleRoute(state.value.scope === 'business' ? '/monitor/monitoring-targets?tab=business' : '/monitor/monitoring-targets?tab=infra')
const bindingState=row=>row.enabled?row.host_binding_state==='bound'?'bound':row.host_binding_reason==='node_kind_mismatch'?'node_kind_mismatch':'node_disabled':'deployment_disabled'
useConsolePageDescriptor(router,'system',{title:computed(()=>t('system.engineDeployments.details')),subject:computed(()=>record.value?.display_name||''),ready:computed(()=>Boolean(record.value)&&!configuring.value)})
function validRecord(value,id='') {
  if (!isDeploymentUUID(value?.deployment_id) || (id && value.deployment_id!==id) || !Number.isSafeInteger(value.version) || value.version<1 || typeof value.display_name!=='string' || typeof value.enabled!=='boolean' || !isDeploymentUUID(value.declared_node_id) || !['bound','rejected'].includes(value.host_binding_state)) throw new Error('invalid_deployment')
  validateDeploymentInventory(value.inventory);return value
}
async function loadCurrentSource() {
  if(!canRead.value || !record.value || configuring.value)return
  clearTimeout(sourceExpiry)
  const id=record.value.deployment_id,inventory=record.value.inventory,ticket=sourceRequests.invalidate()
  currentSource.value=null;sourceError.value=false;sourceLoading.value=true
  try {
    const discovery=await sourceRequests.request(config=>engineDeploymentsAPI.discover(config))
    if(sourceRequests.current(ticket) && record.value?.deployment_id===id){
      currentSource.value=resolveDeploymentSource(inventory,discovery)
      const at=Date.parse(currentSource.value.observedAt)
      if(!['unavailable','missing','unconfigured','stale'].includes(currentSource.value.state) && Number.isFinite(at) && at+120000>Date.now())sourceExpiry=setTimeout(()=>{
        if(sourceRequests.current(ticket))currentSource.value={state:'stale',observedAt:currentSource.value.observedAt,container:null,changed:false}
      },at+120001-Date.now())
    }
  }catch{if(sourceRequests.current(ticket))sourceError.value=true}
  finally{if(sourceRequests.current(ticket))sourceLoading.value=false}
}
function clearCurrentSource(){clearTimeout(sourceExpiry);sourceRequests.invalidate();currentSource.value=null;sourceLoading.value=false;sourceError.value=false}
function populate(value) {
  record.value=value;Object.assign(form,{display_name:value.display_name,inventory:value.inventory,declared_node_id:value.declared_node_id});selectedHost.value=null;formReady.value=true
}
async function loadList() {
  if(!canRead.value)return
  const ticket=listRequests.invalidate();rows.value=[];listLoaded.value=false;listError.value='';listLoading.value=true
  try {
    const {scope,page,pageSize,search}=state.value
    const result=await listRequests.request(config=>engineDeploymentsAPI.list({scope,page,page_size:pageSize,...(search?{search}:{})},config))
    if(!listRequests.current(ticket))return
    if(!Array.isArray(result.data)||!Number.isSafeInteger(result.total)||result.total<0)throw new Error('invalid_page')
    if(result.data.some(value=>validRecord(value).scope!==scope))throw new Error('invalid_deployment_scope')
    rows.value=result.data;total.value=result.total;listLoaded.value=true
  }catch{if(listRequests.current(ticket))listError.value=t('system.engineDeployments.readFailed')}
  finally{if(listRequests.current(ticket))listLoading.value=false}
}
async function loadDetail() {
  if(!canRead.value||!deploymentID.value)return
  clearCurrentSource()
  const id=deploymentID.value,ticket=detailRequests.invalidate();formReady.value=false;record.value=null;detailError.value='';saveError.value='';blocked.value=false;detailLoading.value=true
  try {
    if(!isDeploymentUUID(id))throw new Error('invalid_id')
    const value=validRecord(await detailRequests.request(config=>engineDeploymentsAPI.get(id,config)),id)
    if(detailRequests.current(ticket)){populate(value);loadCurrentSource();await focusForm()}
  }catch{if(detailRequests.current(ticket))detailError.value=t('system.engineDeployments.readFailed')}
  finally{if(detailRequests.current(ticket))detailLoading.value=false}
}
async function readRegistered(ticket) {
  const size = 100
  const records = new Map()
  let total
  for (let page = 1; discoveryRequests.current(ticket); page++) {
    const result = await discoveryRequests.request(config => engineDeploymentsAPI.list({ page, page_size: size }, config))
    total ??= result.total
    if (!Number.isSafeInteger(total) || total < 0 || result.total !== total || result.page !== page ||
        result.page_size !== size || !Array.isArray(result.data) || result.data.length !== Math.min(size, total - records.size)) {
      throw new Error('invalid_deployment_page')
    }
    for (const row of result.data) {
      validRecord(row)
      if (!['infra', 'business'].includes(row.scope) || records.has(row.deployment_id)) throw new Error('invalid_deployment')
      records.set(row.deployment_id, row)
    }
    if (records.size === total) return [...records.values()]
  }
}
async function readCollections(ticket, records) {
  const owners = records.filter(row => row.enabled && row.host_binding_state === 'bound')
  if (!canObserve.value || !owners.length || owners.length > 100) return
  try {
    const result = await discoveryRequests.request(config => engineDeploymentsAPI.summaries(owners.map(row => row.deployment_id), config))
    if (!discoveryRequests.current(ticket) || !canObserve.value) return
    if (!Array.isArray(result?.data) || result.data.length !== owners.length) return
    const byID = new Map(result.data.map(row => [row?.subject?.deployment_id, row]))
    const queried = Date.parse(result.queried_at)
    collections.value = Object.fromEntries(owners.map(owner => [owner.deployment_id, validateEngineCollection(byID.get(owner.deployment_id), owner, queried)]))
  } catch {
    // The monitoring read is independent of permissions and readiness for configuration writes.
  }
}
function resetConfiguration() {
  registered.value = []; candidates.value = []; draftEnabled.value = {}; collections.value = {}
  discoveryScopes.value = []; discoveryError.value = false
}
async function loadDiscovery() {
  if (!canCreate.value || saving.value) return
  const ticket = discoveryRequests.invalidate()
  resetConfiguration()
  form.declared_node_id = ''
  selectedHost.value = null
  reading.value = true; saveError.value = ''; blocked.value = true
  const [owners, discovery] = await Promise.allSettled([
    readRegistered(ticket),
    discoveryRequests.request(config => engineDeploymentsAPI.discover(config)).then(validateDeploymentDiscovery)
  ])
  if (!discoveryRequests.current(ticket)) return
  reading.value = false
  discoveryError.value = discovery.status === 'rejected'
  if (owners.status === 'rejected') {
    saveError.value = t('system.engineDeployments.readFailed')
    return
  }
  registered.value = owners.value
  if (discovery.status === 'fulfilled') {
    discoveryScopes.value = discovery.value.scopes
    candidates.value = discovery.value.candidates.map(item => ({
      ...item, key: `${item.scope}/${item.service}`,
      display_name: `${item.scope === 'infra' ? 'Infra' : 'Business'} ${({ postgres: 'PostgreSQL', redis: 'Redis', minio: 'MinIO', 'falkordb': 'FalkorDB', 'mysql': 'MySQL', 'neo4j': 'Neo4j', 'ftp': 'FTP', 'spark-master': 'Spark' })[item.service] || item.service}`
    }))
  }
  blocked.value = false
  void readCollections(ticket, owners.value)
}
function openConfiguration(event) {
  if (!canCreate.value) return
  discoveryScope.value = state.value.scope
  trigger = event?.currentTarget
  detailRequests.invalidate(); clearCurrentSource(); record.value = null; selectedHost.value = null; detailError.value = ''
  Object.assign(form, { display_name: '', inventory: null, declared_node_id: '' })
  formReady.value = true; configuring.value = true
  void loadDiscovery()
}
function openDetail(id,event){trigger=event?.currentTarget;return navigate(`/engine-deployments/${id}`)}
function closeConfiguration() {
  discoveryRequests.invalidate(); resetConfiguration(); configuring.value = false; reading.value = false
}
function close(){if(saving.value)return;clearCurrentSource();epoch++;closeConfiguration();if(deploymentID.value)return navigate('/engine-deployments',state.value.query,'replace')}
async function focusForm(event){event?.preventDefault?.();await nextTick();if(canWrite.value && formReady.value && !configuring.value)nameInput.value?.focus();else closeButton.value?.$el?.focus()}
async function restoreFocus(){await nextTick();if(trigger?.isConnected)trigger.focus();else searchInput.value?.focus();trigger=null}
function setQuery(patch){return navigate('/engine-deployments',{...state.value.query,...patch},'replace')}
function changeScope(scope){return setQuery({scope:scope==='business'?'business':undefined,page:undefined})}
function search(){return setQuery({search:searchDraft.value.trim()||undefined,page:undefined})}
async function save() {
  if (!canWrite.value || saving.value || blocked.value || reading.value) return
  const configuration = configuring.value
  const additions = configuration && canCreate.value ? selectedCandidates.value : []
  const name = form.display_name.trim()
  const validName = value => typeof value === 'string' && value.trim() && [...value.trim()].length <= 255 && !/\p{Cc}/u.test(value)
  if (configuration ? !additions.length || additions.length > 16 || additions.some(item => !validName(item.display_name))
    : !validName(name) || !record.value) {
    saveError.value = t('system.engineDeployments.invalidForm')
    return
  }
  if (!configuration || additions.length) {
    if (!isDeploymentUUID(form.declared_node_id)) {
      saveError.value = t('system.engineDeployments.selectAdmissionHost')
      return
    }
    const desktop = configuration
      ? additions.some(item => discoveryScopes.value.find(scope => scope.scope === item.scope)?.source?.runtime === 'desktop_linux_vm')
      : form.inventory.source.runtime === 'desktop_linux_vm'
    if (selectedHost.value && (selectedHost.value.node_id !== form.declared_node_id ||
        ((configuration || record.value.enabled) && !selectedHost.value.enabled) || (desktop && selectedHost.value.node_kind !== 'virtual'))) {
      saveError.value = t('system.engineDeployments.hostMismatch')
      return
    }
  }
  const own = epoch
  if (configuration) discoveryRequests.invalidate()
  saving.value = true; saveError.value = ''
  try {
    if (configuration) {
      if (additions.length) {
        const items = additions.map(item => ({ scope: item.scope, service: item.service, fingerprint: item.fingerprint,
          display_name: item.display_name.trim(), enabled: true, declared_node_id: form.declared_node_id }))
        const result = await engineDeploymentsAPI.create({ items })
        if (own !== epoch) return
        if (!Array.isArray(result.data) || result.data.length !== items.length) throw new Error('invalid_admission')
        const created = result.data.map(value => validRecord(value))
        const keys = new Set(created.map(row => `${row.scope}/${row.inventory.services[0].service}`))
        if (!items.every(item => keys.has(`${item.scope}/${item.service}`)) || created.some(row => !row.enabled)) throw new Error('invalid_admission')
      }
      closeConfiguration()
    } else {
      const id = deploymentID.value
      const body = { display_name: name, enabled: record.value.enabled, declared_node_id: form.declared_node_id, version: record.value.version }
      const value = validRecord(await engineDeploymentsAPI.update(id, body), id)
      if (own !== epoch) return
      populate(value)
    }
    ElMessage.success(t('system.engineDeployments.saved'))
    await loadList()
  } catch (error) {
    if (own !== epoch) return
    const status = error.response?.status, code = error.response?.data?.error_code
    blocked.value = !status || status >= 500 || ['resource_version_conflict', 'engine_deployment_source_changed', 'engine_deployment_source_occupied'].includes(code)
    saveError.value = t(`system.engineDeployments.${code === 'engine_deployment_source_changed' ? 'sourceChanged' : code === 'engine_deployment_source_occupied' ? 'occupied' : code === 'engine_deployment_host_unavailable' ? 'hostMismatch' : blocked.value ? 'unknownWrite' : 'saveFailed'}`)
  } finally {
    if (own === epoch) saving.value = false
  }
}
watch([()=>route.fullPath,identity],async()=>{
  epoch++;clearCurrentSource();listRequests.invalidate();detailRequests.invalidate();closeConfiguration();rows.value=[];record.value=null;selectedHost.value=null;formReady.value=false;saving.value=false;blocked.value=false;detailLoading.value=false;detailError.value='';saveError.value='';listError.value='';listLoading.value=false;listLoaded.value=false;Object.assign(form,{inventory:null,declared_node_id:'',display_name:''});searchDraft.value=state.value.search
  if(!canRead.value)return
  if(state.value.changed){await navigate(route.path,state.value.query,'replace');return}
  await Promise.all([loadList(),loadDetail()])
},{immediate:true,flush:'sync'})
onBeforeUnmount(()=>{clearCurrentSource();epoch++;listRequests.dispose();detailRequests.dispose();discoveryRequests.dispose();sourceRequests.dispose()})
</script>
<style scoped>
.toolbar > h2 { color: var(--addp-text-primary); }
.engine-deployments{padding:20px;min-width:0}.toolbar{display:flex;align-items:center;gap:12px;margin-bottom:16px;flex-wrap:wrap}.toolbar h2{flex:1;margin:0}.toolbar .el-input{max-width:420px}.el-pagination{margin-top:16px;overflow-x:auto}.el-alert{margin-bottom:12px}.current-source{margin-bottom:20px}.current-source h3{flex:1;margin:0}.candidate-name{margin-top:6px}.hint{color:var(--addp-text-secondary);margin:8px 0;width:100%}dl{display:grid;grid-template-columns:140px minmax(0,1fr);gap:10px}dd{margin:0;overflow-wrap:anywhere}dt{color:var(--addp-text-secondary)}@media(max-width:600px){.engine-deployments{padding:12px}.toolbar .el-input{max-width:none;flex-basis:100%}dl{grid-template-columns:1fr;gap:6px}dd{margin-bottom:8px}}
.candidate-unavailable-icon{color:var(--el-text-color-placeholder);font-size:20px}
:deep(.candidate-unavailable td.el-table__cell){color:var(--el-text-color-placeholder);background-color:var(--el-fill-color-light)}
.candidate-status{max-width:100%;height:auto;white-space:normal;line-height:1.5;padding:3px 8px}
:deep(.candidate-status .el-tag__content){white-space:normal;overflow-wrap:anywhere}
.change-preview p{overflow-wrap:anywhere;margin:6px 0}.collection-time{font-size:12px;color:var(--addp-text-secondary);overflow-wrap:anywhere}
</style>
