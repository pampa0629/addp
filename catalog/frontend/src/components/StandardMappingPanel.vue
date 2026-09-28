<template>
  <el-card shadow="never" class="mapping-card">
    <template #header>
      <div class="mapping-header">
        <strong>{{ t('catalog.mapping.title') }}</strong>
        <el-button v-if="canEditMapping" type="primary" @click="openCandidate()">{{ t('catalog.mapping.propose') }}</el-button>
      </div>
    </template>
    <p class="mapping-hint">{{ t('catalog.mapping.hint') }}</p>
    <el-table :data="entry.standard_mappings || []" row-key="id">
      <el-table-column :label="t('catalog.entry.componentName')" min-width="160">
        <template #default="{ row }">{{ componentNames.get(row.component_id) || t('catalog.mapping.componentUnavailable') }}</template>
      </el-table-column>
      <el-table-column :label="t('catalog.mapping.element')" min-width="160">
        <template #default="{ row }">{{ elementLabel(row) }}</template>
      </el-table-column>
      <el-table-column :label="t('catalog.mapping.revision')" min-width="120">
        <template #default="{ row }">{{ revisionLabel(row) }}</template>
      </el-table-column>
      <el-table-column :label="t('catalog.mapping.status')" min-width="110">
        <template #default="{ row }"><el-tag :type="statusType(row.review_status)">{{ t(`catalog.mapping.states.${row.review_status}`) }}</el-tag></template>
      </el-table-column>
      <el-table-column :label="t('catalog.mapping.source')" min-width="105">
        <template #default="{ row }">{{ row.id ? t(`catalog.mapping.sources.${row.source}`) : '' }}</template>
      </el-table-column>
      <el-table-column :label="t('catalog.mapping.evidence')" min-width="190" show-overflow-tooltip>
        <template #default="{ row }">{{ row.evidence?.reason || row.review_opinion || '-' }}</template>
      </el-table-column>
      <el-table-column :label="t('catalog.mapping.actions')" min-width="240" fixed="right">
        <template #default="{ row }">
          <template v-if="row.review_status === 'proposed'">
            <el-button v-if="canEditMapping" text @click="openCandidate(row)">{{ t('catalog.mapping.edit') }}</el-button>
            <el-button v-if="canEditMapping" text type="danger" @click="removeCandidate(row)">{{ t('catalog.mapping.delete') }}</el-button>
            <el-button v-if="canReviewMapping && row.element_revision_id" text type="success" @click="decide(row, 'approve')">{{ t('catalog.mapping.approve') }}</el-button>
            <el-button v-if="canReviewMapping" text type="warning" @click="decide(row, 'reject')">{{ t('catalog.mapping.reject') }}</el-button>
          </template>
          <el-button v-else-if="row.review_status === 'approved' && canReviewMapping" text type="warning" @click="decide(row, 'withdraw')">{{ t('catalog.mapping.withdraw') }}</el-button>
          <span v-else>-</span>
        </template>
      </el-table-column>
    </el-table>
    <el-empty v-if="!entry.standard_mappings?.length" :image-size="60" :description="t('catalog.mapping.empty')" />

    <el-dialog v-model="dialogVisible" :title="editingId ? t('catalog.mapping.edit') : t('catalog.mapping.propose')" width="min(640px, 92vw)" :close-on-click-modal="false">
      <el-form label-position="top">
        <el-form-item :label="t('catalog.entry.componentName')" required>
          <el-select v-model="form.componentId" filterable :placeholder="t('catalog.mapping.componentPlaceholder')">
            <el-option v-for="component in activeComponents" :key="component.id" :value="component.id" :label="component.display_name" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('catalog.mapping.element')" required>
          <el-select v-model="form.elementId" filterable remote reserve-keyword :remote-method="searchElements" :loading="elementsLoading" :placeholder="t('catalog.mapping.elementPlaceholder')" @visible-change="visible => visible && searchElements('')" @change="() => loadRevisions()">
            <el-option v-for="option in elements" :key="option.id" :value="String(option.id)" :label="option.code ? `${option.name} · ${option.code}` : option.name" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('catalog.mapping.revision')" required>
          <el-select v-model="form.revisionId" :disabled="!form.elementId" :loading="revisionsLoading" :placeholder="t('catalog.mapping.revisionPlaceholder')">
            <el-option v-for="option in revisions" :key="option.id" :value="String(option.id)" :label="`R${option.revision_no} · ${option.name}`" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('catalog.mapping.confidence')">
          <el-input-number v-model="form.confidence" :min="0" :max="1" :step="0.05" :precision="2" />
        </el-form-item>
        <el-form-item :label="t('catalog.mapping.evidence')" required>
          <el-input v-model.trim="form.reason" type="textarea" :rows="3" :placeholder="t('catalog.mapping.evidencePlaceholder')" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">{{ t('catalog.edit.cancel') }}</el-button>
        <el-button type="primary" :loading="saving" :disabled="!canSubmit" @click="saveCandidate">{{ t(editingId ? 'catalog.mapping.saveEdit' : 'catalog.mapping.submit') }}</el-button>
      </template>
    </el-dialog>
  </el-card>
</template>

<script setup>
import { computed, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { createStandardMapping, deleteStandardMapping, listReferenceCandidates, listStandardMappingRevisionOptions, reviewStandardMapping, updateStandardMapping } from '../api/catalog'
import { standardMappingElementLabel, standardMappingRevisionLabel } from '../utils/standardMappingPresentation'

const props = defineProps({
  entry: { type: Object, required: true },
  canEdit: { type: Boolean, default: false },
  canReview: { type: Boolean, default: false }
})
const emit = defineEmits(['changed'])
const { t } = useI18n()
const dialogVisible = ref(false)
const editingId = ref('')
const saving = ref(false)
const elementsLoading = ref(false)
const revisionsLoading = ref(false)
const elements = ref([])
const revisions = ref([])
const form = reactive({ componentId: '', elementId: '', revisionId: '', confidence: null, reason: '', version: 0 })
const componentNames = computed(() => new Map((props.entry.components || []).map(item => [item.id, item.display_name])))
const activeComponents = computed(() => (props.entry.components || []).filter(item => item.component_status === 'active'))
const canEditMapping = computed(() => props.canEdit && props.entry.entry_status === 'active' && !['certified', 'deprecated'].includes(props.entry.governance_status))
const canReviewMapping = computed(() => props.canReview && props.entry.entry_status === 'active' && !['certified', 'deprecated'].includes(props.entry.governance_status))
const canSubmit = computed(() => form.componentId && form.elementId && form.revisionId && form.reason.trim() && !saving.value)
let elementSearchVersion = 0
let revisionRequestVersion = 0

function statusType(status) {
  return ({ proposed: 'info', approved: 'success', rejected: 'danger', withdrawn: 'warning' })[status] || 'info'
}

const elementLabel = row => standardMappingElementLabel(row, t)
const revisionLabel = row => standardMappingRevisionLabel(row, t)

async function openCandidate(row = null) {
  editingId.value = row?.id || ''
  Object.assign(form, {
    componentId: row?.component_id || '', elementId: row?.element_id ? String(row.element_id) : '',
    revisionId: row?.element_revision_id ? String(row.element_revision_id) : '',
    confidence: row?.confidence ?? null, reason: row?.evidence?.reason || '', version: Number(row?.version || 0)
  })
  elements.value = row?.element_id ? [{
    id: String(row.element_id), name: row?.element_reference?.name || row?.evidence?.legacy_observed_snapshot?.name || t('catalog.mapping.elementUnavailable'),
    code: row?.element_reference?.code || ''
  }] : []
  revisions.value = []
  dialogVisible.value = true
  if (form.elementId) await loadRevisions(true)
}

async function searchElements(search) {
  const version = ++elementSearchVersion
  elementsLoading.value = true
  try {
    const result = await listReferenceCandidates({ reference_type: 'element', search: String(search || '').trim(), page: 1, page_size: 20 })
    if (version !== elementSearchVersion) return
    const options = new Map((result.data || []).map(item => [String(item.id), item]))
    for (const item of elements.value) if (String(item.id) === form.elementId && !options.has(form.elementId)) options.set(form.elementId, item)
    elements.value = [...options.values()]
  } catch (error) {
    if (version === elementSearchVersion) ElMessage.error(error?.response?.data?.error || t('catalog.mapping.loadFailed'))
  } finally {
    if (version === elementSearchVersion) elementsLoading.value = false
  }
}

async function loadRevisions(preserveSelection = false) {
  const version = ++revisionRequestVersion
  const selectedRevision = preserveSelection ? form.revisionId : ''
  form.revisionId = ''
  revisions.value = []
  if (!form.elementId) return
  revisionsLoading.value = true
  try {
    const result = await listStandardMappingRevisionOptions(form.elementId)
    if (version === revisionRequestVersion) {
      revisions.value = Array.isArray(result) ? result : []
      if (revisions.value.some(item => String(item.id) === selectedRevision)) form.revisionId = selectedRevision
    }
  } catch (error) {
    if (version === revisionRequestVersion) ElMessage.error(error?.response?.data?.error || t('catalog.mapping.loadFailed'))
  } finally {
    if (version === revisionRequestVersion) revisionsLoading.value = false
  }
}

async function saveCandidate() {
  if (!canSubmit.value) return
  saving.value = true
  const payload = {
    catalog_entry_id: props.entry.id, component_id: form.componentId,
    element_id: form.elementId, element_revision_id: form.revisionId,
    confidence: form.confidence, evidence: { reason: form.reason.trim() },
    ...(editingId.value ? { version: form.version } : {})
  }
  try {
    if (editingId.value) await updateStandardMapping(editingId.value, payload)
    else await createStandardMapping(payload)
    dialogVisible.value = false
    emit('changed')
  } catch (error) {
    ElMessage.error(error?.response?.data?.error || t('catalog.mapping.saveFailed'))
    if (error?.response?.status === 409) emit('changed')
  } finally { saving.value = false }
}

async function removeCandidate(row) {
  try {
    await ElMessageBox.confirm(t('catalog.mapping.deleteConfirm'), t('catalog.mapping.delete'))
    await deleteStandardMapping(row.id, row.version)
    emit('changed')
  } catch (error) {
    if (error !== 'cancel' && error !== 'close') ElMessage.error(error?.response?.data?.error || t('catalog.mapping.saveFailed'))
  }
}

async function decide(row, action) {
  let opinion = ''
  try {
    if (action === 'approve') {
      await ElMessageBox.confirm(t('catalog.mapping.approveConfirm'), t('catalog.mapping.approve'))
    } else {
      const answer = await ElMessageBox.prompt(t(`catalog.mapping.${action}Prompt`), t(`catalog.mapping.${action}`), {
        inputValidator: value => Boolean(String(value || '').trim()),
        inputErrorMessage: t('catalog.mapping.opinionRequired')
      })
      opinion = answer.value.trim()
    }
    await reviewStandardMapping(row.id, action, { version: row.version, opinion })
    emit('changed')
  } catch (error) {
    if (error !== 'cancel' && error !== 'close') {
      ElMessage.error(error?.response?.data?.error || t('catalog.mapping.saveFailed'))
      if (error?.response?.status === 409) emit('changed')
    }
  }
}
</script>

<style scoped>
.mapping-card { margin-top: 16px; }
.mapping-header { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.mapping-hint { margin: 0 0 12px; color: var(--addp-text-secondary); }
.mapping-card :deep(.el-select) { width: 100%; }
</style>
