<template>
  <div class="glossary-detail" v-loading="loading">
    <div class="page-header">
      <div class="header-left"><el-button :icon="ArrowLeft" @click="goBack">{{ $t('standard.common.back') }}</el-button><h2>{{ title }}</h2><el-tag :type="statusType(revision.status)">{{ statusLabel(revision.status) }}</el-tag><el-tag v-if="isDirty" type="warning">{{ $t('standard.common.unsaved') }}</el-tag></div>
      <div v-if="!loadError && glossary.id" class="header-right">
        <el-button v-if="!glossary.draft_revision && canUpdate" @click="newDraft">{{ $t('standard.glossary.createRevision') }}</el-button>
        <el-button v-if="editable" type="primary" :loading="savingRevision" :disabled="savingIdentity || savingElements" @click="saveRevision">{{ $t('standard.common.save') }}</el-button>
        <el-button v-if="editable" type="warning" @click="runRevisionAction('submit')">{{ $t('standard.revision.submit') }}</el-button>
        <el-button v-if="reviewing && canPublish" @click="runRevisionAction('return')">{{ $t('standard.revision.return') }}</el-button>
        <el-button v-if="reviewing && canPublish" type="success" @click="runRevisionAction('publish')">{{ $t('standard.revision.publish') }}</el-button>
        <el-button v-if="revision.status === 'published' && canPublish" type="danger" @click="runRevisionAction('withdraw')">{{ $t('standard.revision.withdraw') }}</el-button>
      </div>
    </div>

    <el-alert v-if="loadError" :title="loadError" type="error" :closable="false" />
    <el-row v-else :gutter="20">
      <el-col :span="16">
        <el-card class="section-card">
          <template #header><div class="card-header"><h3>{{ $t('standard.glossary.identityInfo') }}</h3><el-button v-if="canUpdate && glossary.id" type="primary" size="small" :loading="savingIdentity" :disabled="savingRevision || savingElements" @click="saveIdentity">{{ $t('standard.common.save') }}</el-button></div></template>
          <el-form label-width="120px" :disabled="!canUpdate || saving">
            <el-form-item :label="$t('standard.common.code')"><el-input :model-value="glossary.code" disabled /></el-form-item>
            <el-form-item :label="$t('standard.common.scopeLabel')">
              <el-select v-model="identity.scope_type" style="width:100%" @change="onScopeChange"><el-option :label="$t('standard.common.scopeValue.tenant_common')" value="tenant_common" /><el-option :label="$t('standard.common.scopeValue.domain')" value="domain" /></el-select>
            </el-form-item>
            <el-form-item v-if="identity.scope_type === 'domain'" :label="$t('standard.glossary.domainLabel')"><BusinessDomainSelect v-model="identity.owner_domain_id" style="width:100%" :options="domains" /></el-form-item>
            <el-form-item :label="$t('standard.common.tags')"><el-select v-model="identity.tags" multiple filterable allow-create default-first-option style="width:100%" /></el-form-item>
          </el-form>
        </el-card>

        <el-card class="section-card">
          <template #header><div class="card-header"><h3>{{ $t('standard.glossary.revisionInfo') }}</h3><span v-if="revision.revision_no">R{{ revision.revision_no }}</span></div></template>
          <el-form label-width="120px" :disabled="!editable || saving">
            <el-form-item :label="$t('standard.glossary.nameLabel')"><el-input v-model="revision.name" /></el-form-item>
            <el-form-item :label="$t('standard.glossary.aliasLabel')"><el-select v-model="revision.alias" multiple filterable allow-create default-first-option style="width:100%" /></el-form-item>
            <el-form-item :label="$t('standard.glossary.definitionLabel')"><el-input v-model="revision.definition" type="textarea" :rows="4" /></el-form-item>
            <el-form-item :label="$t('standard.glossary.exampleLabel')"><el-input v-model="revision.example" type="textarea" :rows="2" /></el-form-item>
            <el-form-item :label="$t('standard.glossary.noteLabel')"><el-input v-model="revision.note" type="textarea" :rows="2" /></el-form-item>
            <el-form-item :label="$t('standard.revision.changeSummary')"><el-input v-model="revision.change_summary" /></el-form-item>
            <el-form-item :label="$t('standard.revision.effectiveFrom')"><el-date-picker v-model="revision.effective_from" type="datetime" value-format="YYYY-MM-DDTHH:mm:ssZ" style="width:100%" /></el-form-item>
            <el-form-item :label="$t('standard.revision.effectiveTo')"><el-date-picker v-model="revision.effective_to" type="datetime" value-format="YYYY-MM-DDTHH:mm:ssZ" clearable style="width:100%" /></el-form-item>
          </el-form>
        </el-card>

        <el-card class="section-card">
          <template #header><div class="card-header"><h3>{{ $t('standard.glossary.relatedElements') }}</h3><el-button v-if="canUpdate" size="small" :disabled="saving || elementsLoading || Boolean(elementsError)" @click="openElementDialog">{{ $t('standard.glossary.manageElements') }}</el-button></div></template>
          <div v-if="elementsError">
            <el-alert :title="$t('standard.glossary.mappingRefreshFailed')" :description="elementsError" type="error" :closable="false" />
            <el-button :loading="elementsLoading" @click="refreshMappedElements()">{{ $t('common.refresh') }}</el-button>
          </div>
          <el-empty v-else-if="mappedElements.length === 0" v-loading="elementsLoading" :description="$t('standard.glossary.noElements')" />
          <el-table v-else v-loading="elementsLoading" :data="mappedElements" size="small">
            <el-table-column prop="name" :label="$t('standard.common.name')" min-width="140">
              <template #default="{ row }">{{ row.name }} <el-tag v-if="row.lifecycle_state !== 'active'" type="warning" size="small">{{ $t('standard.glossary.identityUnavailable') }}</el-tag></template>
            </el-table-column>
            <el-table-column prop="code" :label="$t('standard.common.code')" min-width="140" />
            <el-table-column :label="$t('standard.glossary.displayRevision')" width="120"><template #default="{ row }">{{ row.revision_no ? `R${row.revision_no}` : '-' }}</template></el-table-column>
            <el-table-column :label="$t('standard.common.status')" min-width="120"><template #default="{ row }">{{ statusLabel(row.status) }}</template></el-table-column>
            <el-table-column :label="$t('standard.glossary.effectiveness')" min-width="160"><template #default="{ row }"><el-tag :type="row.is_effective ? 'success' : 'info'" size="small">{{ $t(row.is_effective ? 'standard.glossary.effective' : 'standard.glossary.noEffectiveRevision') }}</el-tag></template></el-table-column>
          </el-table>
        </el-card>

        <DocumentPanel v-if="glossary.id" entity-type="glossary" :entity-id="glossary.id" v-model:entity-version="glossary.version" />
      </el-col>

      <el-col :span="8">
        <el-card class="section-card">
          <template #header><h3>{{ $t('standard.glossary.revisionHistory') }}</h3></template>
          <el-table :data="history" size="small" @row-click="selectRevision"><el-table-column prop="revision_no" label="R" width="55" /><el-table-column :label="$t('standard.common.status')"><template #default="{ row }"><el-tag size="small" :type="statusType(row.status)">{{ statusLabel(row.status) }}</el-tag></template></el-table-column></el-table>
        </el-card>
        <el-card class="section-card">
          <template #header><h3>{{ $t('standard.common.metadata') }}</h3></template>
          <el-descriptions :column="1" size="small"><el-descriptions-item :label="$t('standard.common.id')">{{ glossary.id }}</el-descriptions-item><el-descriptions-item :label="$t('standard.common.createdAt')">{{ formatTime(glossary.created_at) }}</el-descriptions-item><el-descriptions-item :label="$t('standard.common.updatedAt')">{{ formatTime(glossary.updated_at) }}</el-descriptions-item></el-descriptions>
        </el-card>
      </el-col>
    </el-row>

    <el-dialog v-model="elementDialog" :title="$t('standard.glossary.manageElements')" width="620px" :show-close="!savingElements" :close-on-click-modal="!savingElements" :close-on-press-escape="!savingElements">
      <el-select v-model="selectedElementIDs" multiple filterable remote :remote-method="searchElements" :loading="elementSearchLoading" :disabled="saving" style="width:100%" @change="retainSelectedElements">
        <el-option v-for="item in elementOptions" :key="item.id" :label="`${elementName(item)} (${item.code})`" :value="item.id" />
      </el-select>
      <template #footer><el-button :disabled="savingElements" @click="elementDialog=false">{{ $t('standard.common.cancel') }}</el-button><el-button type="primary" :loading="savingElements" :disabled="savingIdentity || savingRevision" @click="saveElements">{{ $t('standard.common.confirm') }}</el-button></template>
    </el-dialog>
  </div>
</template>

<script setup>
import { BusinessDomainSelect, buildBusinessDomainOptions, createLatestRequestCoordinator } from '@common-ui'
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ArrowLeft } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { useConsolePageDescriptor } from '@common-ui'
import { domainAPI, elementAPI, glossaryAPI } from '../api/standard'
import DocumentPanel from '../components/DocumentPanel.vue'
import { navigateStandardRoute } from '@/utils/moduleNavigation'
import { getStandardErrorMessage, isCanceledInteraction } from '../utils/apiError'
import { formatStandardDateTime } from '../utils/dateTime'
import { useStandardPermissions } from '../composables/useStandardPermissions'
import { useUnsavedChanges } from '../composables/useUnsavedChanges'
import { buildGlossaryRevisionLocation } from '../utils/glossaryRouteState'

const { t, locale } = useI18n()
const { canUpdate, canPublish } = useStandardPermissions('glossary')
const route = useRoute(), router = useRouter()
const loading = ref(false), glossary = ref({}), history = ref([]), revision = reactive({}), domains = ref([])
const savingIdentity = ref(false), savingRevision = ref(false)
const savingElements = ref(false)
const saving = computed(() => savingIdentity.value || savingRevision.value || savingElements.value)
const mappedElements = ref([]), elementDialog = ref(false), selectedElementIDs = ref([]), searchedElements = ref([]), elementSearchLoading = ref(false)
const selectedElements = ref([])
const elementOptions = computed(() => [...new Map([...selectedElements.value, ...searchedElements.value].map(item => [item.id, item])).values()])
const elementsLoading = ref(false), elementsError = ref('')
const elementListRequests = createLatestRequestCoordinator()
const elementSearchRequests = createLatestRequestCoordinator()
const identity = reactive({ scope_type: 'tenant_common', owner_domain_id: null, tags: [] })
const loadError = ref('')
const detailRequests = createLatestRequestCoordinator()
const detailTarget = computed(() => JSON.stringify([route.params.id, route.query.revision_id]))
const editable = computed(() => canUpdate.value && revision.status === 'draft' && glossary.value.draft_revision_id === revision.id)
const reviewing = computed(() => revision.status === 'in_review' && glossary.value.draft_revision_id === revision.id)
const title = computed(() => revision.name || glossary.value.code || t('standard.glossary.detailTitle'))
useConsolePageDescriptor(router, 'standard', { title: computed(() => t('standard.glossary.recentVisitTitle')), subject: title, ready: computed(() => Boolean(glossary.value.id)) })
const statusType = status => ({ draft: 'info', in_review: 'warning', published: 'success', withdrawn: 'danger' }[status] || 'info')
const statusLabel = status => status ? t(`standard.revision.status.${status}`) : '-'
const formatTime = value => formatStandardDateTime(value, locale.value)
const elementName = item => item.draft_revision?.name || item.current_revision?.name || item.name || item.code
const editableState = computed(() => ({ identity: { ...identity, tags: [...identity.tags] }, revision: { ...revision, alias: [...(revision.alias || [])], related_ids: [...(revision.related_ids || [])] } }))
const { isDirty, markSaved } = useUnsavedChanges({ state: editableState })
const setRevision = value => { Object.keys(revision).forEach(key => delete revision[key]); Object.assign(revision, JSON.parse(JSON.stringify(value || {}))); revision.alias ||= []; revision.related_ids ||= [] }
const goBack = () => {
  const { revision_id, ...query } = route.query
  return navigateStandardRoute(router, { path: '/glossaries', query }, { history: 'replace' })
}
const onScopeChange = value => { if (value !== 'domain') identity.owner_domain_id = null }

async function load() {
  const request = detailRequests.begin(detailTarget.value)
  const glossaryID = route.params.id
  const requestedRevision = route.query.revision_id
  loading.value = true
  loadError.value = ''
  elementListRequests.invalidate()
  elementSearchRequests.invalidate()
  elementDialog.value = false
  elementsLoading.value = false
  elementsError.value = ''
  glossary.value = {}; history.value = []; mappedElements.value = []
  setRevision(null)
  markSaved()
  try {
    if (requestedRevision !== undefined) buildGlossaryRevisionLocation(glossaryID, requestedRevision)
    const [aggregate, revisions, elements] = await Promise.all([glossaryAPI.get(glossaryID), glossaryAPI.listRevisions(glossaryID), glossaryAPI.getElements(glossaryID)])
    if (!detailRequests.isCurrent(request, detailTarget.value)) return
    const selectedID = requestedRevision ?? (aggregate.draft_revision || aggregate.current_revision || revisions?.[0])?.id
    const selectedRevision = selectedID ? await glossaryAPI.getRevision(glossaryID, selectedID) : null
    if (!detailRequests.isCurrent(request, detailTarget.value)) return
    glossary.value = aggregate; history.value = revisions || []; mappedElements.value = elements || []
    Object.assign(identity, { scope_type: aggregate.scope_type, owner_domain_id: aggregate.owner_domain_id || null, tags: aggregate.tags || [] })
    setRevision(selectedRevision)
    markSaved()
  } catch (error) {
    if (detailRequests.isCurrent(request, detailTarget.value)) loadError.value = getStandardErrorMessage(error, t, 'standard.common.loadFailed')
  } finally { if (detailRequests.isCurrent(request, detailTarget.value)) loading.value = false }
}
async function saveIdentity() {
  if (saving.value || !canUpdate.value || !glossary.value.id) return
  if (identity.scope_type === 'domain' && !identity.owner_domain_id) { ElMessage.warning(t('standard.common.selectDomain')); return }
  savingIdentity.value = true
  const request = detailRequests.begin(detailTarget.value)
  try {
    const aggregate = await glossaryAPI.update(glossary.value.id, { ...editableState.value.identity, version: glossary.value.version })
    if (!detailRequests.isCurrent(request, detailTarget.value)) return
    glossary.value = aggregate
    Object.assign(identity, { scope_type: aggregate.scope_type, owner_domain_id: aggregate.owner_domain_id || null, tags: aggregate.tags || [] })
    markSaved('identity')
    ElMessage.success(t('standard.common.saveSuccess'))
  } catch (error) {
    if (detailRequests.isCurrent(request, detailTarget.value)) ElMessage.error(getStandardErrorMessage(error, t, 'standard.common.saveFailed'))
  } finally { savingIdentity.value = false }
}
async function saveRevision() {
  if (saving.value || !editable.value) return
  if (!revision.name?.trim() || !revision.definition?.trim() || !revision.change_summary?.trim()) { ElMessage.warning(t('standard.glossary.revisionRequired')); return }
  savingRevision.value = true
  const request = detailRequests.begin(detailTarget.value)
  const glossaryID = glossary.value.id
  const revisionID = revision.id
  const revisionPayload = JSON.parse(JSON.stringify(revision))
  try {
    const aggregate = await glossaryAPI.updateRevision(glossaryID, revisionID, { ...revisionPayload, version: glossary.value.version })
    if (!detailRequests.isCurrent(request, detailTarget.value)) return
    glossary.value = aggregate; setRevision(aggregate.draft_revision); markSaved('revision')
    history.value = history.value.map(item => item.id === revisionID ? aggregate.draft_revision : item)
    ElMessage.success(t('standard.common.saveSuccess'))
  } catch (error) {
    if (detailRequests.isCurrent(request, detailTarget.value)) ElMessage.error(getStandardErrorMessage(error, t, 'standard.common.saveFailed'))
  }
  finally { savingRevision.value = false }
}
async function newDraft() {
  if (isDirty.value || saving.value) { ElMessage.warning(t('standard.common.saveBeforeAction')); return }
  const target = detailTarget.value
  try {
    const { value } = await ElMessageBox.prompt(t('standard.revision.changeSummary'), t('standard.glossary.createRevision'), { inputValidator: value => Boolean(value?.trim()) })
    if (target !== detailTarget.value) return
    const aggregate = await glossaryAPI.createRevision(glossary.value.id, { version: glossary.value.version, change_summary: value.trim() })
    if (target !== detailTarget.value) return
    await selectRevision(aggregate.draft_revision)
  }
  catch (error) { if (!isCanceledInteraction(error)) ElMessage.error(getStandardErrorMessage(error, t)) }
}
async function runRevisionAction(action) {
  if (isDirty.value || saving.value) { ElMessage.warning(t('standard.common.saveBeforeAction')); return }
  try { await ElMessageBox.confirm(t(`standard.revision.confirm.${action}`), t('standard.common.hint'), { type: 'warning' }); await glossaryAPI[`${action}Revision`](glossary.value.id, revision.id, glossary.value.version); await load() }
  catch (error) { if (!isCanceledInteraction(error)) ElMessage.error(getStandardErrorMessage(error, t)) }
}
function selectRevision(row) {
  if (row.id === revision.id) return
  return navigateStandardRoute(router, buildGlossaryRevisionLocation(glossary.value.id, row.id, route.query), { history: 'replace' })
}
async function searchElements(keyword='') {
  const request = elementSearchRequests.begin(detailTarget.value)
  elementSearchLoading.value = true
  try {
    const result = await elementAPI.list({ keyword, page_size: 50 })
    if (elementSearchRequests.isCurrent(request, detailTarget.value)) replaceSearchedElements(result.data || [])
  } catch {
    if (elementSearchRequests.isCurrent(request, detailTarget.value)) replaceSearchedElements([])
  } finally {
    if (elementSearchRequests.isCurrent(request, detailTarget.value)) elementSearchLoading.value = false
  }
}
function retainSelectedElements() {
  const selectedIDs = new Set(selectedElementIDs.value)
  selectedElements.value = elementOptions.value.filter(item => selectedIDs.has(item.id))
}
function replaceSearchedElements(elements) {
  retainSelectedElements()
  searchedElements.value = elements
}
function openElementDialog() {
  if (saving.value || elementsLoading.value || elementsError.value || !canUpdate.value) return
  selectedElementIDs.value = mappedElements.value.map(item => item.id)
  selectedElements.value = [...mappedElements.value]
  searchedElements.value = []
  elementDialog.value = true
  searchElements()
}
async function refreshMappedElements() {
  if (elementsLoading.value || !glossary.value.id) return
  const request = elementListRequests.begin(detailTarget.value)
  elementsLoading.value = true
  try {
    const elements = await glossaryAPI.getElements(glossary.value.id)
    if (!elementListRequests.isCurrent(request, detailTarget.value)) return
    mappedElements.value = elements || []
    elementsError.value = ''
  } catch (error) {
    if (elementListRequests.isCurrent(request, detailTarget.value)) elementsError.value = getStandardErrorMessage(error, t, 'standard.common.loadFailed')
  } finally {
    if (elementListRequests.isCurrent(request, detailTarget.value)) elementsLoading.value = false
  }
}
async function saveElements() {
  if (saving.value || !canUpdate.value || !elementDialog.value) return
  savingElements.value = true
  const request = detailRequests.begin(detailTarget.value)
  try {
    const aggregate = await glossaryAPI.updateElements(glossary.value.id, { version: glossary.value.version, element_ids: [...selectedElementIDs.value] })
    if (!detailRequests.isCurrent(request, detailTarget.value)) return
    glossary.value.version = aggregate.version
    elementDialog.value = false
    ElMessage.success(t('standard.common.saveSuccess'))
    await refreshMappedElements()
  } catch (error) {
    if (detailRequests.isCurrent(request, detailTarget.value)) ElMessage.error(getStandardErrorMessage(error, t, 'standard.common.saveFailed'))
  } finally { savingElements.value = false }
}
watch(elementDialog, open => {
  if (!open) {
    elementSearchRequests.invalidate()
    elementSearchLoading.value = false
  }
})
watch(() => [route.params.id, route.query.revision_id], load, { immediate: true })
onBeforeUnmount(() => {
  detailRequests.invalidate()
  elementListRequests.invalidate()
  elementSearchRequests.invalidate()
})
onMounted(async () => { try { domains.value = buildBusinessDomainOptions(await domainAPI.list() || []) } catch { domains.value = [] } })
</script>

<style scoped>
.glossary-detail { padding:20px; }
.page-header, .header-left, .header-right, .card-header { display:flex; align-items:center; gap:12px; }
.page-header, .card-header { justify-content:space-between; }
.page-header { margin-bottom:20px; }
.header-left h2, .card-header h3 { margin:0; color:var(--addp-text-primary); }
.section-card { margin-bottom:20px; }
@media (max-width:768px) { .glossary-detail { padding:12px; } .page-header { align-items:flex-start; flex-wrap:wrap; } .glossary-detail :deep(.el-col) { max-width:100%; flex:0 0 100%; } }
</style>
