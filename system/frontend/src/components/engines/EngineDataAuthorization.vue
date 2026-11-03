<template>
  <section class="data-authorization" data-testid="engine-data-authorization">
    <div class="workspace-toolbar" data-testid="authorization-workspace-toolbar">
      <el-radio-group v-model="workspaceTab" data-testid="authorization-workspace-tabs" :disabled="busy" @change="changeWorkspace">
        <el-radio-button v-if="canGrant" value="create" data-testid="authorization-new">{{ t('system.engine.dataAuthorization.newAuthorization') }}</el-radio-button>
        <el-radio-button ref="currentTab" value="current" :disabled="!canReadGrants">{{ t('system.engine.sourceGrants.current') }}</el-radio-button>
        <el-radio-button value="history" :disabled="!canReadGrants">{{ t('system.engine.sourceGrants.history') }}</el-radio-button>
      </el-radio-group>
    </div>
    <el-alert v-if="workspaceView === 'create'" type="info" :closable="false" show-icon :title="t('system.engine.dataAuthorization.boundary')" />
    <el-alert v-if="error" type="error" :closable="false" :title="error" />
    <template v-if="workspaceView !== 'list'">
      <div class="section-heading"><h3>{{ t('system.engine.dataAuthorization.selectData') }}</h3><el-button :loading="loading" :disabled="busy" @click="load">{{ t('common.refresh') }}</el-button></div>
      <el-alert v-if="!canBrowse" type="warning" :closable="false" :title="t('system.engine.dataAuthorization.browseRequired')" />
      <el-alert v-else-if="!adapter" type="warning" :closable="false" :title="t('system.engine.dataAuthorization.unsupported')" />
      <template v-else>
        <ResourceTreePicker ref="picker" :key="authorizationKey" :model-value="selections" multiple :branch-selection="!['postgresql', 'nfs', 'minio'].includes(engine.engine_type)"
          :engine-id="engine.id" :adapter="adapter" mode="any" :selectable-filter="isSelectableTarget"
          :max-selections="200" :validate-selection="validateSelection" :disabled="!authorizationReady || grantBusy"
          :show-engine-selector="false" :show-selection-summary="false" :show-count="false" :title="engine.name"
          :search-placeholder="t('system.engine.sourceGrants.tableSearch')" tree-height="min(44vh, 480px)"
          data-testid="approval-resource-tree" @update:model-value="applySelection" @busy="selecting = $event"
          @loading="loading = $event" @ready="selectionReady" @error="selectionError" />
        <template v-if="workspaceView === 'create'">
          <p class="hint">{{ t(['postgresql', 'nfs', 'minio'].includes(engine.engine_type) ? 'system.engine.dataAuthorization.mixedSelection' : 'system.engine.dataAuthorization.snapshotOnly') }}</p>
          <el-table v-if="targets.length" :data="targets" data-testid="approval-targets">
            <el-table-column :label="t('system.engine.dataAuthorization.target')" min-width="180"><template #default="{ row }">{{ approvalPathLabel(row) || engine.name }}</template></el-table-column>
            <el-table-column :label="t('system.engine.dataAuthorization.scope')" min-width="230"><template #default="{ row }">
              <template v-if="isNodePath(row)">
                <el-tag>{{ t('system.engine.dataAuthorization.nodeTarget') }}</el-tag>
                <el-checkbox v-model="nodeRecursions[basisKey(row)]" :disabled="busy" data-testid="authorization-node-recursive">{{ t('system.engine.dataAuthorization.nodeRecursive') }}</el-checkbox>
              </template>
              <el-tag v-else type="info">{{ t('system.engine.dataAuthorization.itemTarget') }}</el-tag>
              <p v-if="isOverlapping(row)" class="hint" data-testid="authorization-scope-overlap">{{ t('system.engine.dataAuthorization.overlap') }}</p>
            </template></el-table-column>
            <el-table-column :label="t('system.engine.dataAuthorization.basis.title')" min-width="260"><template #default="{ row }">
              <div class="basis-status" data-testid="approval-basis-status">
                <el-tag :type="basisTag(row)">{{ t(`system.engine.dataAuthorization.basis.${basisState(row)}`) }}</el-tag>
                <el-button data-testid="approval-basis-refresh" link type="primary" :loading="basisState(row) === 'checking'" :disabled="busy || !canReadGrants" @click="inspectTargets([row])">{{ t('system.engine.dataAuthorization.basis.refresh') }}</el-button>
              </div>
            </template></el-table-column>
            <el-table-column width="90"><template #default="{ $index }"><el-button link type="danger" :disabled="busy" @click="remove($index)">{{ t('system.engine.dataAuthorization.remove') }}</el-button></template></el-table-column>
          </el-table>
          <p v-if="targets.length" class="hint">{{ t('system.engine.dataAuthorization.basis.boundary') }}</p>
        </template>
      </template>
    </template>
    <EngineSourceGrants :engine="engine" :authorization-key="authorizationKey" :authorization-ready="authorizationReady" :target-ready="directRequirements.length > 0 && directRequirements.every(row => ['complete', 'node'].includes(basisState(row.catalog_path)))" :view="workspaceView" :list-mode="workspaceTab === 'history' ? 'history' : 'current'" :requirements="directRequirements" :catalog-adapter="itemAdapter" @busy="grantBusy = $event" @close="closeGrant" @issued="reloadAfterGrant" />
  </section>
</template>

<script setup>
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ResourceTreePicker, serializeEngineCatalogTarget, readNativeApprovalFacts } from '@common-ui'
import { enginesAPI } from '../../api/engines'
import { useAuthStore } from '../../store/auth'
import { approvalPathLabel, approvalTargetsEqual, approvalTargetFromSelection, isApprovalTarget, isApprovalNode, approvalScopeContains, createApprovalCatalogAdapter, createMixedApprovalCatalogAdapter } from '../../utils/engineApprovalCatalog'
import EngineSourceGrants from './EngineSourceGrants.vue'

const props = defineProps({ engine: { type: Object, required: true }, authorizationKey: { type: String, required: true }, authorizationReady: Boolean })
const auth = useAuthStore(), { t } = useI18n()
const canGrant = computed(() => auth.hasPermission('system.engine_access_grant.create'))
const canReadGrants = computed(() => auth.hasPermission('system.engine_access_grant.read'))
const workspaceTab = ref('current'), nodeRecursions = ref({})
const workspaceView = computed(() => workspaceTab.value === 'create' ? 'create' : 'list')
const currentTab = ref(null)
const canBrowse = computed(() => canReadGrants.value)
const loading = ref(false), ready = ref(false), error = ref(''), selecting = ref(false), grantBusy = ref(false)
const targets = ref([]), generation = ref(0), selections = ref([]), picker = ref(null)
// Domain cache contains only independently resolved signing boundaries.
const targetPaths = new Map()
const basisStates = ref({}), basisBindings = ref({}), basisRequests = new Map()
let basisSequence = 0
// jsonb may reorder object keys; UI identity follows ordered path values, not
// their original JSON spelling. Facts/signing validate the target separately.
const basisKey = path => serializeEngineCatalogTarget({ ...path, engine_id: String(path.engine_id),
  segments: path.segments.map(({ term, kind, name }) => ({ term, kind, name })) })
const basisState = path => path?.segments ? basisStates.value[basisKey(path)] || 'pending' : 'pending'
const basisTag = path => ({ complete: 'success', failed: 'danger', incomplete: 'warning', needs_refresh: 'warning', missing: 'warning' }[basisState(path)] || 'info')
const operating = computed(() => selecting.value || grantBusy.value)
const busy = computed(() => !props.authorizationReady || operating.value)
// Health polling replaces the engine projection every ten seconds. Keep
// navigation stable unless one of its actual inputs changes; authorization
// identity and availability continue to be checked by the session boundary.
const catalogEngineKey = computed(() => JSON.stringify([props.engine.id, props.engine.name, props.engine.engine_type, props.engine.lifecycle_state, props.engine.connection_status]))
const catalogEngine = computed(() => {
  const [id, name, engine_type, lifecycle_state, connection_status] = JSON.parse(catalogEngineKey.value)
  return { id, name, engine_type, lifecycle_state, connection_status }
})
const itemAdapter = computed(() => {
  if (!canBrowse.value || catalogEngine.value.lifecycle_state !== 'active' || catalogEngine.value.connection_status !== 'online') return null
  try { return createApprovalCatalogAdapter(catalogEngine.value, enginesAPI.listAuthorizationItems, enginesAPI.getAuthorizationFacts) } catch { return null }
})
const adapter = computed(() => {
  if (!itemAdapter.value) return null
  return createMixedApprovalCatalogAdapter(catalogEngine.value, enginesAPI.listAuthorizationItems, enginesAPI.getAuthorizationFacts, enginesAPI.listAuthorizationNodes)
})
const isSelectableTarget = node => isApprovalTarget(node) || (isApprovalNode(node) && !(node.metadata.node_identity.target.segments[0].term === 'service' && node.metadata.node_identity.target.segments.length === 1))
const isNodePath = path => selections.value.some(selection => isApprovalNode(selection.raw?.node) && approvalTargetsEqual(targetPaths.get(selection.identity.locator), path))
const isOverlapping = path => targets.value.some(other => !approvalTargetsEqual(path, other) && (
  (isNodePath(other) && approvalScopeContains(other, path, !!nodeRecursions.value[basisKey(other)], isNodePath(path))) ||
  (isNodePath(path) && approvalScopeContains(path, other, !!nodeRecursions.value[basisKey(path)], isNodePath(other)))
))
const directRequirementCandidates = computed(() => targets.value.map(path => ({ engine_id: String(props.engine.id), engine_name: props.engine.name, catalog_path: path, metadata_only: !!basisBindings.value[basisKey(path)]?.metadata_target, ...(isNodePath(path) ? { node_recursive: !!nodeRecursions.value[basisKey(path)] } : {}) })))
const directRequirements = ref([])
watch([() => props.authorizationReady, directRequirementCandidates], ([isReady, requirements]) => {
  if (isReady) directRequirements.value = requirements
}, { immediate: true })
function changeWorkspace(tab) {
  error.value = ''
  if (tab !== 'create') { nodeRecursions.value = {}; generation.value++; selections.value = []; targets.value = []; basisStates.value = {}; basisBindings.value = {}; basisRequests.clear(); targetPaths.clear() }
}
function closeGrant() {
  if (busy.value) return
  workspaceTab.value = 'current'; changeWorkspace('current')
  nextTick(() => currentTab.value?.$el?.querySelector('input')?.focus())
}
async function reloadAfterGrant() {
  // The child has finished its immutable batch before returning to the list.
  grantBusy.value = false
  closeGrant(); await nextTick(); load()
}
function selectionError(e) {
  error.value = e?.response?.data?.error || t('system.engine.dataAuthorization.selectionFailed')
}
function selectionReady(value) {
  ready.value = value
  if (value) inspectTargets(targets.value)
}
async function load() {
  if (busy.value) return
  generation.value++; ready.value = false; error.value = ''
  adapter.value?.invalidate()
  await picker.value?.reload()
}
async function validateSelection(next) {
  const epoch = generation.value, resolved = new Map()
  error.value = ''
  for (const selection of next) {
    const key = selection.identity.locator
    // Retained selections keep their exact boundary; a refresh is checked separately.
    const path = targetPaths.get(key) || (isApprovalNode(selection.raw?.node) ? await adapter.value.resolveTarget(selection.raw.node) : await approvalTargetFromSelection(selection, props.engine.id, adapter.value))
    if (epoch !== generation.value || !props.authorizationReady) throw new Error('selectionContextChanged')
    resolved.set(key, path)
  }
  targetPaths.clear()
  for (const [key, path] of resolved) targetPaths.set(key, path)
}
function applySelection(next) {
  const retained = new Set(next.map(selection => targetPaths.get(selection.identity.locator)).filter(Boolean).map(basisKey))
  nodeRecursions.value = Object.fromEntries(Object.entries(nodeRecursions.value).filter(([key]) => retained.has(key)))
  selections.value = next
  targets.value = next.map(selection => targetPaths.get(selection.identity.locator)).filter(Boolean)
  inspectTargets(targets.value)
}
function remove(index) {
  if (busy.value) return
  const path = targets.value[index], key = basisKey(path)
  const selection = selections.value.find(item => approvalTargetsEqual(targetPaths.get(item.identity.locator), path))
  if (!selection) return
  basisRequests.delete(key); delete basisStates.value[key]; delete basisBindings.value[key]; delete nodeRecursions.value[key]
  picker.value?.removeSelection(selection.identity.locator)
}
async function inspectTargets(paths) {
  if (!props.authorizationReady) return
  const epoch = generation.value, queue = [...paths]
  await Promise.all(Array.from({ length: Math.min(4, queue.length) }, async () => {
    while (queue.length && epoch === generation.value && props.authorizationReady) {
      const path = queue.shift(), key = basisKey(path), sequence = ++basisSequence
      if (!targets.value.some(target => approvalTargetsEqual(target, path))) continue
      basisRequests.set(key, sequence)
      if (!canReadGrants.value) { basisStates.value[key] = 'forbidden'; continue }
      basisStates.value[key] = 'checking'; delete basisBindings.value[key]
      let state, binding = null
      try {
        if (isNodePath(path)) {
          const observed = await enginesAPI.listAuthorizationNodes(props.engine.id, { target: path, page: 1, page_size: 100 })
          state = approvalTargetsEqual(observed?.parent?.target, path) && observed.parent.exists === true ? 'node' : 'missing'
        } else {
          const observed = await readNativeApprovalFacts(path, enginesAPI.getAuthorizationFacts)
          state = observed.state; binding = observed.binding
        }
      }
      catch (e) {
        state = e?.response?.status === 404 && e.response.data?.error_code === 'authorization_target_not_found' ? 'missing'
          : e?.response?.status === 403 ? 'forbidden' : 'failed'
      }
      if (epoch === generation.value && basisRequests.get(key) === sequence && targets.value.some(target => approvalTargetsEqual(target, path))) { basisStates.value[key] = state; if (binding) basisBindings.value[key] = binding }
    }
  }))
}
watch(() => props.authorizationKey, () => {
  basisStates.value = {}; basisBindings.value = {}; basisRequests.clear()
  generation.value++; targets.value = []; selections.value = []; targetPaths.clear()
  workspaceTab.value = 'current'; nodeRecursions.value = {}
  ready.value = loading.value = selecting.value = grantBusy.value = false
  load()
}, { immediate: true })
watch(() => props.authorizationReady, isReady => {
  if (!isReady) {
    basisStates.value = {}; basisBindings.value = {}; basisRequests.clear()
    generation.value++
    loading.value = selecting.value = grantBusy.value = false
    ready.value = false
  } else {
    if (!ready.value) load()
  }
})
onBeforeUnmount(() => { generation.value++ })
</script>

<style scoped>
.data-authorization { display: grid; gap: 16px; min-width: 0; }
.section-heading { display: flex; align-items: center; justify-content: space-between; gap: 16px; }
.workspace-toolbar { position: sticky; top: 0; z-index: 2; align-self: start; display: flex; flex-wrap: wrap; align-items: center; gap: 12px; padding: 8px 0; background: var(--addp-bg-primary); }
.workspace-toolbar :deep(.el-radio-group) { display: flex; flex-wrap: wrap; max-width: 100%; }
.workspace-toolbar :deep(.el-radio-button__inner) { white-space: normal; }
.el-select { width: 100%; }
.hint { color: var(--addp-text-color-secondary); margin: 0; overflow-wrap: anywhere; }
.basis-status { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; }
.basis-status :deep(.el-tag) { height: auto; white-space: normal; }
</style>
