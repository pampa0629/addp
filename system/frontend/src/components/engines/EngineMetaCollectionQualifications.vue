<template>
  <section class="collection-qualifications" data-testid="meta-collection-qualifications" :inert="!authorizationReady || undefined">
    <el-radio-group v-model="view" :disabled="saving || !!revocation" @change="changeView" data-testid="collection-workspace-tabs">
      <el-radio-button value="create" :disabled="!canApprove">{{ t('system.engine.collection.new') }}</el-radio-button>
      <el-radio-button value="records" :disabled="!canRead">{{ t('system.engine.collection.records') }}</el-radio-button>
    </el-radio-group>
    <el-alert type="info" :closable="false" :title="t('system.engine.collection.boundary')" />
    <el-alert v-if="!canRead" type="warning" :closable="false" :title="t('system.engine.collection.readRequired')" />
    <el-alert v-if="error" type="error" :closable="false" :title="error" />
    <StatusAnnouncer :message="success" />
    <el-alert v-if="success" type="success" :closable="false" :title="success" />
    <template v-if="view === 'create' && canApprove">
      <el-form label-position="top" @submit.prevent="approve">
        <el-form-item :label="t('system.engine.collection.task')" required>
          <el-input v-model="taskID" data-testid="collection-task" :disabled="saving || !!command" @input="invalidateFacts" />
        </el-form-item>
        <el-button data-testid="collection-inspect" :loading="inspecting" :disabled="!canRead || saving || !!command" @click="inspect">{{ t('system.engine.collection.inspect') }}</el-button>
        <el-descriptions v-if="facts" :column="1" border data-testid="collection-facts">
          <el-descriptions-item :label="t('system.engine.collection.engine')">{{ engine.name }}</el-descriptions-item>
          <el-descriptions-item :label="t('system.engine.collection.task')">{{ facts.task_id }}</el-descriptions-item>
          <el-descriptions-item :label="t('system.engine.collection.scope')">{{ t('system.engine.collection.entireEngine') }}</el-descriptions-item>
          <el-descriptions-item :label="t('system.engine.collection.depth')">{{ t('system.engine.collection.basic') }}</el-descriptions-item>
          <el-descriptions-item :label="t('system.engine.collection.force')">{{ t(facts.force ? 'system.engine.collection.yes' : 'system.engine.collection.no') }}</el-descriptions-item>
        </el-descriptions>
        <el-form-item :label="t('system.engine.collection.purpose')" required>
          <el-input v-model="purpose" type="textarea" maxlength="2000" show-word-limit data-testid="collection-purpose" :disabled="saving || !!command" />
        </el-form-item>
        <el-form-item :label="t('system.engine.collection.expiry')" required>
          <el-date-picker v-model="expiresAt" type="datetime" :disabled="saving || !!command" data-testid="collection-expiry" />
        </el-form-item>
        <p v-if="command" role="status">{{ t('system.engine.collection.unknown') }}</p>
        <el-button v-if="command" type="primary" data-testid="collection-retry" :loading="saving" @click="approve">{{ t('system.engine.collection.retry') }}</el-button>
        <el-button v-else type="primary" data-testid="collection-approve" :loading="saving" :disabled="!facts || !canRead || inspecting" @click="approve">{{ t('system.engine.collection.approve') }}</el-button>
      </el-form>
    </template>
    <template v-if="view === 'records' && canRead">
      <el-button data-testid="collection-refresh" :loading="loading" :disabled="saving || !!revocation" @click="load">{{ t('common.refresh') }}</el-button>
      <el-table :data="rows" v-loading="loading" data-testid="collection-records">
        <el-table-column prop="task_id" :label="t('system.engine.collection.task')" min-width="100" />
        <el-table-column prop="purpose" :label="t('system.engine.collection.purpose')" min-width="200" />
        <el-table-column :label="t('system.engine.collection.state')" min-width="150"><template #default="{ row }">{{ t(`system.engine.collection.states.${metaCollectionRecordState(row)}`) }}</template></el-table-column>
        <el-table-column :label="t('system.engine.collection.expiry')" min-width="180"><template #default="{ row }">{{ formatTime(row.expires_at) }}</template></el-table-column>
        <el-table-column :label="t('common.actions')" min-width="140"><template #default="{ row }">
          <el-button v-if="canRevoke && metaCollectionRecordState(row) === 'unexpired'" link type="danger" data-testid="collection-revoke" :disabled="saving || !!revocation" @click="openRevoke(row)">{{ t('system.engine.collection.revoke') }}</el-button>
          <el-popover trigger="click" width="320"><template #reference><el-button link>{{ t('system.engine.collection.technical') }}</el-button></template>
            <dl class="technical"><dt>{{ t('system.engine.collection.id') }}</dt><dd>{{ row.id }}</dd><dt>{{ t('system.engine.collection.digest') }}</dt><dd>{{ row.task_definition_digest }}</dd><dt>{{ t('system.engine.collection.engineVersion') }}</dt><dd>{{ row.engine_version }}</dd><dt>{{ t('system.engine.collection.reason') }}</dt><dd>{{ row.revoked_reason || '—' }}</dd></dl>
          </el-popover>
        </template></el-table-column>
      </el-table>
      <el-pagination layout="prev, pager, next, total" :total="total" :page-size="10" :current-page="page" :disabled="saving || !!revocation || loading" @current-change="changePage" />
    </template>
    <el-card v-if="revocation" data-testid="collection-revocation">
      <template #header>{{ t('system.engine.collection.revoke') }} · {{ revocation.row.task_id }} · {{ revocation.row.purpose }}</template>
      <el-alert type="warning" :closable="false" :title="t('system.engine.collection.revokeBoundary')" />
      <el-form label-position="top" @submit.prevent="revoke">
        <el-form-item :label="t('system.engine.collection.reason')" required><el-input v-model="revocation.reason" type="textarea" maxlength="2000" :disabled="saving" data-testid="collection-revoke-reason" /></el-form-item>
        <div class="actions">
          <el-button :disabled="saving" @click="revocation = null">{{ t('common.cancel') }}</el-button>
          <el-button v-if="revocation.needsReview" data-testid="collection-review-revoke" :loading="loading" :disabled="saving" @click="reviewRevoke">{{ t('system.engine.collection.review') }}</el-button>
          <el-button type="danger" data-testid="collection-revoke-confirm" :loading="saving" :disabled="revocation.needsReview || !canRevoke" @click="revoke">{{ t('system.engine.collection.confirmRevoke') }}</el-button>
        </div>
      </el-form>
    </el-card>
  </section>
</template>

<script setup>
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { StatusAnnouncer } from '@common-ui'
import { enginesAPI } from '../../api/engines'
import { useAuthStore } from '../../store/auth'
import { captureMetaCollectionApproval, captureMetaCollectionFacts, captureMetaCollectionRevocation, metaCollectionRecordState } from '../../utils/metaCollectionQualification'

const props = defineProps({ engine: { type: Object, required: true }, authorizationReady: { type: Boolean, default: false } })
const auth = useAuthStore(), { t, locale } = useI18n()
const canRead = computed(() => props.authorizationReady && auth.hasPermission('system.meta_collection_qualification.read'))
const canApprove = computed(() => props.authorizationReady && auth.hasPermission('system.meta_collection_qualification.approve'))
const canRevoke = computed(() => props.authorizationReady && auth.hasPermission('system.meta_collection_qualification.revoke'))
const view = ref('records'), rows = ref([]), total = ref(0), page = ref(1)
const loading = ref(false), inspecting = ref(false), saving = ref(false), error = ref(''), success = ref('')
const taskID = ref(''), facts = ref(null), purpose = ref(''), expiresAt = ref(null), command = ref(null), revocation = ref(null)
let generation = 0, listSequence = 0, factsSequence = 0
const message = e => e?.response?.data?.error || t('system.engine.collection.failed')
const formatTime = value => new Date(value).toLocaleString(locale.value)
function invalidateFacts() { factsSequence++; facts.value = null; inspecting.value = false; error.value = ''; success.value = '' }
function changeView() { listSequence++; loading.value = false; error.value = ''; if (view.value === 'records') load() }
async function load() {
  if (!canRead.value || view.value !== 'records') return
  const epoch = generation, seq = ++listSequence
  loading.value = true; error.value = ''; rows.value = []
  try {
    const result = await enginesAPI.listMetaCollectionQualifications(props.engine.id, { page: page.value, page_size: 10 })
    if (epoch === generation && seq === listSequence) { rows.value = result.data; total.value = result.total }
  } catch (e) { if (epoch === generation && seq === listSequence) error.value = message(e) }
  finally { if (epoch === generation && seq === listSequence) loading.value = false }
}
function changePage(value) { if (!saving.value && !revocation.value) { page.value = value; load() } }
async function inspect() {
  if (!canRead.value || inspecting.value || saving.value || command.value) return
  invalidateFacts()
  const epoch = generation, seq = ++factsSequence, id = taskID.value.trim()
  if (!/^[1-9]\d{0,18}$/.test(id) || BigInt(id) > 9223372036854775807n) { error.value = t('system.engine.collection.invalid'); return }
  inspecting.value = true
  try {
    const result = await enginesAPI.getMetaCollectionTaskFacts(props.engine.id, id)
    if (epoch === generation && seq === factsSequence) {
      try { facts.value = captureMetaCollectionFacts(result, String(props.engine.id), id) }
      catch { error.value = t('system.engine.collection.unsupported') }
    }
  } catch (e) { if (epoch === generation && seq === factsSequence) error.value = message(e) }
  finally { if (epoch === generation && seq === factsSequence) inspecting.value = false }
}
async function approve() {
  if (!canApprove.value || !canRead.value || saving.value || inspecting.value || view.value !== 'create') return
  if (!command.value) {
    try { command.value = captureMetaCollectionApproval(facts.value, String(props.engine.id), taskID.value.trim(), purpose.value, expiresAt.value) }
    catch { error.value = t('system.engine.collection.invalid'); return }
  }
  const epoch = generation, sent = command.value
  saving.value = true; error.value = ''; success.value = ''
  try {
    const result = await enginesAPI.approveMetaCollection(props.engine.id, sent)
    if (epoch !== generation) return
    if (result.id !== sent.command_id || result.task_id !== sent.task_id || result.engine_id !== String(props.engine.id) || result.task_definition_digest !== sent.expected_task_definition_digest) throw new Error('unconfirmedMetaCollectionResponse')
    command.value = facts.value = null; taskID.value = purpose.value = ''; expiresAt.value = null
    success.value = t('system.engine.collection.saved'); page.value = 1; view.value = 'records'; await load()
  } catch (e) {
    if (epoch !== generation) return
    error.value = message(e)
    if (e.response?.status >= 400 && e.response.status < 500 && e.response.status !== 408) {
      command.value = null
      if (e.response.status === 409) facts.value = null
    }
  } finally { if (epoch === generation) saving.value = false }
}
function openRevoke(row) {
  if (saving.value || revocation.value || !canRevoke.value || metaCollectionRecordState(row) !== 'unexpired') return
  error.value = ''; success.value = ''; revocation.value = { row: { ...row }, reason: '', needsReview: false }
}
async function revoke() {
  if (!canRevoke.value || saving.value || !revocation.value || revocation.value.needsReview) return
  let payload
  try { payload = captureMetaCollectionRevocation(revocation.value.row, revocation.value.reason) }
  catch { error.value = t('system.engine.collection.invalidReason'); return }
  const epoch = generation, id = revocation.value.row.id
  saving.value = true; error.value = ''
  try {
    const result = await enginesAPI.revokeMetaCollection(props.engine.id, id, payload)
    if (epoch !== generation) return
    if (result.id !== id || result.status !== 'revoked' || result.engine_id !== String(props.engine.id)) throw new Error('unconfirmedMetaCollectionRevocation')
    revocation.value = null; success.value = t('system.engine.collection.revoked'); await load()
  } catch (e) {
    if (epoch === generation) { error.value = message(e); revocation.value.needsReview = true }
  } finally { if (epoch === generation) saving.value = false }
}
async function reviewRevoke() {
  if (!revocation.value || saving.value || loading.value) return
  const epoch = generation, id = revocation.value.row.id
  await load()
  if (epoch !== generation || !revocation.value || error.value) return
  const row = rows.value.find(row => row.id === id)
  if (!row || metaCollectionRecordState(row) !== 'unexpired') { error.value = t('system.engine.collection.notRevocable'); return }
  revocation.value.row = { ...row }; revocation.value.needsReview = false
}
// Engines keys this workspace by the settled engine/AuthContext identity.
// A credential-only refresh temporarily blocks interaction, not the draft.
watch(() => String(props.engine.id), () => {
  generation++; listSequence++; factsSequence++
  rows.value = []; total.value = 0; page.value = 1; facts.value = command.value = revocation.value = null
  taskID.value = purpose.value = error.value = success.value = ''; expiresAt.value = null
  loading.value = inspecting.value = saving.value = false
  view.value = canRead.value ? 'records' : 'create'; load()
}, { immediate: true })
watch(() => props.authorizationReady, ready => {
  generation++; listSequence++; factsSequence++
  loading.value = inspecting.value = saving.value = false
  if (!ready) {
    if (revocation.value) revocation.value.needsReview = true
    return
  }
  if (view.value === 'records') load()
})
onBeforeUnmount(() => { generation++ })
</script>

<style scoped>
.collection-qualifications { display: grid; gap: 16px; min-width: 0; }
.collection-qualifications :deep(.el-radio-group), .actions { display: flex; flex-wrap: wrap; gap: 8px; }
.collection-qualifications :deep(.el-radio-button__inner) { white-space: normal; }
.collection-qualifications :deep(.el-date-editor) { max-width: 100%; }
.technical { overflow-wrap: anywhere; }
.technical dd { margin: 0 0 8px; }
</style>
