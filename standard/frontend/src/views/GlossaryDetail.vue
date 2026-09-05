<template>
  <div class="glossary-detail" v-loading="loading">
    <div class="page-header">
      <div class="header-left"><el-button :icon="ArrowLeft" @click="goBack">{{ $t('standard.common.back') }}</el-button><h2>{{ title }}</h2><el-tag :type="statusType(revision.status)">{{ statusLabel(revision.status) }}</el-tag><el-tag v-if="isDirty" type="warning">{{ $t('standard.common.unsaved') }}</el-tag></div>
      <div class="header-right">
        <el-button v-if="!glossary.draft_revision && canUpdate" @click="newDraft">{{ $t('standard.glossary.createRevision') }}</el-button>
        <el-button v-if="editable" type="primary" :loading="saving" @click="saveAll">{{ $t('standard.common.save') }}</el-button>
        <el-button v-if="editable" type="warning" @click="runRevisionAction('submit')">{{ $t('standard.revision.submit') }}</el-button>
        <el-button v-if="reviewing && canPublish" @click="runRevisionAction('return')">{{ $t('standard.revision.return') }}</el-button>
        <el-button v-if="reviewing && canPublish" type="success" @click="runRevisionAction('publish')">{{ $t('standard.revision.publish') }}</el-button>
        <el-button v-if="revision.status === 'published' && canPublish" type="danger" @click="runRevisionAction('withdraw')">{{ $t('standard.revision.withdraw') }}</el-button>
      </div>
    </div>

    <el-row :gutter="20">
      <el-col :span="16">
        <el-card class="section-card">
          <template #header><h3>{{ $t('standard.glossary.identityInfo') }}</h3></template>
          <el-form label-width="120px" :disabled="!canUpdate">
            <el-form-item :label="$t('standard.common.code')"><el-input :model-value="glossary.code" disabled /></el-form-item>
            <el-form-item :label="$t('standard.common.scopeLabel')">
              <el-select v-model="identity.scope_type" style="width:100%" @change="onScopeChange"><el-option :label="$t('standard.common.scopeValue.tenant_common')" value="tenant_common" /><el-option :label="$t('standard.common.scopeValue.domain')" value="domain" /></el-select>
            </el-form-item>
            <el-form-item v-if="identity.scope_type === 'domain'" :label="$t('standard.glossary.domainLabel')"><el-select v-model="identity.owner_domain_id" filterable style="width:100%"><el-option v-for="domain in domains" :key="domain.id" :label="domain.name" :value="domain.id" /></el-select></el-form-item>
            <el-form-item :label="$t('standard.common.tags')"><el-select v-model="identity.tags" multiple filterable allow-create default-first-option style="width:100%" /></el-form-item>
          </el-form>
        </el-card>

        <el-card class="section-card">
          <template #header><div class="card-header"><h3>{{ $t('standard.glossary.revisionInfo') }}</h3><span v-if="revision.revision_no">R{{ revision.revision_no }}</span></div></template>
          <el-form label-width="120px" :disabled="!editable">
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
          <template #header><div class="card-header"><h3>{{ $t('standard.glossary.relatedElements') }}</h3><el-button v-if="canUpdate" size="small" @click="openElementDialog">{{ $t('standard.glossary.manageElements') }}</el-button></div></template>
          <el-empty v-if="mappedElements.length === 0" :description="$t('standard.glossary.noElements')" />
          <el-table v-else :data="mappedElements" size="small"><el-table-column prop="name" :label="$t('standard.common.name')" /><el-table-column prop="code" :label="$t('standard.common.code')" /><el-table-column prop="revision_no" label="Revision" width="100" /></el-table>
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

    <el-dialog v-model="elementDialog" :title="$t('standard.glossary.manageElements')" width="620px">
      <el-select v-model="selectedElementIDs" multiple filterable remote :remote-method="searchElements" :loading="elementSearchLoading" style="width:100%">
        <el-option v-for="item in searchedElements" :key="item.id" :label="`${elementName(item)} (${item.code})`" :value="item.id" />
      </el-select>
      <template #footer><el-button @click="elementDialog=false">{{ $t('standard.common.cancel') }}</el-button><el-button type="primary" @click="saveElements">{{ $t('standard.common.confirm') }}</el-button></template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref, watch } from 'vue'
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

const { t, locale } = useI18n()
const { canUpdate, canPublish } = useStandardPermissions('glossary')
const route = useRoute(), router = useRouter()
const loading = ref(false), saving = ref(false), glossary = ref({}), history = ref([]), revision = reactive({}), domains = ref([])
const mappedElements = ref([]), elementDialog = ref(false), selectedElementIDs = ref([]), searchedElements = ref([]), elementSearchLoading = ref(false)
const identity = reactive({ scope_type: 'tenant_common', owner_domain_id: null, steward_id: null, tags: [] })
const editable = computed(() => canUpdate.value && revision.status === 'draft' && glossary.value.draft_revision_id === revision.id)
const reviewing = computed(() => revision.status === 'in_review' && glossary.value.draft_revision_id === revision.id)
const title = computed(() => revision.name || glossary.value.code || t('standard.glossary.detailTitle'))
useConsolePageDescriptor(router, 'standard', { title: computed(() => t('standard.glossary.recentVisitTitle')), subject: title, ready: computed(() => Boolean(glossary.value.id)) })
const statusType = status => ({ draft: 'info', in_review: 'warning', published: 'success', withdrawn: 'danger' }[status] || 'info')
const statusLabel = status => status ? t(`standard.revision.status.${status}`) : '-'
const formatTime = value => formatStandardDateTime(value, locale.value)
const flattenDomains = nodes => nodes.flatMap(node => [node, ...flattenDomains(node.children || [])])
const elementName = item => item.draft_revision?.name || item.current_revision?.name || item.name || item.code
const editableState = computed(() => ({ identity: { ...identity, tags: [...identity.tags] }, revision: { ...revision, alias: [...(revision.alias || [])], related_ids: [...(revision.related_ids || [])] } }))
const { isDirty, markSaved } = useUnsavedChanges({ state: editableState, t })
const setRevision = value => { Object.keys(revision).forEach(key => delete revision[key]); Object.assign(revision, JSON.parse(JSON.stringify(value || {}))); revision.alias ||= []; revision.related_ids ||= [] }
const goBack = () => navigateStandardRoute(router, { path: '/glossaries', query: route.query }, { history: 'replace' })
const onScopeChange = value => { if (value !== 'domain') identity.owner_domain_id = null }

async function load() {
  loading.value = true
  try {
    const [aggregate, revisions, elements] = await Promise.all([glossaryAPI.get(route.params.id), glossaryAPI.listRevisions(route.params.id), glossaryAPI.getElements(route.params.id)])
    glossary.value = aggregate; history.value = revisions || []; mappedElements.value = elements || []
    Object.assign(identity, { scope_type: aggregate.scope_type, owner_domain_id: aggregate.owner_domain_id || null, steward_id: aggregate.steward_id || null, tags: aggregate.tags || [] })
    setRevision(aggregate.draft_revision || aggregate.current_revision || history.value[0])
    markSaved()
  } catch (error) { ElMessage.error(getStandardErrorMessage(error, t, 'standard.common.loadFailed')); goBack() }
  finally { loading.value = false }
}
async function saveAll() {
  if (!revision.name?.trim() || !revision.definition?.trim() || !revision.change_summary?.trim()) { ElMessage.warning(t('standard.glossary.revisionRequired')); return }
  if (identity.scope_type === 'domain' && !identity.owner_domain_id) { ElMessage.warning(t('standard.common.selectDomain')); return }
  saving.value = true
  try {
    let aggregate = await glossaryAPI.update(glossary.value.id, { ...identity, version: glossary.value.version })
    aggregate = await glossaryAPI.updateRevision(glossary.value.id, revision.id, { ...revision, version: aggregate.version })
    glossary.value = aggregate; ElMessage.success(t('standard.common.saveSuccess')); await load(); markSaved()
  } catch (error) { ElMessage.error(getStandardErrorMessage(error, t, 'standard.common.saveFailed')) }
  finally { saving.value = false }
}
async function newDraft() {
  try { const { value } = await ElMessageBox.prompt(t('standard.revision.changeSummary'), t('standard.glossary.createRevision'), { inputValidator: value => Boolean(value?.trim()) }); await glossaryAPI.createRevision(glossary.value.id, { version: glossary.value.version, change_summary: value.trim() }); await load() }
  catch (error) { if (!isCanceledInteraction(error)) ElMessage.error(getStandardErrorMessage(error, t)) }
}
async function runRevisionAction(action) {
  if (isDirty.value) { ElMessage.warning(t('standard.common.saveBeforeAction')); return }
  try { await ElMessageBox.confirm(t(`standard.revision.confirm.${action}`), t('standard.common.hint'), { type: 'warning' }); await glossaryAPI[`${action}Revision`](glossary.value.id, revision.id, glossary.value.version); await load() }
  catch (error) { if (!isCanceledInteraction(error)) ElMessage.error(getStandardErrorMessage(error, t)) }
}
function selectRevision(row) { setRevision(row) }
async function searchElements(keyword='') { elementSearchLoading.value = true; try { const result = await elementAPI.list({ keyword, page_size: 50 }); searchedElements.value = result.data || [] } catch { searchedElements.value = [] } finally { elementSearchLoading.value = false } }
function openElementDialog() { selectedElementIDs.value = mappedElements.value.map(item => item.id); elementDialog.value = true; searchElements() }
async function saveElements() { try { const aggregate = await glossaryAPI.updateElements(glossary.value.id, { version: glossary.value.version, element_ids: selectedElementIDs.value }); glossary.value = aggregate; elementDialog.value = false; await load(); ElMessage.success(t('standard.common.saveSuccess')) } catch (error) { ElMessage.error(getStandardErrorMessage(error, t)) } }
watch(() => route.params.id, load, { immediate: true })
onMounted(async () => { try { domains.value = flattenDomains(await domainAPI.list() || []) } catch { domains.value = [] } })
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
