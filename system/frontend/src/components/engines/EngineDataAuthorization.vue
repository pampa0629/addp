<template>
  <section class="data-authorization" data-testid="engine-data-authorization">
    <el-alert type="info" :closable="false" show-icon :title="t('system.engine.dataAuthorization.boundary')" />
    <el-alert v-if="error" type="error" :closable="false" :title="error" />
    <el-alert v-if="changed" type="success" :closable="false" :title="t('system.engine.dataAuthorization.changed')" />
    <div class="section-heading"><h3>{{ t('system.engine.dataAuthorization.selectData') }}</h3><el-button :loading="loading" :disabled="busy" @click="load">{{ t('common.refresh') }}</el-button></div>
    <el-alert v-if="!canRead" type="warning" :closable="false" :title="t('system.engine.dataAuthorization.readRequired')" />
    <el-alert v-else-if="!canBrowse" type="warning" :closable="false" :title="t('system.engine.dataAuthorization.browseRequired')" />
    <el-alert v-else-if="!adapter" type="warning" :closable="false" :title="t('system.engine.dataAuthorization.unsupported')" />
    <template v-else>
      <div class="picker" :inert="busy || !ready || undefined">
        <ResourceTreePicker :key="generation" :model-value="selection" :adapter="adapter" :engine-id="engine.id" mode="any"
          :show-engine-selector="false" :show-search="false" :selectable-filter="isApprovalSelection"
          :title="t('system.engine.dataAuthorization.pickTable')" tree-height="280px" @update:model-value="selection = $event" @error="catalogError" />
      </div>
      <el-button data-testid="approval-add-selection" :loading="selecting" :disabled="busy || !selection || !ready" @click="addSelection">{{ t('system.engine.dataAuthorization.addSelection') }}</el-button>
      <p class="hint">{{ t('system.engine.dataAuthorization.snapshotOnly') }}</p>
      <el-table v-if="targets.length" :data="targets" data-testid="approval-targets">
        <el-table-column :label="t('system.engine.dataAuthorization.target')"><template #default="{ row }">{{ approvalPathLabel(row) }}</template></el-table-column>
        <el-table-column :label="t('system.engine.dataAuthorization.mode')"><template #default="{ row }">
          {{ existing(row) ? t(`system.engine.dataAuthorization.modes.${existing(row).mode}`) : t('system.engine.dataAuthorization.newTarget') }}
          <el-button v-if="canUpdate && existing(row)" data-testid="approval-change" link type="primary" :disabled="busy || !ready" @click="openChange(existing(row))">{{ t('system.engine.dataAuthorization.changeMode') }}</el-button>
        </template></el-table-column>
        <el-table-column width="90"><template #default="{ $index }"><el-button link type="danger" :disabled="busy" @click="remove($index)">{{ t('system.engine.dataAuthorization.remove') }}</el-button></template></el-table-column>
      </el-table>
      <el-form-item v-if="hasNew" :label="t('system.engine.dataAuthorization.mode')" required>
        <el-select v-model="mode" data-testid="approval-mode" :disabled="busy || !canInitialize" :placeholder="t('system.engine.dataAuthorization.selectMode')">
          <el-option value="independent" :label="t('system.engine.dataAuthorization.modes.independent')" />
          <el-option v-if="catalogAvailable" value="catalog" :label="t('system.engine.dataAuthorization.modes.catalog')" />
        </el-select>
      </el-form-item>
      <p v-if="hasNew && !catalogAvailable" class="hint">{{ t('system.engine.dataAuthorization.catalogUnavailable') }}</p>
      <p v-if="hasNew && !canInitialize" class="hint">{{ t('system.engine.dataAuthorization.initializeRequired') }}</p>
      <template v-if="businessTargets.length">
        <el-alert type="info" :closable="false" :title="t('system.engine.dataAuthorization.businessBoundary')" />
        <el-form v-if="newBusinessTargets.length" label-position="top" @submit.prevent="initializeBusiness">
          <el-form-item :label="t('system.engine.dataAuthorization.reason')" required><el-input v-model="reason" data-testid="approval-reason" type="textarea" maxlength="2000" :disabled="busy" /></el-form-item>
          <el-button data-testid="approval-initialize" :loading="saving" :disabled="busy || !canInitialize || !catalogAvailable" @click="initializeBusiness">{{ t('system.engine.dataAuthorization.prepareBusiness') }}</el-button>
        </el-form>
        <el-button v-for="path in businessTargets" :key="JSON.stringify(path)" :disabled="busy || !catalogAvailable || !existing(path)" @click="openBusiness(path)">{{ t('system.engine.dataAuthorization.openBusiness') }} · {{ approvalPathLabel(path) }}</el-button>
      </template>
    </template>
    <EngineSourceGrants :engine="engine" :requirements="directRequirements" @busy="grantBusy = $event" @close="closeGrant" @issued="reloadAfterGrant" />
    <el-collapse v-if="canRead"><el-collapse-item :title="t('system.engine.dataAuthorization.configurations')">
      <el-table :data="rows.slice((page - 1) * 10, page * 10)">
        <el-table-column :label="t('system.engine.dataAuthorization.target')"><template #default="{ row }">{{ approvalPathLabel(row.catalog_path) }}</template></el-table-column>
        <el-table-column :label="t('system.engine.dataAuthorization.mode')"><template #default="{ row }">{{ t(`system.engine.dataAuthorization.modes.${row.mode}`) }}</template></el-table-column>
        <el-table-column prop="version" :label="t('system.engine.dataAuthorization.version')" width="90" />
        <el-table-column v-if="canUpdate" :label="t('common.actions')" min-width="150"><template #default="{ row }">
          <el-button link type="primary" :disabled="busy || !ready" @click="openChange(row)">{{ t('system.engine.dataAuthorization.changeMode') }}</el-button>
        </template></el-table-column>
      </el-table>
      <el-pagination layout="prev, pager, next, total" :total="rows.length" :page-size="10" v-model:current-page="page" />
    </el-collapse-item></el-collapse>
    <el-dialog v-if="change" :model-value="true" :title="t('system.engine.dataAuthorization.changeMode')" width="min(640px, 94vw)" append-to-body
      :close-on-click-modal="false" :close-on-press-escape="!saving" :show-close="!saving" :before-close="closeChange">
      <el-alert type="warning" :closable="false" :title="t('system.engine.dataAuthorization.changeBoundary')" />
      <p class="hint">{{ approvalPathLabel(change.row.catalog_path) }} · {{ t('system.engine.dataAuthorization.currentMode') }}：{{ t(`system.engine.dataAuthorization.modes.${change.row.mode}`) }} · {{ t('system.engine.dataAuthorization.version') }} {{ change.row.version }}</p>
      <el-alert v-if="change.error" data-testid="approval-change-error" type="error" :closable="false" :title="change.error" />
      <el-form label-position="top" @submit.prevent="saveChange">
        <el-form-item :label="t('system.engine.dataAuthorization.mode')" required>
          <el-select v-model="change.mode" data-testid="approval-change-mode" :disabled="saving">
            <el-option value="independent" :disabled="!canGrant" :label="t('system.engine.dataAuthorization.modes.independent')" />
            <el-option value="catalog" :disabled="!catalogAvailable" :label="t('system.engine.dataAuthorization.modes.catalog')" />
          </el-select>
        </el-form-item>
        <p v-if="!canGrant" class="hint">{{ t('system.engine.dataAuthorization.successorRequired') }}</p>
        <p v-if="!catalogAvailable" class="hint">{{ t('system.engine.dataAuthorization.catalogUnavailable') }}</p>
        <el-form-item :label="t('system.engine.dataAuthorization.changeReason')" required>
          <el-input v-model="change.reason" data-testid="approval-change-reason" type="textarea" maxlength="2000" :disabled="saving" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button :disabled="saving" @click="closeChange()">{{ t('common.cancel') }}</el-button>
        <el-button v-if="change.needsReview" data-testid="approval-change-reload" :disabled="saving" @click="reloadChange">{{ t('system.engine.dataAuthorization.reviewCurrent') }}</el-button>
        <el-button data-testid="approval-change-save" type="primary" :loading="saving" :disabled="saving || change.needsReview || !change.reason.trim() || change.mode === change.row.mode" @click="saveChange">{{ t('common.confirm') }}</el-button>
      </template>
    </el-dialog>
  </section>
</template>

<script setup>
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessageBox } from 'element-plus'
import { ResourceTreePicker, serializeEngineApprovalInitialization, openConsoleRoute } from '@common-ui'
import { enginesAPI } from '../../api/engines'
import client from '../../api/client'
import { useAuthStore } from '../../store/auth'
import { approvalPathLabel, approvalTargetsEqual, collectApprovalTargets, createApprovalCatalogAdapter, isApprovalSelection } from '../../utils/engineApprovalCatalog'
import EngineSourceGrants from './EngineSourceGrants.vue'

const props = defineProps({ engine: { type: Object, required: true } })
const auth = useAuthStore(), { t } = useI18n()
const canRead = computed(() => auth.hasPermission('system.engine_access_approval_requirement.read'))
const canInitialize = computed(() => auth.hasPermission('system.engine_access_approval_requirement.initialize'))
const canUpdate = computed(() => auth.hasPermission('system.engine_access_approval_requirement.update'))
const canGrant = computed(() => auth.hasPermission('system.engine_access_grant.create'))
const canBrowse = computed(() => auth.hasPermission('system.engine_catalog.read'))
const rows = ref([]), page = ref(1), loading = ref(false), ready = ref(false), error = ref(''), saving = ref(false), selecting = ref(false), grantBusy = ref(false)
const selection = ref(null), targets = ref([]), mode = ref(''), reason = ref(''), generation = ref(0), catalogAvailable = ref(false)
const change = ref(null), changed = ref(false)
const operating = computed(() => saving.value || selecting.value || grantBusy.value)
const busy = computed(() => operating.value || !!change.value)
const adapter = computed(() => {
  if (!canBrowse.value || props.engine.lifecycle_state !== 'active' || props.engine.connection_status !== 'online') return null
  try { return createApprovalCatalogAdapter(props.engine, enginesAPI.listCatalogChildren) } catch { return null }
})
const existing = path => rows.value.find(row => approvalTargetsEqual(row.catalog_path, path))
const hasNew = computed(() => targets.value.some(path => !existing(path)))
const businessTargets = computed(() => targets.value.filter(path => (existing(path)?.mode || mode.value) === 'catalog'))
const newBusinessTargets = computed(() => businessTargets.value.filter(path => !existing(path)))
const directRequirements = computed(() => !ready.value ? [] : targets.value.filter(path =>
  (existing(path)?.mode || mode.value) === 'independent' && (!!existing(path) || canInitialize.value)).map(path =>
  existing(path) || { engine_id: String(props.engine.id), catalog_path: path, mode: 'independent', version: '1', initialize_approval: true }))
const message = e => e?.response?.data?.error || t('system.engine.dataAuthorization.failed')
let readSequence = 0
function closeGrant() { targets.value = []; mode.value = '' }
async function reloadAfterGrant() { await nextTick(); load() }
function catalogError(e) { if (!busy.value) error.value = message(e) }
async function load() {
  if (!canRead.value || busy.value) return
  const epoch = generation.value, seq = ++readSequence
  loading.value = true; ready.value = false; rows.value = []; error.value = ''
  try {
    const all = []
    for (let current = 1; ; current++) {
      const result = await enginesAPI.listApprovalRequirements(props.engine.id, { page: current, page_size: 100 })
      if (epoch !== generation.value || seq !== readSequence) return
      if (!Array.isArray(result?.data) || !Number.isSafeInteger(result.total) || result.total < 0 || result.data.length > 100 || (all.length < result.total && !result.data.length)) throw new Error('invalidConfiguration')
      all.push(...result.data)
      if (all.length >= result.total) break
    }
    rows.value = all; ready.value = true
  } catch (e) { if (epoch === generation.value && seq === readSequence) error.value = message(e) }
  finally { if (epoch === generation.value && seq === readSequence) loading.value = false }
}
async function observeCatalog() {
  catalogAvailable.value = false
  if (!auth.hasPermission('catalog.entry.read')) return
  const epoch = generation.value
  try {
    const result = await client.get('/catalog/entries', { params: { source_engine_id: String(props.engine.id), page: 1, page_size: 1 } })
    if (epoch === generation.value && Array.isArray(result?.data)) catalogAvailable.value = true
  } catch { /* Do not downgrade an existing business approval on read failure. */ }
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
  } catch (e) { if (epoch === generation.value) error.value = e?.response?.data?.error || t('system.engine.dataAuthorization.selectionFailed') }
  finally { if (epoch === generation.value) selecting.value = false }
}
function remove(index) { if (!busy.value) targets.value = targets.value.filter((_, i) => i !== index) }
function openChange(row) {
  if (busy.value || !canUpdate.value || !ready.value) return
  changed.value = false
  change.value = { row: { ...row }, mode: row.mode, reason: '', error: '', needsReview: false }
}
function closeChange(done) {
  if (saving.value) return
  const needsReview = change.value?.needsReview
  change.value = null
  if (typeof done === 'function') done()
  if (needsReview) load()
}
function validateChangeRow(row, original) {
  if (!row || row.id !== original.id || String(row.engine_id) !== String(props.engine.id) ||
    !approvalTargetsEqual(row.catalog_path, original.catalog_path) || !['catalog', 'independent'].includes(row.mode) ||
    !Number.isSafeInteger(row.version) || row.version <= 0) throw new Error('invalidConfiguration')
}
function replaceRequirement(row) { rows.value = rows.value.map(prior => prior.id === row.id ? row : prior) }
async function reloadChange() {
  if (operating.value || !change.value || !canRead.value) return
  const draft = change.value, epoch = generation.value
  saving.value = true
  try {
    const row = await enginesAPI.getApprovalRequirement(props.engine.id, draft.row.id)
    if (epoch !== generation.value || change.value !== draft) return
    validateChangeRow(row, draft.row)
    replaceRequirement(row); draft.row = row; draft.needsReview = false; draft.error = ''
  } catch (e) { if (epoch === generation.value && change.value === draft) draft.error = message(e) }
  finally { if (epoch === generation.value) saving.value = false }
}
async function saveChange() {
  if (operating.value || !canUpdate.value || !change.value || change.value.needsReview) return
  const draft = change.value, epoch = generation.value
  if (!draft.reason.trim() || draft.reason.length > 2000 || draft.mode === draft.row.mode ||
    (draft.mode === 'independent' && !canGrant.value) || (draft.mode === 'catalog' && !catalogAvailable.value)) return
  saving.value = true; draft.error = ''; changed.value = false
  let submitted = false
  try {
    validateChangeRow(draft.row, draft.row)
    const payload = { version: draft.row.version, mode: draft.mode, reason: draft.reason.trim() }
    await ElMessageBox.confirm(t('system.engine.dataAuthorization.changeConfirm', { target: approvalPathLabel(draft.row.catalog_path), mode: t(`system.engine.dataAuthorization.modes.${draft.mode}`) }),
      t('system.engine.dataAuthorization.changeMode'), { confirmButtonText: t('common.confirm'), cancelButtonText: t('common.cancel'), customClass: 'addp-message-box' })
    if (epoch !== generation.value || change.value !== draft) return
    submitted = true
    const row = await enginesAPI.updateApprovalRequirement(props.engine.id, draft.row.id, payload)
    if (epoch !== generation.value || change.value !== draft) return
    validateChangeRow(row, draft.row)
    if (row.mode !== payload.mode || row.version !== payload.version + 1) throw new Error('invalidConfiguration')
    replaceRequirement(row); change.value = null; changed.value = true
  } catch (e) {
    if (epoch === generation.value && change.value === draft && e !== 'cancel' && e !== 'close') {
      draft.needsReview = submitted
      draft.error = e?.response?.data?.error_code === 'resource_version_conflict'
        ? t('system.engine.dataAuthorization.versionConflict') : message(e)
    }
  } finally { if (epoch === generation.value) saving.value = false }
}
async function initializeBusiness() {
  if (busy.value || !canInitialize.value || !catalogAvailable.value || !newBusinessTargets.value.length) return
  const epoch = generation.value, paths = [...newBusinessTargets.value]
  saving.value = true; error.value = ''
  try {
    const commands = paths.map(path => serializeEngineApprovalInitialization(path, 'catalog', reason.value))
    await ElMessageBox.confirm(t('system.engine.dataAuthorization.businessBoundary'), t('system.engine.dataAuthorization.prepareBusiness'), { confirmButtonText: t('common.confirm'), cancelButtonText: t('common.cancel'), customClass: 'addp-message-box' })
    if (epoch !== generation.value) return
    for (const command of commands) {
      const row = await enginesAPI.initializeApprovalRequirement(props.engine.id, command)
      if (epoch !== generation.value) return
      rows.value = [...rows.value, row]
    }
  } catch (e) { if (epoch === generation.value && e !== 'cancel' && e !== 'close') error.value = message(e) }
  finally { if (epoch === generation.value) saving.value = false }
}
function openBusiness(path) {
  const params = new URLSearchParams({ source_engine_id: String(props.engine.id), search: path.segments.at(-1).name })
  if (auth.hasPermission('catalog.inventory.read')) params.set('view', 'inventory')
  openConsoleRoute(`/catalog/entries?${params}`)
}
watch([() => props.engine.id, () => props.engine.lifecycle_state, () => props.engine.connection_status,
  () => auth.authContext?.principal?.id, () => auth.authContext?.context?.tenant_id, () => auth.authContext?.context?.tenant_membership_id,
  () => auth.authContext?.authorization?.authorization_version, canRead, canInitialize, canUpdate, canGrant, canBrowse,
  () => auth.hasPermission('catalog.entry.read')], () => {
  generation.value++; targets.value = []; rows.value = []; selection.value = null; mode.value = reason.value = ''; page.value = 1
  change.value = null; changed.value = false
  ready.value = catalogAvailable.value = loading.value = saving.value = selecting.value = grantBusy.value = false
  load(); observeCatalog()
}, { immediate: true })
onBeforeUnmount(() => { generation.value++ })
</script>

<style scoped>
.data-authorization { display: grid; gap: 16px; min-width: 0; }
.section-heading { display: flex; align-items: center; justify-content: space-between; gap: 16px; }
.el-select { width: 100%; }
.hint { color: var(--addp-text-color-secondary); margin: 0; overflow-wrap: anywhere; }
</style>
