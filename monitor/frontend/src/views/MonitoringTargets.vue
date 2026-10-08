<template>
  <section v-if="canRead" class="monitoring-targets" data-testid="monitoring-targets">
    <header class="target-toolbar">
      <h2 ref="pageHeading" tabindex="-1">{{ t('monitor.targets.title') }}</h2>
      <el-button :loading="loading" @click="loadList">{{ t('monitor.targets.refresh') }}</el-button>
      <el-button v-if="canCreate" type="primary" @click="navigate('/monitoring-targets/new')">{{ t('monitor.targets.create') }}</el-button>
    </header>
    <el-alert v-if="listError" :title="listError" type="error" :closable="false" show-icon />
    <el-table v-loading="loading" :data="rows" border stripe>
      <el-table-column prop="id" :label="t('monitor.targets.id')" min-width="220" show-overflow-tooltip />
      <el-table-column :label="t('monitor.targets.node')" min-width="220" show-overflow-tooltip><template #default="{ row }">{{ nodeNames[row.subject.node_id] || t('monitor.targets.nodeMissing') }}</template></el-table-column>
      <el-table-column :label="t('monitor.targets.kind')" min-width="160"><template #default="{ row }">{{ t(`monitor.targets.kinds.${row.monitor_kind}`) }}</template></el-table-column>
      <el-table-column :label="t('monitor.targets.endpoint')" min-width="220" show-overflow-tooltip><template #default="{ row }">{{ row.source.endpoint }}</template></el-table-column>
      <el-table-column :label="t('monitor.targets.configState')" width="160"><template #default="{ row }"><el-tag :type="row.enabled ? 'success' : 'info'">{{ t(row.enabled ? 'monitor.targets.enabled' : 'monitor.targets.disabled') }}</el-tag></template></el-table-column>
      <el-table-column prop="version" :label="t('monitor.targets.version')" width="140" />
      <el-table-column :label="t('monitor.targets.details')" fixed="right" width="100"><template #default="{ row }"><el-button text type="primary" @click="navigate(`/monitoring-targets/${row.id}`)">{{ t('monitor.targets.details') }}</el-button></template></el-table-column>
      <template #empty><el-empty :description="t(loaded ? 'monitor.targets.empty' : 'monitor.targets.notLoaded')" :image-size="64" /></template>
    </el-table>
    <div class="target-pagination"><el-pagination v-if="loaded" :current-page="state.page" :page-size="state.pageSize" :total="total" layout="total, prev, pager, next" @current-change="changePage" /></div>
    <el-dialog :model-value="dialogOpen" :title="t(creating ? 'monitor.targets.create' : 'monitor.targets.details')" width="min(680px, calc(100vw - 24px))"
      :close-on-click-modal="!saving" :close-on-press-escape="!saving" :show-close="!saving" @update:model-value="value => !value && closeDialog()" @opened="focusForm" @closed="restoreFocus">
      <div v-loading="detailLoading">
        <el-alert v-if="detailError" :title="detailError" type="error" :closable="false" show-icon />
        <el-alert v-if="saveError" :title="saveError" type="error" :closable="false" show-icon data-testid="target-save-error" />
        <el-form v-if="formReady" label-position="top" :disabled="!(creating ? canCreate : canRead && auth.hasPermission('monitor.monitoring_target.update')) || saving || conflicted" @submit.prevent>
          <el-form-item :label="t('monitor.targets.node')">
            <el-select v-if="creating" ref="nodeInput" v-model="form.node_id" filterable remote :remote-method="searchNodes" :loading="nodesLoading" :placeholder="t('monitor.targets.nodeSearch')" :aria-label="t('monitor.targets.node')" data-testid="target-node" @change="selectNode">
              <el-option v-for="host in selectableNodes" :key="host.node_id" :value="host.node_id" :label="host.display_name" />
            </el-select>
            <el-input v-else :model-value="selectedNode?.display_name || t('monitor.targets.nodeMissing')" readonly data-testid="target-node" />
            <p v-if="selectedNode" class="target-hint">{{ selectedNode.addresses.join(', ') }}</p>
            <el-button v-if="creating && nodeOptions.length < nodesTotal" text :loading="nodesLoading" @click="loadNodes(nodeSearch, nodesPage + 1)">{{ t('monitor.targets.moreNodes') }}</el-button>
            <el-alert v-if="nodesError" :title="t(nodesError)" type="error" :closable="false" />
          </el-form-item>
          <el-form-item :label="t('monitor.targets.kind')"><el-select v-model="form.monitor_kind" :disabled="!creating" :aria-label="t('monitor.targets.kind')"><el-option v-for="kind in Object.keys(targetKinds)" :key="kind" :value="kind" :label="t(`monitor.targets.kinds.${kind}`)" /></el-select></el-form-item>
          <el-form-item :label="t('monitor.targets.endpoint')"><el-input ref="endpointInput" v-model="form.endpoint" maxlength="512" :placeholder="t('monitor.targets.endpointHint')" data-testid="target-endpoint" /></el-form-item>
          <el-form-item :label="t('monitor.targets.configState')"><el-switch v-model="form.enabled" :active-text="t('monitor.targets.enabled')" :inactive-text="t('monitor.targets.disabled')" :aria-label="t('monitor.targets.configState')" data-testid="target-enabled" /></el-form-item>
        </el-form>
        <p class="target-hint">{{ t('monitor.targets.configHint') }}</p>
        <el-button text type="primary" @click="openNodes">{{ t('monitor.targets.nodeInventory') }}</el-button>
      </div>
      <template #footer>
        <el-button ref="closeButton" :disabled="saving" @click="closeDialog">{{ t('common.close') }}</el-button>
        <el-button v-if="!creating" :disabled="saving || detailLoading" @click="loadDetail">{{ t('monitor.targets.reload') }}</el-button>
        <el-popconfirm v-if="canRemove && formReady" :title="t('monitor.targets.deleteConfirm')" :confirm-button-text="t('common.confirm')" :cancel-button-text="t('common.cancel')" @confirm="remove">
          <template #reference><el-button type="danger" :disabled="saving || conflicted" data-testid="target-delete">{{ t('common.delete') }}</el-button></template>
        </el-popconfirm>
        <el-button v-if="canWrite && formReady" type="primary" :loading="saving" :disabled="conflicted" data-testid="target-save" @click="save">{{ t('common.save') }}</el-button>
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
import { ElMessage } from 'element-plus'
import { openConsoleRoute, StatusAnnouncer, useConsolePageDescriptor } from '@common-ui'
import { useAuthStore } from '../store/auth'
import { monitoringTargetsAPI as api } from '../api/monitoringTargets'
import { nodeResourcesAPI as hostsAPI } from '../api/nodeResources'
import { isTargetUUID, resolveTargetRoute, targetInput, targetKinds } from '../utils/monitoringTargets'
import { navigateMonitorRoute } from '../utils/moduleNavigation'

const { t } = useI18n(), auth = useAuthStore(), route = useRoute(), router = useRouter()
const canRead = computed(() => auth.contextType === 'platform' && auth.authContext?.principal?.type === 'user' && !auth.authContext?.delegation && auth.hasPermission('monitor.monitoring_target.read') && auth.hasPermission('platform.host_node.read'))
const canCreate = computed(() => canRead.value && auth.hasPermission('monitor.monitoring_target.create'))
const creating = computed(() => route.name === 'MonitoringTargetNew')
const targetID = computed(() => typeof route.params.id === 'string' ? route.params.id : '')
const dialogOpen = computed(() => creating.value || Boolean(targetID.value))
const canWrite = computed(() => Boolean(selectedNode.value) && (creating.value ? canCreate.value : canRead.value && auth.hasPermission('monitor.monitoring_target.update')))
const canRemove = computed(() => !creating.value && canRead.value && auth.hasPermission('monitor.monitoring_target.delete'))
const state = computed(() => resolveTargetRoute(route.query, creating.value))
const rows = ref([]), total = ref(0), loading = ref(false), loaded = ref(false), listError = ref('')
const target = ref(null), formReady = ref(false), detailLoading = ref(false), detailError = ref(''), saveError = ref(''), saving = ref(false), conflicted = ref(false), statusMessage = ref('')
const selectedNode = ref(null), nodeOptions = ref([]), nodesLoading = ref(false), nodesError = ref('')
const nodeNames = ref({})
let nodesTicket = 0, nodesPage = 1, nodesTotal = 0, nodeSearch = ''
const selectableNodes = computed(() => selectedNode.value && !nodeOptions.value.some(host => host.node_id === selectedNode.value.node_id) ? [selectedNode.value, ...nodeOptions.value] : nodeOptions.value)
const nodeInput = ref(null), endpointInput = ref(null), closeButton = ref(null), pageHeading = ref(null)
const form = reactive({ node_id: '', monitor_kind: 'host_resources', endpoint: '', enabled: true })
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
function changePage(page) { return navigate(route.path, { ...state.value.query, page: page === 1 ? undefined : String(page) }, 'replace') }
function closeDialog() { if (!saving.value) return navigate('/monitoring-targets', state.value.query, 'replace') }
function openNodes() { return openConsoleRoute('/system/host-nodes') }
function resetForm() { Object.assign(form, { node_id: '', monitor_kind: 'host_resources', endpoint: '', enabled: true }); formReady.value = false; target.value = null; selectedNode.value = null }
function populate(value) {
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
async function loadNodes(search = '', page = 1) {
  if (!canRead.value || !creating.value) return
  const generation = epoch, ticket = ++nodesTicket
  nodesLoading.value = true; nodesError.value = ''
  if (page === 1) nodeOptions.value = []
  try {
    const result = await request(config => hostsAPI.nodes({ page, page_size: 20, ...(search.trim() ? { search: search.trim() } : {}) }, config))
    if (generation !== epoch || ticket !== nodesTicket) return
    if (!Array.isArray(result.data) || !Number.isSafeInteger(result.total) || result.total < 0 || result.data.some(host => !isTargetUUID(host.node_id) || typeof host.display_name !== 'string' || !Array.isArray(host.addresses))) throw new Error('invalid_hosts')
    nodeOptions.value = page === 1 ? result.data : [...nodeOptions.value, ...result.data.filter(host => !nodeOptions.value.some(current => current.node_id === host.node_id))]
    nodesTotal = result.total; nodesPage = page; nodeSearch = search
  } catch { if (generation === epoch && ticket === nodesTicket) nodesError.value = 'monitor.targets.nodeLoadingFailed' }
  finally { if (generation === epoch && ticket === nodesTicket) nodesLoading.value = false }
}
function searchNodes(search) { return loadNodes(search, 1) }
function selectNode(id) { nodesError.value = ''; selectedNode.value = nodeOptions.value.find(host => host.node_id === id) || (selectedNode.value?.node_id === id ? selectedNode.value : null) }
async function loadList() {
  if (!canRead.value) return
  const generation = epoch, ticket = ++listTicket
  loading.value = true; loaded.value = false; rows.value = []; listError.value = ''
  try {
    const value = await request(config => api.list({ page: state.value.page, page_size: state.value.pageSize }, config))
    if (generation !== epoch || ticket !== listTicket) return
    rows.value = value.data; total.value = value.total; loaded.value = true
    const names = {}
    for (let i = 0; i < value.data.length; i += 4) {
      const batch = await Promise.allSettled(value.data.slice(i, i + 4).map(row => readNode(row.subject.node_id)))
      if (generation !== epoch || ticket !== listTicket) return
      batch.forEach((result, index) => { if (result.status === 'fulfilled') names[value.data[i + index].subject.node_id] = result.value.display_name })
    }
    nodeNames.value = names
  } catch { if (generation === epoch && ticket === listTicket) listError.value = t('monitor.targets.loadFailed') }
  finally { if (generation === epoch && ticket === listTicket) loading.value = false }
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
    formReady.value = true; await focusForm()
  } catch (error) { if (generation === epoch && ticket === detailTicket) detailError.value = t(error.response?.status === 404 ? 'monitor.targets.notFound' : 'monitor.targets.loadFailed') }
  finally { if (generation === epoch && ticket === detailTicket) detailLoading.value = false }
}
function writeError(error) {
  conflicted.value = !creating.value && error.response?.data?.error_code === 'resource_version_conflict'
  saveError.value = conflicted.value ? t('monitor.targets.conflict') : error.response?.data?.error || t('monitor.targets.saveFailed')
}
async function save() {
  if (!canWrite.value || !formReady.value || saving.value || conflicted.value) return
  const generation = epoch, id = targetID.value, isCreate = creating.value, body = targetInput(form, target.value)
  if (!body) { saveError.value = t('monitor.targets.invalidForm'); return }
  saving.value = true; saveError.value = ''; statusMessage.value = t('monitor.targets.saving')
  try {
    const value = await request(config => isCreate ? api.create(body, config) : api.update(id, body, config))
    if (generation !== epoch) return
    ElMessage.success(t('monitor.targets.saved'))
    if (isCreate) { const { node_id, ...query } = state.value.query; await navigate(`/monitoring-targets/${value.id}`, query, 'replace') }
    else { const host = selectedNode.value; populate(value); selectedNode.value = host; await loadList() }
  } catch (error) { if (generation === epoch) writeError(error) }
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
  invalidate(); resetForm(); nodesTicket++; nodeOptions.value = []; nodeNames.value = {}; nodesError.value = ''; nodesLoading.value = false; nodesTotal = 0; nodesPage = 1
  rows.value = []; total.value = 0; loaded.value = false; loading.value = false; detailLoading.value = false
  listError.value = ''; detailError.value = ''; saveError.value = ''; statusMessage.value = ''; saving.value = false; conflicted.value = false
  if (!canRead.value) return
  if (state.value.changed) { await navigate(route.path, state.value.query, 'replace'); return }
  if (creating.value) {
    formReady.value = canCreate.value
    const generation = epoch
    await loadNodes()
    if (generation !== epoch) return
    if (state.value.query.node_id) {
      try {
        const host = await readNode(state.value.query.node_id)
        if (generation !== epoch) return
        selectedNode.value = host; form.node_id = host.node_id
      } catch { if (generation === epoch) nodesError.value = 'monitor.targets.nodeLoadingFailed' }
    }
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
.target-hint { color: var(--addp-text-secondary); margin: 12px 0; }
.el-alert { margin-bottom: 12px; }
:deep(.el-dialog__footer) { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: 8px; }
:deep(.el-dialog__footer .el-button + .el-button) { margin-left: 0; }
@media (max-width: 600px) { .monitoring-targets { padding: 12px; } }
</style>
