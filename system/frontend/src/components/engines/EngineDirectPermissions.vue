<template>
  <section class="direct-permissions" data-testid="direct-permissions">
    <el-alert type="info" :closable="false" :title="t('system.engine.directPermission.boundary')" show-icon />
    <div class="permission-toolbar">
      <el-select v-model="state" :disabled="busy" :placeholder="t('system.engine.directPermission.state')" :aria-label="t('system.engine.directPermission.state')" data-testid="direct-permission-state">
        <el-option value="" :label="t('system.engine.directPermission.all')" />
        <el-option v-for="value in ['allowed', 'denied', 'unknown']" :key="value" :value="value" :label="t(`system.engine.directPermission.states.${value}`)" />
      </el-select>
      <el-button :disabled="busy || changes.size > 0" :loading="loading" @click="load">{{ t('common.refresh') }}</el-button>
    </div>
    <div v-if="canManage" class="permission-toolbar">
      <el-checkbox v-model="recursive" :disabled="busy" data-testid="direct-permission-recursive">{{ t('system.engine.directPermission.recursive') }}</el-checkbox>
      <span>{{ t('system.engine.directPermission.selectionHint') }}</span>
    </div>
    <el-alert v-if="error" type="error" :closable="false" :title="error" />
    <ResourceTree :tree-data="visibleTree" :loading="loading" :title="engine.name" :show-count="false" :show-refresh-button="false"
      :filter-text="query" :filter-method="matchesQuery" :filter-placeholder="t('system.engine.directPermission.search')" @update:filter-text="query = $event"
      :default-expand-root="true" height="min(48vh, 520px)" :empty-text="t('system.engine.directPermission.empty')" :filter-empty-text="t('system.engine.directPermission.empty')" data-testid="direct-permission-list">
      <template #node="{ data }">
        <div class="permission-node" :data-item-fingerprint="data.permission?.item_fingerprint" :data-permission-node="data.id">
          <el-checkbox v-if="canManage" :model-value="checked(data)" :indeterminate="mixed(data)" :disabled="busy || !nodeItems(data).length"
            :aria-label="data.label || engine.name" :data-testid="data.permission ? 'direct-permission-check' : 'direct-permission-node-check'"
            @click.stop @dblclick.stop @change="value => toggle(data, value)">{{ data.label || engine.name }}</el-checkbox>
          <span v-else class="permission-name">{{ data.label || engine.name }}</span>
          <template v-if="data.permission">
            <el-tag :type="data.permission.state === 'allowed' ? 'success' : 'info'">{{ t(`system.engine.directPermission.states.${data.permission.state}`) }}</el-tag>
            <span class="permission-source">{{ t(`system.engine.directPermission.sources.${data.permission.source}`) }}</span>
            <el-tag v-if="changes.has(data.permission.item_fingerprint)" type="warning">{{ t('system.engine.directPermission.modified') }}</el-tag>
          </template>
          <span v-else class="permission-source">{{ t('system.engine.directPermission.itemCount', { count: nodeItems(data).length }) }}</span>
        </div>
      </template>
    </ResourceTree>
    <div class="permission-toolbar">
      <span data-testid="direct-permission-count">{{ t('system.engine.directPermission.count', { count: rows.length, changed: changedRows.length }) }}</span>
      <el-button v-if="canManage" :disabled="busy || !changes.size" @click="changes = new Map()">{{ t('system.engine.directPermission.discard') }}</el-button>
      <el-button v-if="canManage" type="primary" :disabled="busy || !changedRows.length" data-testid="direct-permission-review" @click="prepare">{{ t('system.engine.directPermission.batchReview') }}</el-button>
    </div>
    <el-dialog v-if="draft && authorizationReady" :model-value="true" :title="t('system.engine.directPermission.batchReview')" append-to-body width="min(900px, 94vw)"
      :close-on-click-modal="false" :close-on-press-escape="!saving" :show-close="!saving" :before-close="close">
      <el-alert type="warning" :closable="false" :title="t('system.engine.directPermission.changeBoundary')" />
      <p>{{ t('system.engine.directPermission.batchHint') }}</p>
      <el-alert v-if="draft.error" type="error" :closable="false" :title="draft.error" />
      <el-table :data="draft.items" max-height="360" data-testid="direct-permission-review-list">
        <el-table-column :label="t('system.engine.dataAuthorization.target')" min-width="200"><template #default="{ row: item }">{{ item.label }}</template></el-table-column>
        <el-table-column :label="t('system.engine.directPermission.current')" min-width="150"><template #default="{ row: item }">
          {{ t(`system.engine.directPermission.states.${item.row.state}`) }} · {{ t('system.engine.dataAuthorization.version') }} {{ item.row.version }}
        </template></el-table-column>
        <el-table-column :label="t('system.engine.directPermission.after')" min-width="140"><template #default="{ row: item }"><el-checkbox v-model="item.allowed" :disabled="saving || ['succeeded', 'unchanged'].includes(item.status)" data-testid="direct-permission-value">{{ t(`system.engine.directPermission.states.${item.allowed ? 'allowed' : 'denied'}`) }}</el-checkbox></template></el-table-column>
        <el-table-column :label="t('system.engine.directPermission.result')" min-width="140"><template #default="{ row: item }">
          {{ t(`system.engine.directPermission.results.${item.status}`) }}<p v-if="item.error">{{ item.error }}</p>
        </template></el-table-column>
      </el-table>
      <el-form label-position="top" @submit.prevent="save">
        <el-form-item :label="t('system.engine.dataAuthorization.reason')" required><el-input v-model="draft.reason" type="textarea" maxlength="2000" :disabled="saving" data-testid="direct-permission-reason" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button :disabled="saving" @click="close()">{{ t('common.cancel') }}</el-button>
        <el-button v-if="draft.needsReview" :disabled="saving" @click="review">{{ t('system.engine.dataAuthorization.reviewCurrent') }}</el-button>
        <el-button type="primary" :loading="saving" :disabled="saving || draft.needsReview || !draft.reason.trim()" data-testid="direct-permission-save" @click="save">{{ t('common.confirm') }}</el-button>
      </template>
    </el-dialog>
  </section>
</template>
<script setup>
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessageBox } from 'element-plus'
import { ResourceTree, filterTree } from '@common-ui'
import { enginesAPI } from '../../api/engines'
import { useAuthStore } from '../../store/auth'
import { buildDirectPermissionTree, captureDirectPermission, changedPermissionRows, permissionNodeItems, readDirectPermissionTree, readPermissionTarget, validateDirectPermission } from '../../utils/directPermission'
const props = defineProps({ engine: { type: Object, required: true }, authorizationKey: { type: String, required: true }, authorizationReady: Boolean })
const { t } = useI18n(), auth = useAuthStore()
const canManage = computed(() => auth.hasPermission('system.engine_access_direct_permission.update') && auth.hasPermission('system.engine_access_delegation.create'))
const rows = ref([]), query = ref(''), state = ref(''), error = ref(''), loading = ref(false), saving = ref(false), draft = ref(null), recursive = ref(false), changes = ref(new Map())
const busy = computed(() => !props.authorizationReady || loading.value || saving.value || !!draft.value)
const tree = computed(() => buildDirectPermissionTree(rows.value))
const changedRows = computed(() => changedPermissionRows(rows.value, changes.value))
const visibleTree = computed(() => !state.value ? tree.value : filterTree(tree.value, node => node.permission?.state === state.value))
const matchesQuery = (node, keyword) => !!node.permission && node.permission.full_name.toLowerCase().includes(keyword.trim().toLowerCase())
let epoch = 0, sequence = 0
const message = e => e?.response?.data?.error || t(e?.message === 'permissionTreeCapacity' ? 'system.engine.directPermission.capacity' : 'system.engine.dataAuthorization.failed')
// Always use the complete tree, not the filtered children supplied by the renderer.
const originalNodes = computed(() => {
  const map = new Map(); const visit = nodes => nodes.forEach(node => { map.set(node.id, node); if (node.children) visit(node.children) }); visit(tree.value); return map
})
const nodeItems = node => permissionNodeItems(originalNodes.value.get(node.id) || node, recursive.value)
const desired = row => changes.value.has(row.item_fingerprint) ? changes.value.get(row.item_fingerprint) : row.state === 'unknown' ? null : row.state === 'allowed'
const checked = node => { const items = nodeItems(node); return items.length > 0 && items.every(row => desired(row) === true) }
const mixed = node => { const items = nodeItems(node); return items.some(row => desired(row) === null) || items.some(row => desired(row) === true) && !items.every(row => desired(row) === true) }
function toggle(node, allowed) {
  if (busy.value || !canManage.value) return
  const next = new Map(changes.value)
  for (const row of nodeItems(node)) {
    if (row.state !== 'unknown' && allowed === (row.state === 'allowed')) next.delete(row.item_fingerprint)
    else next.set(row.item_fingerprint, allowed)
  }
  changes.value = next
}
function updateRow(permission) { rows.value = rows.value.map(row => row.item_fingerprint === permission.item_fingerprint ? { ...row, ...permission } : row) }
async function load() {
  if (!props.authorizationReady || saving.value || draft.value || changes.value.size) return
  const generation = epoch, request = ++sequence
  loading.value = true; error.value = ''; rows.value = []
  try {
    const result = await readDirectPermissionTree(props.engine.id, enginesAPI.listDirectPermissions, () => epoch === generation && request === sequence)
    if (epoch === generation && request === sequence) rows.value = result
  } catch (e) { if (epoch === generation && request === sequence) error.value = message(e) }
  finally { if (epoch === generation && request === sequence) loading.value = false }
}
async function observe(row) {
  const [permission, target] = await Promise.all([
    enginesAPI.getDirectPermission(props.engine.id, row.item_fingerprint),
    readPermissionTarget(props.engine.id, row.item_fingerprint, enginesAPI.getAuthorizationFacts)
  ])
  return { row: validateDirectPermission(permission, props.engine.id, row.item_fingerprint), target }
}
async function prepare() {
  if (busy.value || !canManage.value || !changedRows.value.length) return
  const generation = epoch, items = changedRows.value.map(row => ({ row, label: row.full_name, allowed: changes.value.get(row.item_fingerprint), status: 'pending', error: '' }))
  loading.value = true; error.value = ''
  try {
    for (const item of items) {
      const observed = await observe(item.row)
      if (generation !== epoch || !props.authorizationReady) return
      Object.assign(item, observed)
    }
    draft.value = { items, reason: '', error: '', needsReview: false }
  } catch (e) { if (generation === epoch) error.value = message(e) }
  finally { if (generation === epoch) loading.value = false }
}
function close(done) {
  if (saving.value) return
  // Preserve the reviewed intent, including any explicit change after a conflict.
  for (const item of draft.value?.items || []) {
    if (['succeeded', 'unchanged'].includes(item.status)) continue
    if (item.row.state === (item.allowed ? 'allowed' : 'denied')) changes.value.delete(item.row.item_fingerprint)
    else changes.value.set(item.row.item_fingerprint, item.allowed)
  }
  draft.value = null
  if (typeof done === 'function') done()
}
async function review() {
  if (saving.value || !draft.value || !props.authorizationReady) return
  const current = draft.value, generation = epoch
  saving.value = true
  try {
    const observations = []
    for (const item of current.items.filter(item => !['succeeded', 'unchanged'].includes(item.status))) {
      const observation = await observe(item.row)
      if (generation !== epoch || draft.value !== current) return
      observations.push([item, observation])
    }
    for (const [item, observation] of observations) { Object.assign(item, observation, { status: 'pending', error: '' }); updateRow(observation.row) }
    current.needsReview = false; current.error = ''
  } catch (e) { if (generation === epoch && draft.value === current) current.error = message(e) }
  finally { if (generation === epoch) saving.value = false }
}
async function save() {
  if (saving.value || !props.authorizationReady || !canManage.value || !draft.value || draft.value.needsReview || !draft.value.reason.trim()) return
  const current = draft.value, generation = epoch
  saving.value = true; current.error = ''; let active = null
  try {
    // Freeze every command before the first write; no partial submission on preparation failure.
    const commands = current.items.filter(item => item.status === 'pending').map(item => ({ item, payload: captureDirectPermission(item.row, item.target, item.allowed, current.reason) }))
    await ElMessageBox.confirm(t('system.engine.directPermission.changeBoundary'), t('system.engine.directPermission.batchReview'), { confirmButtonText: t('common.confirm'), cancelButtonText: t('common.cancel'), customClass: 'addp-message-box' })
    for (const { item, payload } of commands) {
      if (generation !== epoch || draft.value !== current || !props.authorizationReady || !canManage.value) return
      active = item; item.status = 'saving'
      if (item.row.state === (item.allowed ? 'allowed' : 'denied')) {
        item.status = 'unchanged'; updateRow(item.row); changes.value.delete(item.row.item_fingerprint); active = null; continue
      }
      const result = validateDirectPermission(await enginesAPI.setDirectPermission(props.engine.id, item.row.item_fingerprint, payload), props.engine.id, item.row.item_fingerprint)
      if (generation !== epoch || draft.value !== current) return
      const expected = item.row.source === 'explicit' ? BigInt(item.row.version) + 1n : 1n
      if (result.source !== 'explicit' || result.state !== (item.allowed ? 'allowed' : 'denied') || BigInt(result.version) !== expected) throw new Error('invalidPermissionWrite')
      item.status = 'succeeded'; updateRow(result); changes.value.delete(item.row.item_fingerprint); active = null
    }
    draft.value = null
  } catch (e) {
    if (generation === epoch && draft.value === current && e !== 'cancel' && e !== 'close') {
      current.needsReview = true; current.error = message(e)
      if (active) { active.status = e?.response?.status >= 400 && e?.response?.status < 500 ? 'failed' : 'unknown'; active.error = message(e) }
    }
  } finally { if (generation === epoch) saving.value = false }
}
watch(() => props.authorizationKey, () => {
  epoch++; sequence++; rows.value = []; changes.value = new Map(); draft.value = null; loading.value = saving.value = false; error.value = ''; ElMessageBox.close()
  if (props.authorizationReady) load()
}, { immediate: true })
watch(() => props.authorizationReady, ready => {
  epoch++; sequence++; loading.value = saving.value = false; ElMessageBox.close()
  if (draft.value) {
    draft.value.needsReview = true
    for (const item of draft.value.items) if (item.status === 'saving') item.status = 'unknown'
  }
  if (ready) load()
})
onBeforeUnmount(() => { epoch++; sequence++; ElMessageBox.close() })
</script>
<style scoped>
.direct-permissions { display: grid; gap: 16px; min-width: 0; }
.permission-toolbar { display: flex; flex-wrap: wrap; gap: 12px; align-items: center; }
.permission-toolbar .el-input { width: min(300px, 100%); }
.permission-toolbar .el-select { width: 200px; }
.permission-node { display: flex; align-items: center; gap: 12px; min-width: 0; width: 100%; }
.permission-node .el-checkbox, .permission-name { min-width: 0; flex: 1; overflow: hidden; }
.permission-node :deep(.el-checkbox__label) { overflow: hidden; text-overflow: ellipsis; }
.permission-source { color: var(--addp-text-secondary); font-size: 12px; }
p { overflow-wrap: anywhere; }
</style>
