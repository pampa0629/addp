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
        <el-checkbox v-model="recursive" :disabled="busy || !ready" data-testid="approval-recursive">{{ t('system.engine.dataAuthorization.recursive') }}</el-checkbox>
        <ResourceTree :tree-data="tree" :loading="loading" :title="engine.name" :show-count="false" :show-refresh-button="false"
          :filter-text="query" :filter-method="matchesQuery" @update:filter-text="query = $event"
          :filter-placeholder="t('system.engine.sourceGrants.tableSearch')" :default-expand-root="true" height="min(44vh, 480px)"
          data-testid="approval-resource-tree">
          <template #node="{ data }">
            <el-checkbox :model-value="checked(data)" :indeterminate="mixed(data)" :disabled="busy || !ready || !nodeItems(data).length"
              :aria-label="data.label" :data-testid="data.metadata?.data_item ? 'approval-item-check' : 'approval-node-check'"
              @click.stop @dblclick.stop @change="value => toggleNode(data, value)">{{ data.label }}</el-checkbox>
          </template>
        </ResourceTree>
        <template v-if="workspaceView === 'create'">
          <p class="hint">{{ t('system.engine.dataAuthorization.snapshotOnly') }}</p>
          <el-table v-if="targets.length" :data="targets" data-testid="approval-targets">
            <el-table-column :label="t('system.engine.dataAuthorization.target')" min-width="180"><template #default="{ row }">{{ approvalPathLabel(row) }}</template></el-table-column>
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
    <EngineSourceGrants :engine="engine" :authorization-key="authorizationKey" :authorization-ready="authorizationReady" :data-item-ready="directRequirements.length > 0 && directRequirements.every(row => basisState(row.catalog_path) === 'complete')" :view="workspaceView" :list-mode="workspaceTab === 'history' ? 'history' : 'current'" :requirements="directRequirements" :catalog-adapter="adapter" @busy="grantBusy = $event" @close="closeGrant" @issued="reloadAfterGrant" />
  </section>
</template>

<script setup>
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ResourceTree, serializeEngineCatalogTarget, readNativeApprovalFacts } from '@common-ui'
import { enginesAPI } from '../../api/engines'
import { useAuthStore } from '../../store/auth'
import { approvalPathLabel, approvalTargetsEqual, approvalNodeItems, createApprovalCatalogAdapter } from '../../utils/engineApprovalCatalog'
import EngineSourceGrants from './EngineSourceGrants.vue'

const props = defineProps({ engine: { type: Object, required: true }, authorizationKey: { type: String, required: true }, authorizationReady: Boolean })
const auth = useAuthStore(), { t } = useI18n()
const canGrant = computed(() => auth.hasPermission('system.engine_access_grant.create'))
const canReadGrants = computed(() => auth.hasPermission('system.engine_access_grant.read'))
const workspaceTab = ref('current')
const workspaceView = computed(() => workspaceTab.value === 'create' ? 'create' : 'list')
const currentTab = ref(null)
const canBrowse = computed(() => canReadGrants.value)
const loading = ref(false), ready = ref(false), error = ref(''), selecting = ref(false), grantBusy = ref(false)
const targets = ref([]), generation = ref(0), tree = ref([]), query = ref(''), recursive = ref(false)
const targetNodes = ref(new Map())
const basisStates = ref({}), basisRequests = new Map()
let basisSequence = 0
// jsonb may reorder object keys; UI identity follows ordered path values, not
// their original JSON spelling. Facts/signing validate the target separately.
const basisKey = path => serializeEngineCatalogTarget({ ...path, engine_id: String(path.engine_id),
  segments: path.segments.map(({ term, kind, name }) => ({ term, kind, name })) })
const basisState = path => path?.segments ? basisStates.value[basisKey(path)] || 'pending' : 'pending'
const basisTag = path => ({ complete: 'success', failed: 'danger', incomplete: 'warning', needs_refresh: 'warning', missing: 'warning' }[basisState(path)] || 'info')
const operating = computed(() => selecting.value || grantBusy.value)
const busy = computed(() => !props.authorizationReady || operating.value)
const adapter = computed(() => {
  if (!canBrowse.value || props.engine.lifecycle_state !== 'active' || props.engine.connection_status !== 'online') return null
  try { return createApprovalCatalogAdapter(props.engine, enginesAPI.listAuthorizationItems, enginesAPI.getAuthorizationFacts) } catch { return null }
})
const directRequirementCandidates = computed(() => targets.value.map(path => ({ engine_id: String(props.engine.id), catalog_path: path })))
const directRequirements = ref([])
watch([() => props.authorizationReady, directRequirementCandidates], ([isReady, requirements]) => {
  if (isReady) directRequirements.value = requirements
}, { immediate: true })
const message = e => e?.response?.data?.error || t('system.engine.dataAuthorization.failed')
function changeWorkspace(tab) {
  error.value = ''
  if (tab !== 'create') { query.value = ''; recursive.value = false; targets.value = []; basisStates.value = {}; basisRequests.clear(); targetNodes.value = new Map() }
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
const originalNodes = computed(() => {
  const index = new Map()
  function visit(nodes) { for (const node of nodes) { index.set(node.id, node); visit(node.children || []) } }
  visit(tree.value); return index
})
function nodeItems(node) {
  const original = originalNodes.value.get(node.id)
  if (!original) return []
  return approvalNodeItems(original, recursive.value)
}
const checked = node => { const items = nodeItems(node); return items.length > 0 && items.every(item => targetNodes.value.has(item.id)) }
const mixed = node => { const items = nodeItems(node); return items.some(item => targetNodes.value.has(item.id)) && !checked(node) }
const matchesQuery = (node, keyword) => String(node.metadata?.data_item?.full_name || node.label).toLocaleLowerCase().includes(keyword.trim().toLocaleLowerCase())
async function load() {
  if (busy.value) return
  adapter.value?.invalidate(); const epoch = ++generation.value
  ready.value = false; error.value = ''; loading.value = true; tree.value = []
  try {
    if (!adapter.value) return
    const root = await adapter.value.getTreeRoot()
    if (epoch !== generation.value) return
    tree.value = [root]; ready.value = true
    await inspectTargets(targets.value)
  } catch (e) { if (epoch === generation.value) error.value = message(e) }
  finally { if (epoch === generation.value) loading.value = false }
}
async function toggleNode(node, selected) {
  if (busy.value || !ready.value || !adapter.value) return
  const epoch = generation.value, items = nodeItems(node), next = new Map(targetNodes.value)
  selecting.value = true; error.value = ''
  try {
    if (selected) {
      const additions = items.filter(item => !next.has(item.id))
      if (next.size + additions.length > 200) throw new Error('tooManyTargets')
      for (const item of additions) {
        const path = await adapter.value.resolveTarget(item)
        if (epoch !== generation.value || !props.authorizationReady) return
        next.set(item.id, path)
      }
    } else { for (const item of items) next.delete(item.id) }
    if (epoch !== generation.value) return
    targetNodes.value = next; targets.value = [...next.values()]
    await inspectTargets(targets.value)
  } catch (e) { if (epoch === generation.value) error.value = e?.response?.data?.error || t('system.engine.dataAuthorization.selectionFailed') }
  finally { if (epoch === generation.value) selecting.value = false }
}
function remove(index) {
  if (busy.value) return
  const key = basisKey(targets.value[index])
  basisRequests.delete(key); delete basisStates.value[key]
  targetNodes.value = new Map([...targetNodes.value].filter(([, path]) => !approvalTargetsEqual(path, targets.value[index])))
  targets.value = targets.value.filter((_, i) => i !== index)
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
      basisStates.value[key] = 'checking'
      let state
      try {
        const observed = await readNativeApprovalFacts(path, enginesAPI.getAuthorizationFacts)
        state = observed.state
      }
      catch (e) {
        state = e?.response?.status === 404 && e.response.data?.error_code === 'authorization_target_not_found' ? 'missing'
          : e?.response?.status === 403 ? 'forbidden' : 'failed'
      }
      if (epoch === generation.value && basisRequests.get(key) === sequence && targets.value.some(target => approvalTargetsEqual(target, path))) basisStates.value[key] = state
    }
  }))
}
watch(() => props.authorizationKey, () => {
  basisStates.value = {}; basisRequests.clear()
  generation.value++; targets.value = []; targetNodes.value = new Map(); tree.value = []; query.value = ''; recursive.value = false
  workspaceTab.value = 'current'
  ready.value = loading.value = selecting.value = grantBusy.value = false
  load()
}, { immediate: true })
watch(() => props.authorizationReady, isReady => {
  if (!isReady) {
    basisStates.value = {}; basisRequests.clear()
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
