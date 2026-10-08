<template>
  <section v-if="canRead" class="host-nodes" data-testid="host-nodes">
    <header class="node-toolbar">
      <h2>{{ t('system.hostNodes.title') }}</h2>
      <el-button v-if="canCreate" type="primary" data-testid="node-create" @click="openCreate">{{ t('system.hostNodes.create') }}</el-button>
    </header>
    <form class="node-toolbar" @submit.prevent="search">
      <el-input ref="searchInput" v-model="searchDraft" :placeholder="t('system.hostNodes.search')" :aria-label="t('system.hostNodes.search')" clearable maxlength="255" />
      <el-button native-type="submit">{{ t('common.search') }}</el-button>
      <el-button :loading="listLoading" @click="loadList">{{ t('system.module.refresh') }}</el-button>
    </form>
    <el-alert v-if="listError" :title="listError" type="error" :closable="false" show-icon />
    <el-table v-loading="listLoading" :data="rows" stripe border class="node-table">
      <el-table-column prop="display_name" :label="t('system.hostNodes.name')" min-width="160" show-overflow-tooltip />
      <el-table-column prop="node_id" :label="t('system.hostNodes.id')" min-width="310" show-overflow-tooltip />
      <el-table-column :label="t('system.hostNodes.kind')" width="130">
        <template #default="{ row }">{{ t(`system.hostNodes.kinds.${row.node_kind}`) }}</template>
      </el-table-column>
      <el-table-column :label="t('system.hostNodes.addresses')" min-width="200" show-overflow-tooltip>
        <template #default="{ row }">{{ row.addresses.join(', ') || '—' }}</template>
      </el-table-column>
      <el-table-column :label="t('system.hostNodes.state')" width="110">
        <template #default="{ row }"><el-tag :type="row.enabled ? 'success' : 'info'">{{ t(row.enabled ? 'system.hostNodes.enabled' : 'system.hostNodes.disabled') }}</el-tag></template>
      </el-table-column>
      <el-table-column :label="t('system.hostNodes.bindings')" min-width="170" show-overflow-tooltip>
        <template #default="{ row }">{{ row.allowed_module_bindings.map(item => item.module_name).join(', ') || '—' }}</template>
      </el-table-column>
      <el-table-column :label="t('system.hostNodes.details')" width="100" fixed="right">
        <template #default="{ row }"><el-button text type="primary" @click="event => openNode(row.node_id, event)">{{ t('system.hostNodes.details') }}</el-button></template>
      </el-table-column>
      <template #empty><el-empty :description="t(listLoaded ? 'system.hostNodes.empty' : 'system.hostNodes.notLoaded')" :image-size="72" /></template>
    </el-table>
    <el-pagination v-if="listLoaded" :current-page="state.page" :page-size="state.pageSize" :total="total" :page-sizes="[10, 20, 50, 100]"
      layout="total, sizes, prev, pager, next" class="node-pagination" @current-change="changePage" @size-change="changeSize" />

    <el-dialog :model-value="dialogOpen" :title="t(creating ? 'system.hostNodes.create' : 'system.hostNodes.details')" class="addp-dialog"
      width="min(720px, calc(100vw - 24px))" :close-on-click-modal="!saving" :close-on-press-escape="!saving" :show-close="!saving"
      @update:model-value="value => !value && closeDialog()" @open-auto-focus="focusForm" @opened="focusForm" @closed="restoreFocus">
      <div v-loading="detailLoading" data-testid="node-detail">
        <el-alert v-if="detailError" :title="detailError" type="error" :closable="false" show-icon />
        <el-alert v-if="saveError" :title="saveError" type="error" :closable="false" show-icon data-testid="node-save-error" />
        <el-form v-if="formReady" ref="formRef" :model="form" :rules="rules" label-position="top" :disabled="!canWrite || saving" @submit.prevent>
          <el-form-item v-if="!creating" :label="t('system.hostNodes.id')"><el-input :model-value="node.node_id" readonly data-testid="node-id" /></el-form-item>
          <el-form-item :label="t('system.hostNodes.name')" prop="display_name">
            <el-input ref="nameInput" v-model="form.display_name" maxlength="255" data-testid="node-name" />
          </el-form-item>
          <el-form-item :label="t('system.hostNodes.kind')" prop="node_kind">
            <el-select v-model="form.node_kind" :aria-label="t('system.hostNodes.kind')">
              <el-option v-for="kind in ['physical', 'virtual']" :key="kind" :value="kind" :label="t(`system.hostNodes.kinds.${kind}`)" />
            </el-select>
          </el-form-item>
          <el-form-item :label="t('system.hostNodes.addresses')">
            <el-input v-model="form.addressText" type="textarea" :rows="3" :placeholder="t('system.hostNodes.addressHint')" data-testid="node-addresses" />
          </el-form-item>
          <el-form-item :label="t('system.hostNodes.state')"><el-switch v-model="form.enabled" :active-text="t('system.hostNodes.enabled')" :inactive-text="t('system.hostNodes.disabled')" :aria-label="t('system.hostNodes.state')" data-testid="node-enabled" /></el-form-item>
          <el-collapse v-model="advancedSections">
            <el-collapse-item name="bindings" :title="t('system.hostNodes.advanced')">
              <el-alert v-if="moduleError" :title="t(moduleError)" type="warning" :closable="false" />
              <el-form-item :label="t('system.hostNodes.bindings')">
                <div class="node-bindings">
                  <p class="node-hint">{{ t('system.hostNodes.bindingHint') }}</p>
                  <div v-for="(binding, index) in form.bindings" :key="index" class="node-binding-row">
                    <el-select v-if="canReadModules" v-model="binding.module_name" filterable :disabled="!modulesLoaded" :loading="modulesLoading" :aria-label="t('system.hostNodes.moduleAt', { index: index + 1 })">
                      <el-option v-for="module in bindingCandidates(binding)" :key="module.module_name" :value="module.module_name" :label="module.module_name" />
                    </el-select>
                    <el-input v-else :model-value="binding.module_name" readonly />

                    <el-button v-if="canWrite && canReadModules && modulesLoaded" :aria-label="t('system.hostNodes.removeAt', { index: index + 1 })" @click="form.bindings.splice(index, 1)">{{ t('system.hostNodes.remove') }}</el-button>
                  </div>
                  <el-button v-if="canWrite && canReadModules && modulesLoaded" :disabled="form.bindings.length >= 64" data-testid="node-add-binding" @click="form.bindings.push({ module_name: '' })">{{ t('system.hostNodes.addBinding') }}</el-button>
                </div>
              </el-form-item>
            </el-collapse-item>
          </el-collapse>
          <p v-if="!creating" class="node-hint">{{ t('system.hostNodes.version', { version: node.version }) }}</p>
        </el-form>
      </div>
      <template #footer>
        <el-button v-if="!creating && node && canMonitor" @click="openConsoleRoute(`/monitor/node-resources/${node.node_id}`)">{{ t('system.hostNodes.monitor') }}</el-button>
        <el-button ref="closeButton" :disabled="saving" @click="closeDialog">{{ t('common.close') }}</el-button>
        <el-button v-if="!creating" :disabled="saving || detailLoading" @click="loadDetail">{{ t('system.hostNodes.reload') }}</el-button>
        <el-button v-if="canWrite && formReady" type="primary" :loading="saving" data-testid="node-save" @click="save">{{ t('common.save') }}</el-button>
      </template>
    </el-dialog>
    <StatusAnnouncer :message="statusMessage" />
  </section>
</template>

<script setup>
import { computed, nextTick, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { StatusAnnouncer, useConsolePageDescriptor, openConsoleRoute } from '@common-ui'
import { useAuthStore } from '../store/auth'
import { modulesAPI } from '../api/modules'
import { hostNodesAPI } from '../api/hostNodes'
import { navigateSystemRoute } from '../utils/moduleNavigation'
import { resolveHostNodesRouteState } from '../utils/routeState'

const { t } = useI18n()
const auth = useAuthStore(), route = useRoute(), router = useRouter()
const canRead = computed(() => auth.contextType === 'platform' && auth.hasPermission('platform.host_node.read'))
const canCreate = computed(() => canRead.value && auth.hasPermission('platform.host_node.create'))
const canUpdate = computed(() => canRead.value && auth.hasPermission('platform.host_node.update'))
const canWrite = computed(() => creating.value ? canCreate.value : canUpdate.value)
const canReadModules = computed(() => canRead.value && auth.hasPermission('platform.module.read'))
const canMonitor = computed(() => canRead.value && auth.hasPermission('monitor.resource_observation.read'))
const advancedSections = ref([]), moduleCandidates = ref([]), modulesLoaded = ref(false), modulesLoading = ref(false), moduleError = ref('')
const state = computed(() => resolveHostNodesRouteState(route.query))
const nodeID = computed(() => typeof route.params.node_id === 'string' ? route.params.node_id : '')
const rows = ref([]), total = ref(0), searchDraft = ref(''), listLoaded = ref(false), listLoading = ref(false), listError = ref('')
const node = ref(null), creating = ref(false), formReady = ref(false), detailLoading = ref(false), detailError = ref(''), saveError = ref(''), saving = ref(false)
const formRef = ref(null), nameInput = ref(null), closeButton = ref(null), searchInput = ref(null), statusMessage = ref('')
const form = reactive({ display_name: '', node_kind: 'physical', addressText: '', enabled: true, bindings: [] })
const dialogOpen = computed(() => creating.value || Boolean(nodeID.value))
let generation = 0, listRequest = 0, detailRequest = 0
let dialogTrigger = null
const identity = computed(() => JSON.stringify([auth.authContext?.principal?.id, auth.authContext?.context, auth.permissions]))
const rules = computed(() => ({ display_name: [{ validator: (_rule, value, callback) => {
  const name = value.trim()
  callback(!name || [...name].length > 255 || /[\u0000-\u001f\u007f]/u.test(name) ? new Error(t('system.hostNodes.invalidName')) : undefined)
}, trigger: 'blur' }] }))
useConsolePageDescriptor(router, 'system', {
  title: computed(() => t('system.hostNodes.title')), subject: computed(() => node.value?.display_name || ''),
  ready: computed(() => Boolean(node.value) && !creating.value)
})

function bindingCandidates(binding) {
  return binding.module_name && !moduleCandidates.value.some(module => module.module_name === binding.module_name) ? [{ module_name: binding.module_name }, ...moduleCandidates.value] : moduleCandidates.value
}
async function loadModuleCandidates() {
  if (!canReadModules.value) { moduleError.value = 'system.hostNodes.modulesDenied'; return }
  if (modulesLoaded.value || modulesLoading.value) return
  const epoch = generation
  modulesLoading.value = true; moduleError.value = ''
  try {
    const response = await modulesAPI.list()
    if (epoch !== generation) return
    if (!Array.isArray(response.modules) || response.modules.some(module => typeof module.module_name !== 'string' || !/^[a-z][a-z0-9_-]{0,49}$/u.test(module.module_name))) throw new Error('invalid_modules')
    moduleCandidates.value = response.modules; modulesLoaded.value = true
  } catch { if (epoch === generation) moduleError.value = 'system.hostNodes.modulesFailed' }
  finally { if (epoch === generation) modulesLoading.value = false }
}
watch(advancedSections, sections => { if (sections.includes('bindings')) loadModuleCandidates() })
function populate(value) {
  node.value = value
  Object.assign(form, { display_name: value.display_name, node_kind: value.node_kind, addressText: value.addresses.join('\n'), enabled: value.enabled,
    bindings: value.allowed_module_bindings.map(binding => ({ module_name: binding.module_name })) })
  formReady.value = true
  formRef.value?.clearValidate()
}
async function focusForm(event) {
  event?.preventDefault?.()
  await nextTick()
  if (canWrite.value && formReady.value) nameInput.value?.focus()
  else closeButton.value?.$el?.focus()
}
async function restoreFocus() {
  await nextTick()
  if (!canRead.value || dialogOpen.value) return
  if (dialogTrigger?.isConnected) dialogTrigger.focus()
  else searchInput.value?.focus()
  dialogTrigger = null
}
async function loadList() {
  if (!canRead.value) return
  const ticket = ++listRequest, epoch = generation
  listLoading.value = true; listLoaded.value = false; listError.value = ''; rows.value = []
  try {
    const value = await hostNodesAPI.list({ page: state.value.page, page_size: state.value.pageSize, ...(state.value.search ? { search: state.value.search } : {}) })
    if (ticket !== listRequest || epoch !== generation) return
    rows.value = value.data; total.value = value.total; listLoaded.value = true
  } catch { if (ticket === listRequest && epoch === generation) listError.value = t('system.hostNodes.loadFailed') }
  finally { if (ticket === listRequest && epoch === generation) listLoading.value = false }
}
async function loadDetail() {
  if (!canRead.value || !nodeID.value) return
  const id = nodeID.value, ticket = ++detailRequest, epoch = generation
  node.value = null; formReady.value = false; detailError.value = ''; saveError.value = ''; detailLoading.value = false
  if (!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/u.test(id) || /^0{8}-0{4}-0{4}-0{4}-0{12}$/u.test(id)) {
    detailError.value = t('system.hostNodes.invalidID'); return
  }
  detailLoading.value = true
  try {
    const value = await hostNodesAPI.get(id)
    if (ticket !== detailRequest || epoch !== generation) return
    populate(value)
    await nextTick()
    if (canWrite.value) nameInput.value?.focus()
  } catch (error) {
    if (ticket === detailRequest && epoch === generation) detailError.value = t(error.response?.status === 404 ? 'system.hostNodes.notFound' : 'system.hostNodes.loadFailed')
  } finally { if (ticket === detailRequest && epoch === generation) detailLoading.value = false }
}
function navigate(path, query = state.value.query, history = 'push') {
  return navigateSystemRoute(router, { path, query }, { history })
}
function openNode(id, event) {
  dialogTrigger = event?.currentTarget || null
  return navigate(`/host-nodes/${encodeURIComponent(id)}`)
}
function openCreate(event) {
  if (!canCreate.value) return
  dialogTrigger = event?.currentTarget || null
  node.value = null; saveError.value = ''; detailError.value = ''; statusMessage.value = ''
  Object.assign(form, { display_name: '', node_kind: 'physical', addressText: '', enabled: true, bindings: [] })
  formReady.value = true; creating.value = true
  formRef.value?.clearValidate()
}
function closeDialog() {
  if (saving.value) return
  creating.value = false
  if (nodeID.value) return navigate('/host-nodes', state.value.query, 'replace')
}
function search() { return navigate('/host-nodes', { ...state.value.query, search: searchDraft.value.trim(), page: undefined }, 'replace') }
function changePage(page) { return navigate('/host-nodes', resolveHostNodesRouteState({ ...state.value.query, page: String(page) }).query, 'replace') }
function changeSize(size) { return navigate('/host-nodes', resolveHostNodesRouteState({ ...state.value.query, page: '1', page_size: String(size) }).query, 'replace') }
async function save() {
  if (!canWrite.value || !formReady.value || saving.value) return
  if (!await formRef.value.validate().catch(() => false)) return
  const addresses = form.addressText.split('\n').map(value => value.trim()).filter(Boolean)
  const modules = form.bindings.map(binding => binding.module_name.trim())
  if (addresses.length > 32 || modules.length > 64 || modules.some(name => !/^[a-z][a-z0-9_-]{0,49}$/u.test(name)) || new Set(modules).size !== modules.length) {
    saveError.value = t('system.hostNodes.invalidEntries'); return
  }
  const body = { display_name: form.display_name.trim(), node_kind: form.node_kind, addresses, enabled: form.enabled,
    allowed_module_bindings: modules.map(module_name => ({ module_name, client_id: `addp-${module_name}` })) }
  const epoch = generation, isCreate = creating.value, id = nodeID.value
  if (!isCreate) body.version = node.value.version
  saving.value = true; saveError.value = ''; statusMessage.value = t('system.hostNodes.saving')
  try {
    const value = isCreate ? await hostNodesAPI.create(body) : await hostNodesAPI.update(id, body)
    if (epoch !== generation) return
    statusMessage.value = ''
    ElMessage.success(t('system.hostNodes.saved'))
    if (isCreate) { creating.value = false; await navigate(`/host-nodes/${value.node_id}`, state.value.query, 'replace') }
    else { populate(value); await loadList() }
  } catch (error) {
    if (epoch !== generation) return
    statusMessage.value = ''
    saveError.value = t(error.response?.status === 409 ? 'system.hostNodes.conflict' : error.response?.status === 400 ? 'system.hostNodes.invalidEntries' : 'system.hostNodes.saveFailed')
  } finally { if (epoch === generation) saving.value = false }
}
watch([() => route.fullPath, identity], async () => {
  generation++; advancedSections.value = []; moduleCandidates.value = []; modulesLoaded.value = false; modulesLoading.value = false; moduleError.value = ''; creating.value = false; saving.value = false; node.value = null; formReady.value = false
  detailLoading.value = false; listLoading.value = false; detailError.value = ''; saveError.value = ''; statusMessage.value = ''
  rows.value = []; listLoaded.value = false; searchDraft.value = state.value.search
  if (!canRead.value) return
  if (state.value.changed) { await navigate(route.path, state.value.query, 'replace'); return }
  await Promise.all([loadList(), loadDetail()])
}, { immediate: true })
onBeforeUnmount(() => { generation++ })
</script>

<style scoped>
.host-nodes { padding: 20px; min-width: 0; }
.node-toolbar { display: flex; align-items: center; gap: 12px; margin-bottom: 16px; }
.node-toolbar h2 { flex: 1; margin: 0; }
.node-toolbar .el-input { max-width: 420px; }
.node-table { margin-top: 16px; }
.node-pagination { margin-top: 16px; overflow-x: auto; }
.node-bindings { width: 100%; }
.node-binding-row { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1fr) auto; gap: 8px; align-items: center; margin-bottom: 8px; }
.node-client { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.node-hint { color: var(--addp-text-secondary); margin: 8px 0; }
@media (max-width: 600px) {
  .host-nodes { padding: 12px; }
  .node-toolbar { flex-wrap: wrap; }
  .node-toolbar .el-input { max-width: none; flex-basis: 100%; }
  .node-binding-row { grid-template-columns: minmax(0, 1fr) auto; }
  .node-client { grid-column: 1; grid-row: 2; }
}
</style>
