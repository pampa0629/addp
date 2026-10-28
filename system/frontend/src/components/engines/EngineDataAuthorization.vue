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
      <el-alert v-if="!canRead" type="warning" :closable="false" :title="t('system.engine.dataAuthorization.readRequired')" />
      <el-alert v-else-if="!canBrowse" type="warning" :closable="false" :title="t('system.engine.dataAuthorization.browseRequired')" />
      <el-alert v-else-if="!adapter" type="warning" :closable="false" :title="t('system.engine.dataAuthorization.unsupported')" />
      <template v-else>
        <div class="picker" :inert="busy || !ready || undefined">
          <ResourceTreeSelect :key="`${generation}-${workspaceView}`" v-model="selectionText" :selection="selection" :adapter="adapter" :engine-id="engine.id"
            data-testid="approval-resource-input" search-resources :disabled="busy || !ready" :selectable-filter="isApprovalSelection"
            :placeholder="t('system.engine.sourceGrants.tableSearch')" :tree-label="t('system.engine.sourceGrants.selectFromTree')"
            :title="t('system.engine.dataAuthorization.pickTable')" :error-text="t('system.engine.sourceGrants.treeFilterFailed')"
            @input="selection = null" @select="selectResource" @error="catalogError" />
        </div>
        <template v-if="workspaceView === 'create'">
          <el-button data-testid="approval-add-selection" :loading="selecting" :disabled="busy || !selection || !ready" @click="addSelection">{{ t('system.engine.dataAuthorization.addSelection') }}</el-button>
          <p class="hint">{{ t('system.engine.dataAuthorization.snapshotOnly') }}</p>
          <el-table v-if="targets.length" :data="targets" data-testid="approval-targets">
            <el-table-column :label="t('system.engine.dataAuthorization.target')" min-width="180"><template #default="{ row }">{{ approvalPathLabel(row) }}</template></el-table-column>
            <el-table-column :label="t('system.engine.dataAuthorization.basis.title')" min-width="260"><template #default="{ row }">
              <div class="basis-status" data-testid="approval-basis-status">
                <el-tag :type="basisTag(row)">{{ t(`system.engine.dataAuthorization.basis.${basisState(row)}`) }}</el-tag>
                <el-button data-testid="approval-basis-refresh" link type="primary" :loading="basisState(row) === 'checking'" :disabled="busy || !canReadGrants" @click="inspectTargets([row])">{{ t('system.engine.dataAuthorization.basis.refresh') }}</el-button>
              </div>
            </template></el-table-column>
            <el-table-column :label="t('system.engine.directPermission.title')" min-width="200"><template #default="{ row }">
              <el-tag :type="permit(row)?.state === 'allowed' ? 'success' : 'info'">{{ t(`system.engine.directPermission.states.${permit(row)?.state || 'pending'}`) }}</el-tag>
              <p v-if="permit(row)" class="hint">{{ t(`system.engine.directPermission.sources.${permit(row).source}`) }}</p>
            </template></el-table-column>
            <el-table-column width="90"><template #default="{ $index }"><el-button link type="danger" :disabled="busy" @click="remove($index)">{{ t('system.engine.dataAuthorization.remove') }}</el-button></template></el-table-column>
          </el-table>
          <p v-if="targets.length" class="hint">{{ t('system.engine.dataAuthorization.basis.boundary') }}</p>
          <p v-if="targets.length" class="hint">{{ t('system.engine.directPermission.selectedHint') }}</p>
        </template>
      </template>
    </template>
    <EngineSourceGrants :engine="engine" :authorization-key="authorizationKey" :authorization-ready="authorizationReady" :data-item-ready="directRequirements.length > 0 && directRequirements.every(row => row.state === 'allowed' && basisState(row.catalog_path) === 'complete')" :view="workspaceView" :list-mode="workspaceTab === 'history' ? 'history' : 'current'" :requirements="directRequirements" :catalog-adapter="adapter" @busy="grantBusy = $event" @close="closeGrant" @issued="reloadAfterGrant" />
  </section>
</template>

<script setup>
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ResourceTreeSelect, serializeEngineCatalogTarget, readNativeApprovalFacts } from '@common-ui'
import { enginesAPI } from '../../api/engines'
import { useAuthStore } from '../../store/auth'
import { approvalPathLabel, approvalTargetsEqual, collectApprovalTargets, createApprovalCatalogAdapter, isApprovalSelection } from '../../utils/engineApprovalCatalog'
import { validateDirectPermission } from '../../utils/directPermission'
import EngineSourceGrants from './EngineSourceGrants.vue'

const props = defineProps({ engine: { type: Object, required: true }, authorizationKey: { type: String, required: true }, authorizationReady: Boolean })
const auth = useAuthStore(), { t } = useI18n()
const canRead = computed(() => auth.hasPermission('system.engine_access_direct_permission.read'))
const canGrant = computed(() => auth.hasPermission('system.engine_access_grant.create'))
const canReadGrants = computed(() => auth.hasPermission('system.engine_access_grant.read'))
const workspaceTab = ref('current')
const workspaceView = computed(() => workspaceTab.value === 'create' ? 'create' : 'list')
const currentTab = ref(null)
const canBrowse = computed(() => auth.hasPermission('system.engine_catalog.read'))
const loading = ref(false), ready = ref(false), error = ref(''), selecting = ref(false), grantBusy = ref(false)
const selection = ref(null), targets = ref([]), generation = ref(0), permits = ref({})
const selectionText = ref('')
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
  try { return createApprovalCatalogAdapter(props.engine, enginesAPI.listCatalogChildren) } catch { return null }
})
const permit = path => permits.value[basisKey(path)]
const directRequirementCandidates = computed(() => targets.value.map(path => ({ ...permit(path), engine_id: String(props.engine.id), catalog_path: path })))
const directRequirements = ref([])
watch([() => props.authorizationReady, directRequirementCandidates], ([isReady, requirements]) => {
  if (isReady) directRequirements.value = requirements
}, { immediate: true })
const message = e => e?.response?.data?.error || t('system.engine.dataAuthorization.failed')
function changeWorkspace(tab) {
  error.value = ''
  if (tab !== 'create') { targets.value = []; basisStates.value = {}; basisRequests.clear(); selection.value = null; selectionText.value = ''; permits.value = {} }
}
function selectResource(value) { selection.value = value; selectionText.value = value.display.path || value.display.label }
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
function catalogError(e) { if (!busy.value) error.value = message(e) }
async function load() {
  if (busy.value) return
  ready.value = true; error.value = ''
  loading.value = true
  try { await inspectTargets(targets.value) } finally { loading.value = false }
}
async function addSelection() {
  if (busy.value || !ready.value || !adapter.value) return
  const epoch = generation.value
  selecting.value = true; error.value = ''
  try {
    const snapshot = await collectApprovalTargets(selection.value, props.engine.id, adapter.value)
    if (epoch !== generation.value) return
    const added = snapshot.filter(path => !targets.value.some(prior => approvalTargetsEqual(path, prior)))
    if (!snapshot.length || targets.value.length + added.length > 200) throw new Error('invalidSelection')
    targets.value = [...targets.value, ...added]
    await inspectTargets(added)
  } catch (e) { if (epoch === generation.value) error.value = e?.response?.data?.error || t('system.engine.dataAuthorization.selectionFailed') }
  finally { if (epoch === generation.value) selecting.value = false }
}
function remove(index) {
  if (busy.value) return
  const key = basisKey(targets.value[index])
  basisRequests.delete(key); delete basisStates.value[key]; delete permits.value[key]
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
        if (observed.binding && canRead.value) {
          const row = validateDirectPermission(await enginesAPI.getDirectPermission(props.engine.id, observed.binding.item_fingerprint), props.engine.id, observed.binding.item_fingerprint)
          if (epoch === generation.value && basisRequests.get(key) === sequence) permits.value[key] = row
        }
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
  generation.value++; targets.value = []; permits.value = {}; selection.value = null; selectionText.value = ''
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
