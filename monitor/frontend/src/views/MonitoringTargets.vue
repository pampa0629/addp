<template>
  <section v-if="canRead" class="monitoring-targets" data-testid="monitoring-targets">
    <header class="target-toolbar">
      <h2 ref="pageHeading" tabindex="-1">{{ t('monitor.targets.title') }}</h2>
      <el-button :loading="loading" @click="loadList">{{ t('monitor.targets.refresh') }}</el-button>
      <el-button v-if="canCreate" type="primary" @click="navigate('/monitoring-targets/new')">{{ t('monitor.targets.create') }}</el-button>
    </header>
    <el-alert v-if="listError" :title="listError" type="error" :closable="false" show-icon />
    <el-alert v-if="!dialogOpen && listCollectionError" :title="t(listCollectionError)" type="warning" :closable="false" show-icon data-testid="target-list-collection-error" />
    <el-table v-loading="loading" :data="rows" border stripe>
      <el-table-column :label="t('monitor.targets.node')" min-width="170" show-overflow-tooltip><template #default="{ row }">{{ nodeNames[row.subject.node_id] || t('monitor.targets.nodeMissing') }}</template></el-table-column>
      <el-table-column :label="t('monitor.targets.configState')" width="100"><template #default="{ row }"><el-tag :type="row.enabled ? 'success' : 'info'">{{ t(row.enabled ? 'monitor.targets.enabled' : 'monitor.targets.disabled') }}</el-tag></template></el-table-column>
      <el-table-column v-if="!dialogOpen" :label="t('monitor.targets.collection.title')" min-width="210">
        <template #header><span>{{ t('monitor.targets.collection.title') }}</span><el-tooltip :content="t('monitor.targets.collection.metricHint')"><el-icon :aria-label="t('monitor.targets.collection.metricHint')" tabindex="0"><InfoFilled /></el-icon></el-tooltip></template>
        <template #default="{ row }">
          <div :data-testid="`target-list-collection-${row.id}`">
            <el-tag class="target-collection-status" :type="listCollectionType(row)">{{ t(listCollectionKey(row)) }}</el-tag>
            <p v-if="listCollections.get(row.id)?.collection?.sampled_at" class="target-sample-time" :title="t('monitor.targets.listCollection.queried', { time: listDate(listCollections.get(row.id).queried_at) })">{{ t('monitor.resources.sampledAt') }}: {{ listDate(listCollections.get(row.id).collection.sampled_at) }}</p>
          </div>
        </template>
      </el-table-column>
      <el-table-column :label="t('monitor.targets.kind')" min-width="160"><template #default="{ row }">{{ t(`monitor.targets.kinds.${row.monitor_kind}`) }}</template></el-table-column>
      <el-table-column :label="t('monitor.targets.endpoint')" min-width="220" show-overflow-tooltip><template #default="{ row }">{{ row.source.endpoint }}</template></el-table-column>
      <el-table-column prop="id" :label="t('monitor.targets.id')" min-width="220" show-overflow-tooltip />
      <el-table-column prop="version" :label="t('monitor.targets.version')" width="140" />
      <el-table-column :label="t('monitor.targets.details')" fixed="right" width="100"><template #default="{ row }"><el-button text type="primary" @click="navigate(`/monitoring-targets/${row.id}`)">{{ t('monitor.targets.details') }}</el-button></template></el-table-column>
      <template #empty><el-empty :description="t(loaded ? 'monitor.targets.empty' : 'monitor.targets.notLoaded')" :image-size="64" /></template>
    </el-table>
    <div class="target-pagination"><el-pagination v-if="loaded" :current-page="state.page" :page-size="state.pageSize" :page-sizes="[10, 20, 50]" :total="total" layout="total, sizes, prev, pager, next" @current-change="changePage" @size-change="changeSize" /></div>
    <el-dialog :model-value="dialogOpen" :title="t(creating ? 'monitor.targets.create' : 'monitor.targets.details')" width="min(680px, calc(100vw - 24px))"
      :close-on-click-modal="!saving" :close-on-press-escape="!saving" :show-close="!saving" @update:model-value="value => !value && closeDialog()" @opened="focusForm" @closed="restoreFocus">
      <div v-loading="detailLoading">
        <el-alert v-if="detailError" :title="detailError" type="error" :closable="false" show-icon />
        <el-alert v-if="saveError" :title="saveError" type="error" :closable="false" show-icon data-testid="target-save-error" />
        <section v-if="creating && canCreate" class="target-admission" data-testid="target-admission">
          <input ref="admissionFileInput" type="file" accept="application/json,.json" hidden data-testid="admission-file" @change="importAdmission" />
          <el-button :loading="admissionReading" :disabled="saving || Boolean(admissionHostCreated) || admissionBlocked" @click="admissionFileInput?.click()">{{ t('monitor.targets.admission.import') }}</el-button>
          <el-button v-if="(admission || admissionError) && !admissionHostCreated && !admissionBlocked" :disabled="saving" @click="clearAdmission">{{ t('monitor.targets.admission.clear') }}</el-button>
          <el-alert v-if="admissionError" :title="t(admissionError)" type="error" :closable="false" show-icon />
          <template v-if="admission">
            <h3>{{ t('monitor.targets.admission.hostFacts') }}</h3>
            <dl class="admission-facts">
              <dt>{{ t('monitor.targets.node') }}</dt><dd>{{ admission.host.display_name }}</dd>
              <dt>{{ t('monitor.targets.admission.hostKind') }}</dt><dd>{{ t(`monitor.targets.admission.kinds.${admission.host.node_kind}`) }}</dd>
              <dt>{{ t('monitor.targets.admission.addresses') }}</dt><dd>{{ admission.host.addresses.join(', ') || '—' }}</dd>
              <dt>{{ t('monitor.targets.admission.modules') }}</dt><dd>{{ admission.host.allowed_module_bindings.map(binding => binding.module_name).join(', ') || '—' }}</dd>
            </dl>
            <el-radio-group v-if="!admissionHostCreated" :model-value="admissionMode" :disabled="saving || admissionBlocked" :aria-label="t('monitor.targets.admission.mode')" @update:model-value="selectAdmissionMode">
              <el-radio value="existing">{{ t('monitor.targets.admission.existing') }}</el-radio>
              <el-radio v-if="canCreateNode" value="new">{{ t('monitor.targets.admission.new') }}</el-radio>
            </el-radio-group>
            <p class="target-hint">{{ t(admissionMode === 'new' ? 'monitor.targets.admission.newHint' : 'monitor.targets.admission.existingHint') }}</p>
          </template>
          <el-alert v-if="admissionHostCreated" :title="t('monitor.targets.admission.created', { name: admissionHostCreated.display_name, id: admissionHostCreated.node_id })" type="success" :closable="false" show-icon data-testid="admission-created" />
          <el-alert v-if="admissionProgress" :title="t(admissionProgress)" type="warning" :closable="false" show-icon data-testid="admission-progress" />
          <template v-if="admissionHostCreated || (admissionBlocked && selectedNode)">
            <el-button text type="primary" @click="navigate('/monitoring-targets', {})">{{ t('monitor.targets.admission.verifyCollection') }}</el-button>
            <el-button v-if="canObserve" text type="primary" @click="navigate(`/node-resources/${selectedNode.node_id}`, {})">{{ t('monitor.targets.admission.verify') }}</el-button>
          </template>
        </section>
        <el-form v-if="formReady" label-position="top" :disabled="!(creating ? canCreate : canRead && auth.hasPermission('monitor.monitoring_target.update')) || saving || conflicted" @submit.prevent>
          <el-form-item v-if="!admission || admissionMode === 'existing'" :label="t('monitor.targets.node')">
            <HostNodeSelect v-if="creating" :key="route.fullPath" ref="nodeInput" v-model="form.node_id" :selected="selectedNode" :disabled="Boolean(admissionHostCreated) || admissionBlocked" @selected="selectedNode = $event" />
            <el-input v-else :model-value="selectedNode?.display_name || t('monitor.targets.nodeMissing')" readonly data-testid="target-node" />
            <p v-if="selectedNode" class="target-hint">{{ selectedNode.addresses.join(', ') }}</p>
          </el-form-item>
          <el-form-item :label="t('monitor.targets.kind')"><el-select v-model="form.monitor_kind" :disabled="!creating || Boolean(admission)" :aria-label="t('monitor.targets.kind')"><el-option v-for="kind in Object.keys(targetKinds)" :key="kind" :value="kind" :label="t(`monitor.targets.kinds.${kind}`)" /></el-select></el-form-item>
          <el-form-item :label="t('monitor.targets.endpoint')"><el-input ref="endpointInput" v-model="form.endpoint" maxlength="512" :placeholder="t('monitor.targets.endpointHint')" data-testid="target-endpoint" /></el-form-item>
          <el-form-item :label="t('monitor.targets.configState')"><el-switch v-model="form.enabled" :active-text="t('monitor.targets.enabled')" :inactive-text="t('monitor.targets.disabled')" :aria-label="t('monitor.targets.configState')" data-testid="target-enabled" /></el-form-item>
        </el-form>
        <section v-if="!creating && formReady" class="target-collection" aria-labelledby="target-collection-heading" data-testid="target-collection">
          <div class="target-collection-toolbar">
            <h3 id="target-collection-heading">{{ t('monitor.targets.collection.title') }}</h3>
            <el-button v-if="canObserve && target.enabled && target.monitor_kind === 'host_resources'" :loading="collectionLoading" @click="loadCollection">{{ t('monitor.targets.collection.refresh') }}</el-button>
          </div>
          <p v-if="draftChanged" class="target-hint">{{ t('monitor.targets.collection.savedConfig') }}</p>
          <p v-if="!canObserve" role="status">{{ t('monitor.targets.collection.denied') }}</p>
          <p v-else-if="target.monitor_kind !== 'host_resources'" role="status">{{ t('monitor.targets.collection.unsupported') }}</p>
          <p v-else-if="!target.enabled" role="status">{{ t('monitor.targets.collection.disabled') }}</p>
          <p v-else-if="collectionLoading" role="status">{{ t('monitor.targets.collection.loading') }}</p>
          <el-alert v-else-if="collectionError" :title="t(collectionError)" type="warning" :closable="false" show-icon />
          <template v-else-if="collection">
            <p role="status"><strong>{{ t(`monitor.targets.collection.states.${collection.collection.state}`) }}</strong></p>
            <p v-if="collection.collection.state !== 'collecting'" class="target-hint">{{ t(`monitor.resources.collection.${collection.collection.state}`) }}</p>
            <p v-if="collection.collection.state === 'collecting'" class="target-hint">{{ t('monitor.targets.collection.metricHint') }}</p>
            <p>{{ t('monitor.resources.sampledAt') }}: {{ collection.collection.sampled_at ? formatDateTime(collection.collection.sampled_at) : '—' }}</p>
            <p>{{ t('monitor.resources.queriedAt') }}: {{ formatDateTime(collection.queried_at) }}</p>
          </template>
          <el-button v-if="canObserve && target.monitor_kind === 'host_resources'" text type="primary" @click="navigate(`/node-resources/${target.subject.node_id}`, {})">{{ t('monitor.targets.collection.resources') }}</el-button>
        </section>
        <p v-if="creating" class="target-hint">{{ t('monitor.targets.configHint') }}</p>
        <el-button text type="primary" @click="openNodes">{{ t('monitor.targets.nodeInventory') }}</el-button>
      </div>
      <template #footer>
        <el-button ref="closeButton" :disabled="saving" @click="closeDialog">{{ t('common.close') }}</el-button>
        <el-button v-if="!creating" :disabled="saving || detailLoading" @click="loadDetail">{{ t('monitor.targets.reload') }}</el-button>
        <el-popconfirm v-if="canRemove && formReady" :title="t('monitor.targets.deleteConfirm')" :confirm-button-text="t('common.confirm')" :cancel-button-text="t('common.cancel')" @confirm="remove">
          <template #reference><el-button type="danger" :disabled="saving || conflicted" data-testid="target-delete">{{ t('common.delete') }}</el-button></template>
        </el-popconfirm>
        <el-button v-if="canWrite && formReady" type="primary" :loading="saving" :disabled="conflicted" data-testid="target-save" @click="save">{{ t(admission ? 'monitor.targets.admission.submit' : 'common.save') }}</el-button>
      </template>
    </el-dialog>
    <StatusAnnouncer :message="statusMessage" />
  </section>
  <el-alert v-else :title="t('monitor.targets.accessDenied')" type="error" :closable="false" show-icon />
</template>

<script setup>
import { computed, nextTick, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { InfoFilled } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { openConsoleRoute, StatusAnnouncer, useConsolePageDescriptor } from '@common-ui'
import { useAuthStore } from '../store/auth'
import { monitoringTargetsAPI as api } from '../api/monitoringTargets'
import { nodeResourcesAPI as hostsAPI } from '../api/nodeResources'
import HostNodeSelect from '../components/HostNodeSelect.vue'
import { parseHostAdmission } from '../utils/hostAdmission'
import { loadHostReferences } from '../utils/hostReferences'
import { isTargetUUID, resolveTargetRoute, targetInput, targetKinds, validateTargetCollectionReference } from '../utils/monitoringTargets'
import { navigateMonitorRoute } from '../utils/moduleNavigation'
import { validateCollection, validateResourceResponse, validateResourceSummaries, resourceErrorKey } from '../utils/nodeResources'
import { formatDateTime } from '../../../../common-frontend/basic/src/utils/formatters.js'
import { formatFieldPresentationValue } from '../../../../common-frontend/basic/src/utils/fieldPresentation.mjs'

const { t, locale } = useI18n(), auth = useAuthStore(), route = useRoute(), router = useRouter()
const canRead = computed(() => auth.contextType === 'platform' && auth.authContext?.principal?.type === 'user' && !auth.authContext?.delegation && auth.hasPermission('monitor.monitoring_target.read') && auth.hasPermission('platform.host_node.read'))
const canCreate = computed(() => canRead.value && auth.hasPermission('monitor.monitoring_target.create'))
const canCreateNode = computed(() => canCreate.value && auth.hasPermission('platform.host_node.create'))
const canObserve = computed(() => canRead.value && auth.hasPermission('monitor.resource_observation.read'))
const creating = computed(() => route.name === 'MonitoringTargetNew')
const targetID = computed(() => typeof route.params.id === 'string' ? route.params.id : '')
const dialogOpen = computed(() => creating.value || Boolean(targetID.value))
const canWrite = computed(() => !admissionReading.value && !admissionError.value && !admissionBlocked.value
  && (Boolean(selectedNode.value) || (admission.value && admissionMode.value === 'new' && canCreateNode.value))
  && (creating.value ? canCreate.value : canRead.value && auth.hasPermission('monitor.monitoring_target.update')))
const canRemove = computed(() => !creating.value && canRead.value && auth.hasPermission('monitor.monitoring_target.delete'))
const state = computed(() => resolveTargetRoute(route.query, creating.value))
const rows = ref([]), total = ref(0), loading = ref(false), loaded = ref(false), listError = ref('')
const target = ref(null), formReady = ref(false), detailLoading = ref(false), detailError = ref(''), saveError = ref(''), saving = ref(false), conflicted = ref(false), statusMessage = ref('')
const collection = ref(null), collectionLoading = ref(false), collectionError = ref('')
let collectionTicket = 0, collectionController = null
const selectedNode = ref(null)
const admission = ref(null), admissionMode = ref('existing'), admissionFileInput = ref(null), admissionReading = ref(false), admissionError = ref('')
const admissionHostCreated = ref(null), admissionBlocked = ref(false), admissionProgress = ref('')
let admissionReadTicket = 0
const hostReferences = ref({})
const nodeNames = computed(() => Object.fromEntries(Object.entries(hostReferences.value).map(([id, host]) => [id, host.display_name])))
const listCollections = ref(new Map()), listCollectionLoading = ref(false), listCollectionError = ref('')
let listCollectionController = null
const listDate = value => formatFieldPresentationValue(value, { temporal_format: 'datetime' }, locale.value)
const nodeInput = ref(null), endpointInput = ref(null), closeButton = ref(null), pageHeading = ref(null)
const form = reactive({ node_id: '', monitor_kind: 'host_resources', endpoint: '', enabled: true })
const draftChanged = computed(() => target.value && (form.endpoint !== target.value.source.endpoint || form.enabled !== target.value.enabled))
const identity = computed(() => JSON.stringify([auth.authContext?.principal, auth.authContext?.context, auth.authContext?.delegation, auth.permissions]))
let epoch = 0, listTicket = 0, detailTicket = 0
const pending = new Set()
useConsolePageDescriptor(router, 'monitor', { title: computed(() => t('monitor.targets.title')), subject: computed(() => target.value?.id || ''), ready: computed(() => Boolean(target.value)) })

function invalidate() {
  epoch++
  for (const controller of pending) controller.abort()
  pending.clear()
}
async function request(work) {
  const controller = new AbortController(); pending.add(controller)
  try { return await work({ signal: controller.signal }) }
  finally { pending.delete(controller) }
}
function navigate(path, query = state.value.query, history = 'push') { return navigateMonitorRoute(router, { path, query }, { history }) }
function changeSize(size) { return navigate('/monitoring-targets', { ...state.value.query, page: undefined, page_size: size === 20 ? undefined : String(size) }, 'replace') }
function changePage(page) { return navigate(route.path, { ...state.value.query, page: page === 1 ? undefined : String(page) }, 'replace') }
function closeDialog() { if (!saving.value) return navigate('/monitoring-targets', state.value.query, 'replace') }
function openNodes() { return openConsoleRoute('/system/host-nodes') }
function clearListCollections() {
  listCollectionController?.abort(); listCollectionController = null
  listCollections.value = new Map(); listCollectionLoading.value = false; listCollectionError.value = ''
}
function collectionFailure(error) { return error.message === 'target_collection_changed' ? 'monitor.targets.collection.changed' : resourceErrorKey(error) }
function listCollectionKey(row) {
  if (!canObserve.value) return 'monitor.targets.collection.denied'
  if (row.monitor_kind !== 'host_resources') return 'monitor.targets.listCollection.unsupported'
  if (!row.enabled) return 'monitor.targets.listCollection.disabled'
  if (loading.value) return 'monitor.targets.collection.loading'
  if (!hostReferences.value[row.subject.node_id]) return 'monitor.targets.listCollection.hostUnavailable'
  if (listCollectionLoading.value) return 'monitor.targets.collection.loading'
  if (listCollectionError.value) return 'monitor.targets.listCollection.unavailable'
  const observed = listCollections.value.get(row.id)
  return observed ? observed.error || `monitor.targets.collection.states.${observed.collection.state}` : 'monitor.targets.listCollection.unavailable'
}
function listCollectionType(row) {
  const key = listCollectionKey(row)
  return key.endsWith('.collecting') ? 'success' : key.endsWith('.failed') ? 'danger' : key.endsWith('.stale') ? 'warning' : 'info'
}
function clearCollection() {
  collectionTicket++; collectionController?.abort(); collectionController = null
  collection.value = null; collectionError.value = ''; collectionLoading.value = false
}
function resetForm() { clearCollection(); clearAdmission(); admissionHostCreated.value = null; admissionBlocked.value = false; admissionProgress.value = ''; Object.assign(form, { node_id: '', monitor_kind: 'host_resources', endpoint: '', enabled: true }); formReady.value = false; target.value = null; selectedNode.value = null }

function clearAdmission() {
  admissionReadTicket++; admission.value = null; admissionMode.value = 'existing'; admissionReading.value = false; admissionError.value = ''
}
function selectAdmissionMode(value) {
  if (saving.value || admissionBlocked.value || admissionHostCreated.value || !['existing', 'new'].includes(value) || (value === 'new' && !canCreateNode.value)) return
  admissionMode.value = value; form.node_id = ''; selectedNode.value = null; saveError.value = ''
}
async function importAdmission(event) {
  if (!creating.value || !canCreate.value || saving.value || admissionHostCreated.value || admissionBlocked.value) return
  const file = event.target.files?.[0], generation = epoch
  clearAdmission(); const ticket = admissionReadTicket
  if (!file) return
  admissionReading.value = true
  try {
    if (file.size > 16384) throw new Error('invalid_host_admission')
    const value = parseHostAdmission(await file.text())
    if (generation !== epoch || ticket !== admissionReadTicket || !canCreate.value) return
    admission.value = value; form.monitor_kind = value.collection.monitor_kind; form.endpoint = value.collection.source.endpoint; form.enabled = true; saveError.value = ''
  } catch { if (generation === epoch && ticket === admissionReadTicket) admissionError.value = 'monitor.targets.admission.invalid' }
  finally { event.target.value = ''; if (generation === epoch && ticket === admissionReadTicket) admissionReading.value = false }
}
function populate(value) {
  clearCollection()
  target.value = value
  Object.assign(form, { node_id: value.subject.node_id, monitor_kind: value.monitor_kind, endpoint: value.source.endpoint, enabled: value.enabled })
  formReady.value = true
}
async function focusForm() {
  await nextTick()
  if ((creating.value ? canCreate.value : canWrite.value) && formReady.value && !conflicted.value) (creating.value ? nodeInput.value : endpointInput.value)?.focus()
  else closeButton.value?.$el?.focus()
}
async function restoreFocus() { await nextTick(); if (canRead.value && !dialogOpen.value) pageHeading.value?.focus() }
async function readNode(id) {
  const host = await request(config => hostsAPI.node(id, config))
  if (host?.node_id !== id || !isTargetUUID(id) || typeof host.display_name !== 'string' || !host.display_name.trim() || !Array.isArray(host.addresses) || host.addresses.some(address => typeof address !== 'string')) throw new Error('invalid_host')
  return host
}
async function loadList() {
  if (!canRead.value) return
  const generation = epoch, ticket = ++listTicket
  clearListCollections()
  loading.value = true; loaded.value = false; rows.value = []; hostReferences.value = {}; listError.value = ''
  try {
    const value = await request(config => api.list({ page: state.value.page, page_size: state.value.pageSize }, config))
    if (generation !== epoch || ticket !== listTicket) return
    rows.value = value.data; total.value = value.total; loaded.value = true
    const hosts = await loadHostReferences(value.data.map(row => row.subject.node_id), readNode, () => generation === epoch && ticket === listTicket)
    if (generation !== epoch || ticket !== listTicket) return
    hostReferences.value = hosts
    loading.value = false
    await loadListCollections(value.data, hosts, generation, ticket)
  } catch { if (generation === epoch && ticket === listTicket) listError.value = t('monitor.targets.loadFailed') }
  finally { if (generation === epoch && ticket === listTicket) loading.value = false }
}
async function loadListCollections(savedTargets, hosts, generation, ticket) {
  if (route.name !== 'MonitoringTargets' || !canObserve.value) return
  const targets = savedTargets.filter(row => row.enabled && row.monitor_kind === 'host_resources' && hosts[row.subject.node_id])
  const nodes = [...new Set(targets.map(row => row.subject.node_id))].map(id => hosts[id])
  if (!nodes.length) return
  const controller = new AbortController()
  listCollectionController = controller; pending.add(controller); listCollectionLoading.value = true
  try {
    const response = await hostsAPI.summaries(nodes.map(row => row.node_id), { signal: controller.signal })
    if (generation !== epoch || ticket !== listTicket) return
    const observations = validateResourceSummaries(response, nodes), next = new Map()
    for (const target of targets) {
      const value = observations.get(target.subject.node_id)
      try {
        validateTargetCollectionReference(value, target)
        next.set(target.id, { collection: value.collection, queried_at: value.queried_at })
      } catch (error) {
        if (error.message !== 'target_collection_changed') throw error
        next.set(target.id, { error: 'monitor.targets.listCollection.changed' })
      }
    }
    listCollections.value = next
  } catch (error) {
    if (generation === epoch && ticket === listTicket) {
      listCollections.value = new Map()
      listCollectionError.value = error.response?.data?.error_code === 'observability_query_budget_exceeded' ? 'monitor.targets.listCollection.budget' : collectionFailure(error)
    }
  } finally {
    pending.delete(controller)
    if (generation === epoch && ticket === listTicket) { listCollectionLoading.value = false; listCollectionController = null }
  }
}
async function loadDetail() {
  if (!canRead.value || !targetID.value) return
  const generation = epoch, ticket = ++detailTicket, id = targetID.value
  resetForm(); detailError.value = ''; saveError.value = ''; conflicted.value = false
  if (!isTargetUUID(id)) { detailError.value = t('monitor.targets.invalidID'); return }
  detailLoading.value = true
  try {
    const value = await request(config => api.get(id, config))
    if (generation !== epoch || ticket !== detailTicket) return
    populate(value); formReady.value = false
    const host = await readNode(value.subject.node_id)
    if (generation !== epoch || ticket !== detailTicket) return
    selectedNode.value = host
    formReady.value = true; void loadCollection(); await focusForm()
  } catch (error) { if (generation === epoch && ticket === detailTicket) detailError.value = t(error.response?.status === 404 ? 'monitor.targets.notFound' : 'monitor.targets.loadFailed') }
  finally { if (generation === epoch && ticket === detailTicket) detailLoading.value = false }
}
async function loadCollection() {
  clearCollection()
  const saved = target.value
  if (!canObserve.value || !formReady.value || creating.value || !saved?.enabled || saved.monitor_kind !== 'host_resources') return
  const generation = epoch, ticket = collectionTicket, controller = new AbortController()
  collectionController = controller; pending.add(controller); collectionLoading.value = true
  try {
    const metrics = ['node.memory.total_bytes']
    const value = await hostsAPI.instant({ node_id: saved.subject.node_id, metrics: metrics.join(',') }, { signal: controller.signal })
    if (generation !== epoch || ticket !== collectionTicket) return
    validateCollection(value.collection, value.queried_at)
    validateResourceResponse(value, saved.subject.node_id, metrics)
    validateTargetCollectionReference(value, saved)
    collection.value = value
  } catch (error) { if (generation === epoch && ticket === collectionTicket) collectionError.value = collectionFailure(error) }
  finally {
    pending.delete(controller)
    if (generation === epoch && ticket === collectionTicket) { collectionLoading.value = false; collectionController = null }
  }
}
function writeError(error) {
  conflicted.value = !creating.value && error.response?.data?.error_code === 'resource_version_conflict'
  saveError.value = conflicted.value ? t('monitor.targets.conflict') : error.response?.data?.error || t('monitor.targets.saveFailed')
}
async function save() {
  if (!canWrite.value || !formReady.value || saving.value || conflicted.value) return
  const generation = epoch, id = targetID.value, isCreate = creating.value
  if (!(admission.value && admissionMode.value === 'new') && !targetInput(form, target.value)) { saveError.value = t('monitor.targets.invalidForm'); return }
  saving.value = true; saveError.value = ''; statusMessage.value = t('monitor.targets.saving')
  let writeStage = ''
  try {
    if (admission.value) {
      let host
      if (admissionMode.value === 'new') {
        if (!canCreateNode.value) return
        writeStage = 'host'
        host = await request(config => hostsAPI.createNode({ ...admission.value.host, enabled: true }, config))
        if (generation !== epoch) return
        if (!isTargetUUID(host?.node_id)) throw new Error('invalid_host_response')
        admissionHostCreated.value = host; admissionMode.value = 'existing'; selectedNode.value = host; form.node_id = host.node_id
        admissionProgress.value = 'monitor.targets.admission.partial'
      } else {
        host = await request(config => hostsAPI.node(form.node_id, config))
        if (generation !== epoch) return
      }
      if (host?.node_id !== form.node_id || host.node_kind !== admission.value.host.node_kind || host.enabled !== true) {
        saveError.value = t('monitor.targets.admission.hostMismatch'); return
      }
    }
    const body = targetInput(form, target.value)
    if (!body) { saveError.value = t('monitor.targets.invalidForm'); return }
    writeStage = 'collection'
    const value = await request(config => isCreate ? api.create(body, config) : api.update(id, body, config))
    if (generation !== epoch) return
    ElMessage.success(t('monitor.targets.saved'))
    if (isCreate) { const { node_id, ...query } = state.value.query; await navigate(`/monitoring-targets/${value.id}`, query, 'replace') }
    else { const host = selectedNode.value; populate(value); selectedNode.value = host; void loadCollection(); await loadList() }
  } catch (error) {
    if (generation !== epoch) return
    writeError(error)
    const conflict = error.response?.status === 409 && error.response?.data?.error_code === 'resource_version_conflict'
    if (admission.value && writeStage && (!error.response || error.response.status >= 500 || conflict)) {
      admissionBlocked.value = true
      admissionProgress.value = conflict ? 'monitor.targets.admission.conflict' : writeStage === 'host' ? 'monitor.targets.admission.hostUnknown' : 'monitor.targets.admission.collectionUnknown'
    }
  }
  finally { if (generation === epoch) { saving.value = false; statusMessage.value = '' } }
}
async function remove() {
  if (!canRemove.value || !target.value || saving.value || conflicted.value) return
  const generation = epoch, id = target.value.id, version = target.value.version
  saving.value = true; saveError.value = ''; statusMessage.value = t('monitor.targets.saving')
  try {
    await request(config => api.remove(id, version, config))
    if (generation !== epoch) return
    ElMessage.success(t('monitor.targets.deleted'))
    await navigate('/monitoring-targets', state.value.query, 'replace')
  } catch (error) { if (generation === epoch) writeError(error) }
  finally { if (generation === epoch) { saving.value = false; statusMessage.value = '' } }
}
watch([() => route.fullPath, identity], async () => {
  invalidate(); clearListCollections(); resetForm(); hostReferences.value = {}
  rows.value = []; total.value = 0; loaded.value = false; loading.value = false; detailLoading.value = false
  listError.value = ''; detailError.value = ''; saveError.value = ''; statusMessage.value = ''; saving.value = false; conflicted.value = false
  if (!['MonitoringTargets', 'MonitoringTargetNew', 'MonitoringTargetDetail'].includes(route.name)) return
  if (!canRead.value) return
  if (state.value.changed) { await navigate(route.path, state.value.query, 'replace'); return }
  if (creating.value) {
    formReady.value = canCreate.value
    const generation = epoch
    if (generation !== epoch) return
    if (state.value.query.node_id) form.node_id = state.value.query.node_id
  }
  await Promise.all([loadList(), creating.value ? Promise.resolve() : loadDetail()])
}, { immediate: true, flush: 'sync' })
onBeforeUnmount(invalidate)
</script>

<style scoped>
.monitoring-targets { padding: 20px; min-width: 0; color: var(--addp-text-primary); }
.target-toolbar { display: flex; flex-wrap: wrap; align-items: center; gap: 12px; margin-bottom: 16px; }
.target-toolbar h2 { flex: 1; }
.target-pagination { margin-top: 16px; overflow-x: auto; }
.target-collection-status { max-width: 100%; height: auto; white-space: normal; line-height: 1.6; }
.target-sample-time { margin: 8px 0 0; color: var(--addp-text-secondary); font-size: 12px; overflow-wrap: anywhere; }
.target-hint { color: var(--addp-text-secondary); margin: 12px 0; }
.target-collection { margin: 12px 0; padding: 12px; border: 1px solid var(--addp-border-color); border-radius: 4px; }
.target-collection-toolbar { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; }
.target-collection-toolbar h3 { flex: 1; margin: 0; }
.target-collection p { overflow-wrap: anywhere; }
.target-admission { margin-bottom: 16px; }
.target-admission h3 { margin-bottom: 12px; }
.admission-facts { display: grid; grid-template-columns: auto minmax(0, 1fr); gap: 8px 16px; }
.admission-facts dt { color: var(--addp-text-secondary); }
.admission-facts dd { margin: 0; overflow-wrap: anywhere; }
.el-alert { margin-bottom: 12px; }
:deep(.el-dialog__footer) { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: 8px; }
:deep(.el-dialog__footer .el-button + .el-button) { margin-left: 0; }
@media (max-width: 600px) { .monitoring-targets { padding: 12px; } }
</style>
