<template>
  <el-card shadow="never" class="editor-card">
    <template #header>
      <div class="editor-header">
        <div>
          <strong>{{ t('catalog.edit.title') }}</strong>
          <p>{{ t('catalog.edit.description') }}</p>
        </div>
        <span>{{ t('catalog.entry.version') }} {{ form.version }}</span>
      </div>
    </template>

    <el-alert
      v-if="conflict"
      type="warning"
      :closable="false"
      show-icon
      :title="t('catalog.edit.conflict')"
      class="section-gap"
    >
      <template #default>
        <el-button size="small" @click="$emit('reload')">{{ t('catalog.edit.reloadLatest') }}</el-button>
      </template>
    </el-alert>

    <el-form label-position="top" @submit.prevent="submit">
      <el-row :gutter="16">
        <el-col :xs="24" :md="12">
          <el-form-item :label="t('catalog.entry.businessName')" required>
            <el-input v-model="form.businessName" maxlength="200" show-word-limit />
          </el-form-item>
        </el-col>
        <el-col :xs="24" :md="6">
          <el-form-item :label="t('catalog.entries.governanceStatus')" required>
            <el-select v-model="form.governanceStatus" @change="normalizeVisibility">
              <el-option
                v-for="status in governanceStatusOptions"
                :key="status"
                :label="t(`catalog.status.governance.${status}`)"
                :value="status"
              />
            </el-select>
          </el-form-item>
        </el-col>
        <el-col :xs="24" :md="6">
          <el-form-item :label="t('catalog.entries.visibility')" required>
            <el-select v-model="form.visibility">
              <el-option
                v-for="visibility in visibilityOptions"
                :key="visibility"
                :label="t(`catalog.status.visibility.${visibility}`)"
                :value="visibility"
              />
            </el-select>
          </el-form-item>
        </el-col>
      </el-row>
      <el-form-item :label="t('catalog.edit.businessDescription')" required>
        <el-input v-model="form.businessDescription" type="textarea" :rows="4" maxlength="2000" show-word-limit />
      </el-form-item>
      <el-form-item v-if="form.governanceStatus === 'deprecated' && entry.governance_status !== 'deprecated'" :label="t('catalog.edit.deprecationReason')" required>
        <el-input v-model="form.deprecationReason" type="textarea" :rows="3" maxlength="500" show-word-limit />
      </el-form-item>
      <el-form-item v-if="form.governanceStatus === 'deprecated'" :label="t('catalog.edit.recommendedSuccessor')">
        <el-select
          v-model="form.recommendedSuccessorEntryId"
          clearable
          filterable
          remote
          reserve-keyword
          :disabled="!canDeprecate"
          :loading="successorLoading"
          :remote-method="searchSuccessors"
          :placeholder="t('catalog.edit.recommendedSuccessorPlaceholder')"
          @visible-change="visible => visible && searchSuccessors('')"
        >
          <el-option
            v-for="option in successorOptions"
            :key="option.id"
            :label="successorLabel(option)"
            :value="option.id"
          />
        </el-select>
        <div class="field-hint">{{ t('catalog.edit.recommendedSuccessorHint') }}</div>
      </el-form-item>

      <section class="edit-section">
        <div class="section-title">
          <div>
            <strong>{{ t('catalog.edit.domains') }}</strong>
            <p>{{ form.ownerManagedSemantics ? t('catalog.edit.ownerDomainHint', { module: ownerModuleName }) : t('catalog.edit.dynamicCandidateHint') }}</p>
          </div>
          <el-button size="small" @click="addDomain">{{ t('catalog.edit.addDomain') }}</el-button>
        </div>
        <el-alert
          v-if="form.ownerManagedSemantics"
          type="info"
          :closable="false"
          show-icon
          class="owner-domain-alert"
          :title="form.ownerPrimaryDomainId ? t('catalog.edit.ownerPrimaryDomain', { module: ownerModuleName }) : t('catalog.edit.ownerPrimaryDomainMissing', { module: ownerModuleName })"
        />
        <div v-for="(domain, index) in form.domains" :key="`domain-${index}`" class="edit-row domain-row">
          <el-select
            v-model="domain.id"
            filterable remote reserve-keyword
            :loading="candidateState.domain.loading"
            :remote-method="search => searchCandidates('domain', search)"
            :placeholder="t('catalog.edit.domainPlaceholder')"
            @visible-change="visible => visible && searchCandidates('domain', '')"
          >
            <el-option v-for="option in candidateState.domain.options" :key="option.id" :label="candidateLabel(option)" :value="option.id" />
          </el-select>
          <el-select v-model="domain.role" :disabled="form.ownerManagedSemantics">
            <el-option :label="t('catalog.edit.primary')" value="primary" />
            <el-option :label="t('catalog.edit.secondary')" value="secondary" />
          </el-select>
          <el-button type="danger" text @click="form.domains.splice(index, 1)">{{ t('catalog.edit.remove') }}</el-button>
        </div>
        <el-empty v-if="form.domains.length === 0" :image-size="60" :description="t('catalog.edit.noDomains')" />
      </section>

      <section class="edit-section">
        <div class="section-title">
          <div>
            <strong>{{ t('catalog.edit.glossaries') }}</strong>
            <p>{{ t('catalog.edit.dynamicCandidateHint') }}</p>
          </div>
          <el-button size="small" @click="form.glossaryIDs.push('')">{{ t('catalog.edit.addGlossary') }}</el-button>
        </div>
        <div v-for="(_, index) in form.glossaryIDs" :key="`glossary-${index}`" class="edit-row id-row">
          <el-select
            v-model="form.glossaryIDs[index]"
            filterable remote reserve-keyword
            :loading="candidateState.glossary.loading"
            :remote-method="search => searchCandidates('glossary', search)"
            :placeholder="t('catalog.edit.glossaryPlaceholder')"
            @visible-change="visible => visible && searchCandidates('glossary', '')"
          >
            <el-option v-for="option in candidateState.glossary.options" :key="option.id" :label="candidateLabel(option)" :value="option.id" />
          </el-select>
          <el-button type="danger" text @click="form.glossaryIDs.splice(index, 1)">{{ t('catalog.edit.remove') }}</el-button>
        </div>
        <el-empty v-if="form.glossaryIDs.length === 0" :image-size="60" :description="t('catalog.edit.noGlossaries')" />
      </section>

      <section class="edit-section">
        <div class="section-title">
          <div>
            <strong>{{ t('catalog.edit.responsibilities') }}</strong>
            <p>{{ t('catalog.edit.responsibilityHint') }}</p>
          </div>
          <el-button size="small" @click="addResponsibility">{{ t('catalog.edit.addResponsibility') }}</el-button>
        </div>
        <div v-for="(item, index) in form.responsibilities" :key="`responsibility-${index}`" class="edit-row responsibility-row">
          <el-select v-model="item.role" @change="changeResponsibilityRole(item)">
            <el-option v-for="role in responsibilityRoles" :key="role" :label="t(`catalog.edit.role.${role}`)" :value="role" />
          </el-select>
          <el-tag>{{ t(`catalog.edit.subject.${responsibilitySubjectType(item.role)}`) }}</el-tag>
          <el-select
            v-model="item.subjectId"
            filterable remote reserve-keyword
            :loading="candidateState[responsibilitySubjectType(item.role)].loading"
            :remote-method="search => searchCandidates(responsibilitySubjectType(item.role), search)"
            :placeholder="responsibilitySubjectType(item.role) === 'department' ? t('catalog.edit.departmentPlaceholder') : t('catalog.edit.userPlaceholder')"
            @visible-change="visible => visible && searchCandidates(responsibilitySubjectType(item.role), '')"
          >
            <el-option
              v-for="option in candidateState[responsibilitySubjectType(item.role)].options"
              :key="option.id" :label="candidateLabel(option)" :value="option.id"
            />
          </el-select>
          <el-button type="danger" text @click="form.responsibilities.splice(index, 1)">{{ t('catalog.edit.remove') }}</el-button>
        </div>
        <el-empty v-if="form.responsibilities.length === 0" :image-size="60" :description="t('catalog.edit.noResponsibilities')" />
      </section>

      <section v-if="!form.ownerManagedComponents" class="edit-section">
        <div class="section-title">
          <div>
            <strong>{{ t('catalog.edit.componentElements') }}</strong>
            <p>{{ t('catalog.edit.componentElementHint') }}</p>
          </div>
        </div>
        <el-table :data="form.componentElements">
          <el-table-column prop="componentName" :label="t('catalog.entry.componentName')" min-width="220" />
          <el-table-column prop="componentStatus" :label="t('catalog.entry.componentStatus')" width="140">
            <template #default="{ row }">{{ catalogStatusLabel(t, 'catalog.status.source', row.componentStatus) }}</template>
          </el-table-column>
          <el-table-column :label="t('catalog.edit.element')" min-width="260">
            <template #default="{ row }">
              <el-select
                v-model="row.elementId"
                clearable filterable remote reserve-keyword
                :disabled="row.componentStatus !== 'active'"
                :loading="candidateState.element.loading"
                :remote-method="search => searchCandidates('element', search)"
                :placeholder="t('catalog.edit.elementPlaceholder')"
                @visible-change="visible => visible && searchCandidates('element', '')"
              >
                <el-option v-for="option in candidateState.element.options" :key="option.id" :label="candidateLabel(option)" :value="option.id" />
              </el-select>
            </template>
          </el-table-column>
        </el-table>
      </section>

      <div class="editor-actions">
        <el-button @click="$emit('cancel')">{{ t('catalog.edit.cancel') }}</el-button>
        <el-button type="primary" :loading="saving" :disabled="conflict" @click="submit">{{ t('catalog.edit.save') }}</el-button>
      </div>
    </el-form>
  </el-card>
</template>

<script setup>
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { listEntries, listReferenceCandidates } from '../api/catalog'
import { catalogStatusLabel } from '../utils/catalogStatusLabel'
import {
  buildEntryEditForm,
  buildUpdatePayload,
  governanceOptions,
  hasEffectivePrimaryDomain,
  isCanonicalPositiveID,
  isCanonicalUUID,
  responsibilitySubjectType
} from '../utils/entryEdit'

const props = defineProps({
  entry: { type: Object, required: true },
  saving: { type: Boolean, default: false },
  conflict: { type: Boolean, default: false },
  canCertify: { type: Boolean, default: false },
  canDeprecate: { type: Boolean, default: false }
})
const emit = defineEmits(['submit', 'cancel', 'reload'])
const { t } = useI18n()
const form = reactive(buildEntryEditForm(props.entry))
const successorOptions = ref(initialSuccessorOptions(props.entry))
const successorLoading = ref(false)
let successorRequestVersion = 0
const candidateTypes = ['domain', 'glossary', 'element', 'department', 'user']
const candidateState = reactive(Object.fromEntries(candidateTypes.map(type => [type, { options: [], loading: false, version: 0 }])))
const responsibilityRoles = ['accountable_department', 'business_owner', 'data_steward', 'technical_owner']
const ownerModuleName = computed(() => ({ model: 'Model', standard: 'Standard', service: 'Service', develop: 'Develop' }[form.ownerModule] || ''))

watch(() => props.entry, entry => {
  Object.assign(form, buildEntryEditForm(entry))
  successorOptions.value = initialSuccessorOptions(entry)
  resetCandidateOptions(entry)
}, { deep: true })

resetCandidateOptions(props.entry)

const governanceStatusOptions = computed(() => governanceOptions(
  props.entry.governance_status,
  props.canCertify,
  props.canDeprecate
))
const visibilityOptions = computed(() => {
  if (form.governanceStatus === 'discovered') return ['inventory']
  if (form.governanceStatus === 'certified') return ['department', 'tenant']
  return ['inventory', 'department', 'tenant']
})

function normalizeVisibility() {
  if (!visibilityOptions.value.includes(form.visibility)) form.visibility = visibilityOptions.value[0]
  if (form.governanceStatus !== 'deprecated') form.recommendedSuccessorEntryId = ''
}

function initialSuccessorOptions(entry) {
  if (entry?.recommended_successor) return [entry.recommended_successor]
  if (entry?.recommended_successor_entry_id) {
    return [{ id: entry.recommended_successor_entry_id, display_name: t('catalog.edit.referenceUnavailable') }]
  }
  return []
}

function successorLabel(option) {
  const name = option.display_name || t('catalog.entries.unnamed')
  return option.governance_status ? `${name} · ${t(`catalog.status.governance.${option.governance_status}`)}` : name
}

function resetCandidateOptions(entry) {
  for (const type of candidateTypes) candidateState[type].options = []
  for (const link of entry?.semantic_links || []) {
    if (link.semantic_type === 'domain' || link.semantic_type === 'glossary') {
      mergeCandidate(link.semantic_type, historicalCandidate(link.semantic_type, link.semantic_id, link.observed_snapshot))
    }
  }
  for (const item of entry?.responsibilities || []) {
    if (item.status === 'active' && (item.subject_type === 'department' || item.subject_type === 'user')) {
      mergeCandidate(item.subject_type, historicalCandidate(item.subject_type, item.subject_id, item.observed_snapshot))
    }
  }
  for (const item of entry?.component_elements || []) {
    mergeCandidate('element', historicalCandidate('element', item.element_id, item.observed_snapshot))
  }
}

function historicalCandidate(referenceType, id, snapshot = {}) {
  return {
    reference_type: referenceType,
    id: String(id),
    name: snapshot?.name || t('catalog.edit.referenceUnavailable'),
    code: snapshot?.code || '',
    status: snapshot?.status || ''
  }
}

function mergeCandidate(type, candidate) {
  const options = new Map(candidateState[type].options.map(item => [item.id, item]))
  options.set(String(candidate.id), { ...candidate, id: String(candidate.id) })
  candidateState[type].options = [...options.values()]
}

async function searchCandidates(type, search = '') {
  if (!candidateState[type]) return
  const state = candidateState[type]
  const version = ++state.version
  state.loading = true
  try {
    const response = await listReferenceCandidates({
      reference_type: type,
      ...(String(search || '').trim() ? { search: String(search).trim() } : {}),
      page: 1,
      page_size: 20
    })
    if (version !== state.version) return
    const selected = new Set(selectedCandidateIDs(type))
    const preserved = state.options.filter(item => selected.has(item.id))
    const options = new Map(preserved.map(item => [item.id, item]))
    for (const item of response.data || []) options.set(String(item.id), { ...item, id: String(item.id) })
    state.options = [...options.values()]
  } catch (error) {
    if (version === state.version) ElMessage.error(error?.response?.data?.error || t('catalog.edit.candidateSearchFailed'))
  } finally {
    if (version === state.version) state.loading = false
  }
}

function selectedCandidateIDs(type) {
  if (type === 'domain') return form.domains.map(item => String(item.id || '')).filter(Boolean)
  if (type === 'glossary') return form.glossaryIDs.map(item => String(item || '')).filter(Boolean)
  if (type === 'element') return form.componentElements.map(item => String(item.elementId || '')).filter(Boolean)
  return form.responsibilities
    .filter(item => responsibilitySubjectType(item.role) === type)
    .map(item => String(item.subjectId || '')).filter(Boolean)
}

function candidateLabel(option) {
  return option.code ? `${option.name} · ${option.code}` : option.name
}

async function searchSuccessors(search = '') {
  if (!props.canDeprecate) return
  const version = ++successorRequestVersion
  successorLoading.value = true
  try {
    const query = String(search || '').trim()
    const responses = await Promise.all(['curated', 'certified'].map(governanceStatus => listEntries({
      ...(query ? { search: query } : {}),
      source_status: 'active',
      governance_status: governanceStatus,
      page: 1,
      page_size: 20
    })))
    if (version !== successorRequestVersion) return
    const options = new Map(initialSuccessorOptions(props.entry).map(item => [item.id, item]))
    for (const response of responses) {
      for (const candidate of response.data || []) {
        if (candidate.id !== props.entry.id) options.set(candidate.id, candidate)
      }
    }
    successorOptions.value = [...options.values()]
  } catch (error) {
    if (version === successorRequestVersion) {
      ElMessage.error(error?.response?.data?.error || t('catalog.edit.successorSearchFailed'))
    }
  } finally {
    if (version === successorRequestVersion) successorLoading.value = false
  }
}

function addDomain() {
  form.domains.push({
    id: '',
    role: form.ownerManagedSemantics || form.domains.some(item => item.role === 'primary') ? 'secondary' : 'primary'
  })
}

function addResponsibility() {
  form.responsibilities.push({ role: 'data_steward', subjectId: '' })
}

function changeResponsibilityRole(item) {
  item.subjectId = ''
}

function submit() {
  if (!validateForm()) return
  emit('submit', buildUpdatePayload(form))
}

function validateForm() {
  const allIDs = [
    ...form.domains.map(item => item.id),
    ...form.glossaryIDs,
    ...form.responsibilities.map(item => item.subjectId),
    ...form.componentElements.filter(item => item.elementId !== null && item.elementId !== '').map(item => item.elementId)
  ]
  if (allIDs.some(id => !isCanonicalPositiveID(id))) {
    ElMessage.error(t('catalog.edit.invalidId'))
    return false
  }
  if (new Set(form.domains.map(item => String(item.id))).size !== form.domains.length ||
      new Set(form.glossaryIDs.map(String)).size !== form.glossaryIDs.length) {
    ElMessage.error(t('catalog.edit.duplicateReference'))
    return false
  }
  if ((!form.ownerManagedSemantics && form.domains.filter(item => item.role === 'primary').length > 1) ||
      (form.ownerManagedSemantics && form.domains.some(item => item.role === 'primary'))) {
    ElMessage.error(t('catalog.edit.multiplePrimaryDomains'))
    return false
  }
  if (form.governanceStatus !== 'discovered') {
    const roleCounts = Object.fromEntries(responsibilityRoles.map(role => [role, form.responsibilities.filter(item => item.role === role).length]))
    if (!form.businessName.trim() || !form.businessDescription.trim() ||
        !hasEffectivePrimaryDomain(form) ||
        roleCounts.accountable_department !== 1 || roleCounts.business_owner !== 1 || roleCounts.data_steward < 1) {
      ElMessage.error(t('catalog.edit.curationIncomplete'))
      return false
    }
  }
  if (form.governanceStatus === 'deprecated' && props.entry.governance_status !== 'deprecated' && !form.deprecationReason.trim()) {
    ElMessage.error(t('catalog.edit.deprecationReasonRequired'))
    return false
  }
  if (form.recommendedSuccessorEntryId && !isCanonicalUUID(form.recommendedSuccessorEntryId)) {
    ElMessage.error(t('catalog.edit.invalidSuccessor'))
    return false
  }
  return true
}
</script>

<style scoped>
.editor-card { margin-bottom: 16px; }
.editor-header, .section-title, .editor-actions { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; }
.editor-header p, .section-title p { margin: 4px 0 0; color: var(--addp-text-secondary); font-size: 13px; }
.editor-header > span { color: var(--addp-text-secondary); font-size: 13px; }
.section-gap, .edit-section { margin-bottom: 20px; }
.owner-domain-alert { margin-bottom: 12px; }
.field-hint { color: var(--addp-text-secondary); font-size: 12px; margin-top: 6px; }
.edit-section { border-top: 1px solid var(--el-border-color-lighter); padding-top: 18px; }
.section-title { margin-bottom: 12px; }
.edit-row { display: grid; align-items: center; gap: 12px; margin-bottom: 10px; }
.domain-row { grid-template-columns: minmax(180px, 1fr) 180px auto; }
.id-row { grid-template-columns: minmax(180px, 1fr) auto; }
.responsibility-row { grid-template-columns: minmax(190px, 1fr) auto minmax(180px, 1fr) auto; }
.editor-actions { justify-content: flex-end; margin-top: 24px; }
.editor-card :deep(.el-select) { width: 100%; }
@media (max-width: 760px) {
  .domain-row, .id-row, .responsibility-row { grid-template-columns: 1fr; }
}
</style>
