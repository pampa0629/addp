<template>
  <section class="data-authorization" data-testid="engine-data-authorization">
    <el-alert type="info" :closable="false" show-icon :title="t('system.engine.dataAuthorization.boundary')" />
    <el-alert v-if="error" type="error" :closable="false" :title="error" />
    <el-alert v-if="saved" type="success" :closable="false" :title="t('system.engine.dataAuthorization.saved')" />
    <template v-if="canRead">
      <div class="section-heading">
        <h3>{{ t('system.engine.dataAuthorization.configurations') }}</h3>
        <el-button :loading="loading" :disabled="saving" @click="load">{{ t('common.refresh') }}</el-button>
      </div>
      <el-table :data="rows" v-loading="loading">
        <el-table-column :label="t('system.engine.dataAuthorization.target')" min-width="220">
          <template #default="{ row }">{{ approvalPathLabel(row.catalog_path) }}</template>
        </el-table-column>
        <el-table-column :label="t('system.engine.dataAuthorization.mode')" min-width="180">
          <template #default="{ row }">{{ t(`system.engine.dataAuthorization.modes.${row.mode}`) }}</template>
        </el-table-column>
        <el-table-column prop="version" :label="t('system.engine.dataAuthorization.version')" width="90" />
      </el-table>
      <el-pagination layout="prev, pager, next, total" :total="total" :page-size="10" :current-page="page" :disabled="saving" @current-change="changePage" />
    </template>
    <el-alert v-else type="info" :closable="false" :title="t('system.engine.dataAuthorization.readRequired')" />
    <template v-if="canInitialize">
      <h3>{{ t('system.engine.dataAuthorization.initialize') }}</h3>
      <el-alert v-if="!canBrowse" type="warning" :closable="false" :title="t('system.engine.dataAuthorization.browseRequired')" />
      <el-alert v-else-if="!adapter" type="warning" :closable="false" :title="t('system.engine.dataAuthorization.unsupported')" />
      <el-form v-else label-position="top" @submit.prevent="initialize">
        <el-form-item :label="t('system.engine.dataAuthorization.target')" required>
          <div class="picker" :inert="saving || undefined">
            <ResourceTreePicker :key="generation" :model-value="selection" :adapter="adapter" :engine-id="engine.id"
              mode="any" :show-engine-selector="false" :show-search="false" :selectable-filter="isApprovalTable"
              :title="t('system.engine.dataAuthorization.pickTable')" tree-height="320px" @update:model-value="select" @error="catalogError" />
          </div>
        </el-form-item>
        <p class="hint">{{ t('system.engine.dataAuthorization.tableOnly') }}</p>
        <el-alert v-if="existing" type="info" :closable="false" :title="t('system.engine.dataAuthorization.exists', { mode: t(`system.engine.dataAuthorization.modes.${existing.mode}`) })" />
        <el-form-item :label="t('system.engine.dataAuthorization.mode')" required>
          <el-select v-model="form.mode" data-testid="approval-mode" :disabled="saving || !!existing" :placeholder="t('system.engine.dataAuthorization.selectMode')">
            <el-option v-for="mode in ['independent', 'catalog']" :key="mode" :value="mode" :label="t(`system.engine.dataAuthorization.modes.${mode}`)" />
          </el-select>
        </el-form-item>
        <p v-if="form.mode" class="hint">{{ t(`system.engine.dataAuthorization.descriptions.${form.mode}`) }}</p>
        <el-form-item :label="t('system.engine.dataAuthorization.reason')" required>
          <el-input v-model="form.reason" data-testid="approval-reason" type="textarea" maxlength="2000" show-word-limit :disabled="saving || !!existing" />
        </el-form-item>
        <el-button type="primary" data-testid="approval-initialize" :disabled="!!existing" :loading="saving" @click="initialize">{{ t('system.engine.dataAuthorization.saveMode') }}</el-button>
      </el-form>
    </template>
    <el-alert type="info" :closable="false" :title="t('system.engine.dataAuthorization.accessPending')" />
  </section>
</template>

<script setup>
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessageBox } from 'element-plus'
import { ResourceTreePicker, serializeEngineApprovalInitialization } from '@common-ui'
import { enginesAPI } from '../../api/engines'
import { useAuthStore } from '../../store/auth'
import { approvalPathLabel, approvalTargetFromSelection, approvalTargetsEqual, createApprovalCatalogAdapter, isApprovalTable } from '../../utils/engineApprovalCatalog'

const props = defineProps({ engine: { type: Object, required: true } })
const auth = useAuthStore(), { t } = useI18n()
const canRead = computed(() => auth.hasPermission('system.engine_access_approval_requirement.read'))
const canInitialize = computed(() => auth.hasPermission('system.engine_access_approval_requirement.initialize'))
const canBrowse = computed(() => auth.hasPermission('system.engine_catalog.read'))
const rows = ref([]), total = ref(0), page = ref(1), loading = ref(false), error = ref(''), saving = ref(false), saved = ref(false)
const selection = ref(null), target = ref(null), savedRow = ref(null), generation = ref(0)
const form = reactive({ mode: '', reason: '' })
let readSequence = 0
const adapter = computed(() => {
  if (!canBrowse.value || props.engine.lifecycle_state !== 'active' || props.engine.connection_status !== 'online') return null
  try { return createApprovalCatalogAdapter(props.engine, enginesAPI.listCatalogChildren) } catch { return null }
})
const targetMatches = row => target.value && String(row.engine_id) === String(props.engine.id) &&
  approvalTargetsEqual(row.catalog_path, target.value)
const existing = computed(() => [savedRow.value, ...rows.value].find(row => row && targetMatches(row)))
const message = e => e?.response?.data?.error || t('system.engine.dataAuthorization.failed')
function catalogError(e) { if (!saving.value) error.value = message(e) }
function select(value) {
  if (saving.value) return
  selection.value = value; target.value = null; saved.value = false; error.value = ''; form.mode = ''; form.reason = ''
  if (!value) return
  try { target.value = approvalTargetFromSelection(value, props.engine.id) }
  catch { selection.value = null; error.value = t('system.engine.dataAuthorization.invalid') }
}
async function load() {
  if (!canRead.value) return
  const epoch = generation.value, seq = ++readSequence
  loading.value = true; rows.value = []; total.value = 0; error.value = ''
  try {
    const result = await enginesAPI.listApprovalRequirements(props.engine.id, { page: page.value, page_size: 10 })
    if (epoch !== generation.value || seq !== readSequence) return
    if (!Array.isArray(result?.data) || !Number.isSafeInteger(result.total) || result.total < 0) throw new Error('invalidConfiguration')
    rows.value = result.data; total.value = result.total
  } catch (e) { if (epoch === generation.value && seq === readSequence) error.value = message(e) }
  finally { if (epoch === generation.value && seq === readSequence) loading.value = false }
}
function changePage(value) { page.value = value; load() }
async function initialize() {
  if (saving.value || !canInitialize.value || !canBrowse.value || !adapter.value || existing.value) return
  let payload
  try { payload = serializeEngineApprovalInitialization(target.value, form.mode, form.reason) }
  catch { error.value = t('system.engine.dataAuthorization.invalid'); return }
  const epoch = generation.value, snapshot = JSON.parse(JSON.stringify(target.value)), mode = form.mode
  saving.value = true; error.value = ''; saved.value = false
  try {
    await ElMessageBox.confirm(t('system.engine.dataAuthorization.confirmBody', { target: approvalPathLabel(snapshot), mode: t(`system.engine.dataAuthorization.modes.${mode}`) }),
      t('system.engine.dataAuthorization.saveMode'), { customClass: 'addp-message-box', confirmButtonText: t('common.confirm'), cancelButtonText: t('common.cancel') })
    if (epoch !== generation.value || !canInitialize.value || !canBrowse.value) return
    const result = await enginesAPI.initializeApprovalRequirement(props.engine.id, payload)
    if (epoch !== generation.value) return
    if (!result?.id || result.mode !== mode || result.version !== 1 || !targetMatches(result)) throw new Error('invalidConfiguration')
    savedRow.value = result; saved.value = true; form.reason = ''
    await load()
  } catch (e) { if (epoch === generation.value && e !== 'cancel' && e !== 'close') error.value = message(e) }
  finally { if (epoch === generation.value) saving.value = false }
}
watch(() => [props.engine.id, auth.authContext?.principal?.id, auth.authContext?.context?.tenant_id,
  auth.authContext?.context?.tenant_membership_id, auth.authContext?.authorization?.authorization_version,
  canRead.value, canInitialize.value, canBrowse.value, props.engine.lifecycle_state, props.engine.connection_status], () => {
  generation.value++; rows.value = []; total.value = 0; page.value = 1; selection.value = target.value = savedRow.value = null
  loading.value = saving.value = saved.value = false; form.mode = ''; form.reason = ''; error.value = ''
  load()
}, { immediate: true })
onBeforeUnmount(() => { generation.value++ })
</script>

<style scoped>
.data-authorization { display: grid; gap: 16px; }
.section-heading { display: flex; align-items: center; justify-content: space-between; gap: 12px; flex-wrap: wrap; }
h3 { margin: 0; }
.picker, .el-select { width: 100%; min-width: 0; }
.hint { color: var(--addp-text-secondary); overflow-wrap: anywhere; }
</style>
