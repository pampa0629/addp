<template>
  <section class="detector-bindings">
    <div class="section-header">
      <div>
        <h3>{{ t('security.detector.title') }}</h3>
        <p>{{ t('security.detector.description', { name: sensitiveTypeName }) }}</p>
      </div>
      <el-button v-if="can('create')" type="primary" :disabled="availableCapabilities.length === 0" @click="openCreate">
        {{ t('security.detector.create') }}
      </el-button>
    </div>

    <el-alert :title="t('security.detector.thresholdSummary')" type="info" :closable="false" show-icon />
    <el-alert class="parallel-hint" :title="t('security.detector.parallelHint')" type="info" :closable="false" show-icon />

    <section class="quality-panel" :aria-label="t('security.discoveryQuality.title')">
      <div class="quality-heading">
        <div>
          <h4>{{ t('security.discoveryQuality.title') }}</h4>
          <p>{{ t('security.discoveryQuality.description') }}</p>
        </div>
      </div>
      <div class="quality-grid">
        <article class="quality-card">
          <span>{{ t('security.discoveryQuality.currentFindings') }}</span>
          <strong>{{ quality.current_finding_count }}</strong>
        </article>
        <article class="quality-card">
          <span>{{ t('security.discoveryQuality.awaitingReview') }}</span>
          <strong>{{ quality.awaiting_review_count }}</strong>
        </article>
        <article class="quality-card">
          <span>{{ t('security.discoveryQuality.reviewedSamples') }}</span>
          <strong>{{ quality.reviewed_sample_count }}</strong>
        </article>
        <article class="quality-card">
          <span>{{ t('security.discoveryQuality.sensitiveConfirmationRate') }}</span>
          <strong>{{ formatEvidenceRate(quality.sensitive_confirmation_rate) }}</strong>
        </article>
      </div>
      <p class="quality-evidence-hint">{{ t('security.discoveryQuality.evidenceHint') }}</p>
      <el-alert
        class="manual-signal"
        :title="t('security.discoveryQuality.manualSignals', {
          active: quality.active_manual_assessment_count,
          revoked: quality.revoked_manual_assessment_count
        })"
        :description="t('security.discoveryQuality.manualSignalsDescription')"
        type="info"
        :closable="false"
        show-icon
      />
      <div class="quality-table">
        <div class="quality-table-title">{{ t('security.discoveryQuality.byCapability') }}</div>
        <el-table v-loading="loading" :data="quality.capabilities" size="small" row-key="capability_key">
          <el-table-column :label="t('security.detector.capability')" min-width="200">
            <template #default="{ row }">
              <div class="capability-name">{{ qualityCapabilityName(row) }}</div>
              <div class="secondary-text">{{ row.capability_key }}</div>
            </template>
          </el-table-column>
          <el-table-column :label="t('security.discoveryQuality.currentEvidence')" min-width="150">
            <template #default="{ row }">
              {{ t('security.discoveryQuality.currentBreakdown', {
                current: row.current_finding_count,
                pending: row.awaiting_review_count
              }) }}
            </template>
          </el-table-column>
          <el-table-column :label="t('security.discoveryQuality.humanEvidence')" min-width="210">
            <template #default="{ row }">
              <div>{{ t('security.discoveryQuality.reviewBreakdown', {
                confirmed: row.confirmed_count,
                adjusted: row.adjusted_count,
                rejected: row.rejected_count
              }) }}</div>
              <div class="secondary-text">
                {{ t('security.discoveryQuality.rateValue', { value: formatEvidenceRate(row.sensitive_confirmation_rate) }) }}
              </div>
            </template>
          </el-table-column>
        </el-table>
        <el-empty
          v-if="!loading && quality.capabilities.length === 0"
          :description="t('security.discoveryQuality.noEvidence')"
          :image-size="64"
        />
      </div>
    </section>

    <div class="table-panel">
      <el-table v-loading="loading" :data="scopedRows" row-key="id">
        <el-table-column :label="t('security.detector.capability')" min-width="230">
          <template #default="{ row }">
            <div class="capability-name">{{ capabilityName(row.capability_key) }}</div>
            <div class="secondary-text">{{ capabilityDescription(row.capability_key) }}</div>
            <el-button class="details-link" link type="primary" @click="openCapabilityDetails(row.capability_key)">
              {{ t('security.detector.viewImplementation') }}
            </el-button>
          </template>
        </el-table-column>
        <el-table-column :label="t('security.detector.scope')" min-width="150">
          <template #default="{ row }">{{ capabilityScope(row.capability_key) }}</template>
        </el-table-column>
        <el-table-column :label="t('security.detector.confidenceThreshold')" width="150">
          <template #default="{ row }">{{ formatPercent(row.confidence_threshold) }}</template>
        </el-table-column>
        <el-table-column :label="t('security.detector.participates')" width="110">
          <template #default="{ row }">
            <el-tag size="small" :type="row.enabled ? 'success' : 'info'">
              {{ row.enabled ? t('security.detector.participating') : t('security.detector.notParticipating') }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('security.common.actions')" width="150" fixed="right">
          <template #default="{ row }">
            <el-button v-if="can('update')" link @click="openEdit(row)">{{ t('security.common.edit') }}</el-button>
            <el-button v-if="can('delete')" link type="danger" @click="remove(row)">{{ t('security.common.delete') }}</el-button>
          </template>
        </el-table-column>
      </el-table>
      <el-empty v-if="!loading && scopedRows.length === 0" :description="t('security.detector.emptyForType', { name: sensitiveTypeName })" />
    </div>

    <el-dialog
      v-model="dialog"
      class="addp-dialog"
      :title="editing ? t('security.detector.edit') : t('security.detector.create')"
      width="min(760px, calc(100vw - 24px))"
    >
      <el-alert :title="t('security.detector.trustedCapabilityHint')" type="info" :closable="false" show-icon />
      <el-form label-position="top" class="detector-form">
        <el-form-item :label="t('security.detector.capability')" required>
          <div class="capability-options" role="radiogroup" :aria-label="t('security.detector.capability')">
            <button
              v-for="item in selectableCapabilities"
              :key="item.key"
              type="button"
              role="radio"
              class="capability-option"
              :class="{ selected: form.capability_key === item.key }"
              :aria-checked="form.capability_key === item.key"
              @click="selectCapability(item.key)"
            >
              <span class="capability-option-header">
                <strong>{{ capabilityName(item.key) }}</strong>
                <el-tag size="small" effect="plain">{{ capabilityScope(item.key) }}</el-tag>
              </span>
              <span class="option-description">{{ capabilityDescription(item.key) }}</span>
            </button>
          </div>
        </el-form-item>
        <CapabilityExplanation v-if="selectedCapability" :capability="selectedCapability" />
        <el-form-item :label="t('security.detector.confidenceThreshold')" required>
          <el-input-number v-model="form.confidence_percent" :min="1" :max="100" :step="1" controls-position="right" />
          <span class="percent-suffix">%</span>
          <div class="field-help">{{ t('security.detector.confidenceThresholdHelp') }}</div>
        </el-form-item>
        <el-form-item :label="t('security.detector.participates')">
          <el-switch
            v-model="form.enabled"
            :active-text="t('security.detector.participating')"
            :inactive-text="t('security.detector.notParticipating')"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialog = false">{{ t('security.common.cancel') }}</el-button>
        <el-button type="primary" :loading="saving" @click="save">{{ t('security.common.save') }}</el-button>
      </template>
    </el-dialog>

    <el-dialog
      v-model="capabilityDetailsDialog"
      class="addp-dialog"
      :title="capabilityDetails ? capabilityName(capabilityDetails.key) : t('security.detector.viewImplementation')"
      width="min(720px, calc(100vw - 24px))"
    >
      <CapabilityExplanation v-if="capabilityDetails" :capability="capabilityDetails" />
      <template #footer>
        <el-button @click="capabilityDetailsDialog = false">{{ t('security.common.close') }}</el-button>
      </template>
    </el-dialog>
  </section>
</template>

<script setup>
import { computed, defineComponent, h, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { detectorAPI, detectorCapabilityAPI, discoveryQualityAPI } from '../api/security'
import { useAuthStore } from '../store/auth'

const props = defineProps({
  sensitiveTypeId: { type: Number, required: true },
  sensitiveTypeName: { type: String, required: true }
})
const emit = defineEmits(['changed'])
const { t } = useI18n()
const auth = useAuthStore()
const rows = ref([])
const capabilities = ref([])
const quality = ref(emptyQuality())
const loading = ref(false)
const saving = ref(false)
const dialog = ref(false)
const capabilityDetailsDialog = ref(false)
const capabilityDetails = ref(null)
const editing = ref(null)
const form = reactive({ capability_key: '', confidence_percent: 90, enabled: true, version: 0 })

const scopedRows = computed(() => rows.value.filter(row => String(row.sensitive_data_type_id) === String(props.sensitiveTypeId)))
const availableCapabilities = computed(() => {
  const bound = new Set(rows.value.map(row => row.capability_key))
  return capabilities.value.filter(item => !bound.has(item.key))
})
const selectableCapabilities = computed(() => {
  if (!editing.value) return availableCapabilities.value
  return capabilities.value.filter(item => item.key === editing.value.capability_key || !rows.value.some(row => row.id !== editing.value.id && row.capability_key === item.key))
})
const selectedCapability = computed(() => capability(form.capability_key) || null)

function can(action) { return auth.hasPermission(`security.detector.${action}`) }
function capability(key) { return capabilities.value.find(item => item.key === key) }
function capabilityName(key) {
  const item = capability(key)
  return item ? t(item.name_i18n_key) : key
}
function qualityCapabilityName(row) {
  const item = capability(row.capability_key)
  return item ? t(item.name_i18n_key) : t('security.discoveryQuality.historicalCapability', { code: row.detector_code })
}
function capabilityDescription(key) {
  const item = capability(key)
  return item ? t(item.description_i18n_key) : t('security.common.notAvailable')
}
function capabilityScope(key) {
  const item = capability(key)
  if (!item) return t('security.common.notAvailable')
  return item.supported_item_types.map(type => t(`security.enrollment.itemTypes.${type}`)).join(t('security.detector.scopeSeparator'))
}
function fieldScope(item) {
  if (!item?.supported_field_types?.length) return t('security.common.notApplicable')
  return item.supported_field_types.map(type => t(`security.detector.fieldTypes.${type}`)).join(t('security.detector.scopeSeparator'))
}
function targetLabel(item) { return t(`security.detector.targets.${item.target_kind}`) }
function evidenceLabel(item) { return t(`security.detector.evidenceSources.${item.evidence_source}`) }
function formatPercent(value) { return `${Math.round(Number(value || 0) * 100)}%` }
function formatEvidenceRate(value) {
  if (value === null || value === undefined) return t('security.discoveryQuality.insufficientSamples')
  return formatPercent(value)
}
function emptyQuality() {
  return {
    current_finding_count: 0,
    awaiting_review_count: 0,
    reviewed_sample_count: 0,
    sensitive_confirmation_rate: null,
    active_manual_assessment_count: 0,
    revoked_manual_assessment_count: 0,
    capabilities: []
  }
}
function applyRecommendedThreshold(key) {
  const item = capability(key)
  form.confidence_percent = Math.round(Number(item?.recommended_threshold || 0.9) * 100)
}
function selectCapability(key) {
  if (form.capability_key === key) return
  form.capability_key = key
  applyRecommendedThreshold(key)
}
function openCapabilityDetails(key) {
  capabilityDetails.value = capability(key) || null
  capabilityDetailsDialog.value = Boolean(capabilityDetails.value)
}

const CapabilityExplanation = defineComponent({
  props: { capability: { type: Object, required: true } },
  setup(componentProps) {
    const row = (label, value) => h('div', { class: 'explanation-row' }, [
      h('dt', label),
      h('dd', value)
    ])
    return () => h('section', { class: 'capability-explanation' }, [
      h('p', { class: 'capability-summary' }, t(componentProps.capability.description_i18n_key)),
      h('dl', { class: 'explanation-list' }, [
        row(t('security.detector.implementationMethod'), t(componentProps.capability.method_i18n_key)),
        row(t('security.detector.detectionTarget'), targetLabel(componentProps.capability)),
        row(t('security.detector.evidenceSource'), evidenceLabel(componentProps.capability)),
        row(t('security.detector.scope'), capabilityScope(componentProps.capability.key)),
        row(t('security.detector.fieldScope'), fieldScope(componentProps.capability)),
        row(t('security.detector.privacyBoundary'), t(componentProps.capability.privacy_i18n_key)),
        row(t('security.detector.knownLimitations'), t(componentProps.capability.limitations_i18n_key)),
        row(t('security.detector.stableIdentifier'), componentProps.capability.key),
        row(t('security.detector.capabilityVersion'), componentProps.capability.version)
      ])
    ])
  }
})

async function load() {
  loading.value = true
  try {
    const [bindings, installed, qualitySummary] = await Promise.all([
      detectorAPI.list(),
      detectorCapabilityAPI.list(),
      discoveryQualityAPI.get({ sensitive_data_type_id: props.sensitiveTypeId })
    ])
    rows.value = Array.isArray(bindings) ? bindings : []
    capabilities.value = Array.isArray(installed) ? installed : []
    quality.value = qualitySummary && typeof qualitySummary === 'object'
      ? { ...emptyQuality(), ...qualitySummary, capabilities: Array.isArray(qualitySummary.capabilities) ? qualitySummary.capabilities : [] }
      : emptyQuality()
  } catch (error) {
    ElMessage.error(error.message || t('security.common.failed'))
  } finally {
    loading.value = false
  }
}

function openCreate() {
  editing.value = null
  form.capability_key = availableCapabilities.value[0]?.key || ''
  form.enabled = true
  form.version = 0
  applyRecommendedThreshold(form.capability_key)
  dialog.value = true
}
function openEdit(row) {
  editing.value = row
  form.capability_key = row.capability_key
  form.confidence_percent = Math.round(Number(row.confidence_threshold) * 100)
  form.enabled = Boolean(row.enabled)
  form.version = Number(row.version)
  dialog.value = true
}

async function save() {
  if (!form.capability_key || !Number.isFinite(form.confidence_percent) || form.confidence_percent < 1 || form.confidence_percent > 100) {
    ElMessage.warning(t('security.detector.required'))
    return
  }
  saving.value = true
  try {
    const payload = {
      capability_key: form.capability_key,
      sensitive_data_type_id: props.sensitiveTypeId,
      confidence_threshold: form.confidence_percent / 100,
      enabled: Boolean(form.enabled)
    }
    if (editing.value) {
      payload.version = form.version
      await detectorAPI.update(editing.value.id, payload)
    } else {
      await detectorAPI.create(payload)
    }
    dialog.value = false
    await load()
    emit('changed')
    ElMessage.success(t('security.detector.saved'))
  } catch (error) {
    ElMessage.error(error.message || t('security.common.failed'))
  } finally {
    saving.value = false
  }
}

async function remove(row) {
  try {
    await ElMessageBox.confirm(t('security.detector.confirmDelete', { name: capabilityName(row.capability_key) }), t('security.common.hint'), { type: 'warning' })
    await detectorAPI.delete(row.id, { version: Number(row.version) })
    await load()
    emit('changed')
    ElMessage.success(t('security.detector.deleted'))
  } catch (error) {
    if (error !== 'cancel' && error !== 'close') ElMessage.error(error.message || t('security.common.failed'))
  }
}

watch(() => props.sensitiveTypeId, load)
onMounted(load)
</script>

<style scoped>
.detector-bindings { min-height: 0; }
.section-header { display: flex; justify-content: space-between; gap: 20px; align-items: flex-start; margin-bottom: 16px; }
.section-header h3 { margin: 0; }
.section-header p { margin: 8px 0 0; color: var(--addp-text-secondary); }
.parallel-hint { margin-top: 8px; }
.quality-panel { margin-top: 16px; padding: 16px; border: 1px solid var(--addp-border-color); border-radius: 8px; background: var(--addp-bg-secondary); }
.quality-heading h4 { margin: 0; color: var(--addp-text-primary); }
.quality-heading p { margin: 6px 0 0; color: var(--addp-text-secondary); font-size: 13px; line-height: 1.5; }
.quality-grid { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 10px; margin-top: 14px; }
.quality-card { min-width: 0; padding: 12px; border: 1px solid var(--addp-border-color); border-radius: 6px; background: var(--addp-bg-primary); }
.quality-card span { display: block; color: var(--addp-text-tertiary); font-size: 12px; }
.quality-card strong { display: block; margin-top: 6px; color: var(--addp-text-primary); font-size: 20px; }
.quality-evidence-hint { margin: 10px 0 0; color: var(--addp-text-tertiary); font-size: 12px; line-height: 1.5; }
.manual-signal { margin-top: 12px; }
.quality-table { margin-top: 14px; overflow: hidden; border: 1px solid var(--addp-border-color); border-radius: 6px; background: var(--addp-bg-primary); }
.quality-table-title { padding: 10px 12px; border-bottom: 1px solid var(--addp-border-color); color: var(--addp-text-primary); font-weight: 600; }
.table-panel { margin-top: 16px; overflow: hidden; border: 1px solid var(--addp-border-color); border-radius: 6px; background: var(--addp-bg-primary); }
.capability-name { color: var(--addp-text-primary); font-weight: 600; }
.secondary-text, .option-description, .field-help { margin-top: 4px; color: var(--addp-text-tertiary); font-size: 12px; line-height: 1.5; }
.details-link { margin-top: 2px; padding: 0; font-size: 12px; }
.detector-form { margin-top: 20px; }
.wide { width: 100%; }
.percent-suffix { margin-left: 8px; color: var(--addp-text-secondary); }
.field-help { flex-basis: 100%; }
.capability-options { display: grid; width: 100%; gap: 8px; }
.capability-option { width: 100%; padding: 12px 14px; border: 1px solid var(--addp-border-color); border-radius: 8px; background: var(--addp-bg-secondary); color: var(--addp-text-primary); text-align: left; cursor: pointer; transition: border-color 0.2s, background-color 0.2s; }
.capability-option:hover, .capability-option:focus-visible { border-color: var(--el-color-primary); outline: none; }
.capability-option.selected { border-color: var(--el-color-primary); background: var(--addp-bg-primary); box-shadow: 0 0 0 1px var(--el-color-primary); }
.capability-option-header { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.option-description { display: block; }
:deep(.capability-explanation) { margin: 0 0 18px; padding: 14px; border: 1px solid var(--addp-border-color); border-radius: 8px; background: var(--addp-bg-secondary); }
:deep(.capability-summary) { margin: 0 0 12px; color: var(--addp-text-secondary); line-height: 1.6; }
:deep(.explanation-list) { display: grid; grid-template-columns: minmax(120px, 150px) 1fr; margin: 0; }
:deep(.explanation-row) { display: contents; }
:deep(.explanation-row dt), :deep(.explanation-row dd) { margin: 0; padding: 8px 10px; border-top: 1px solid var(--addp-border-color); line-height: 1.6; }
:deep(.explanation-row dt) { color: var(--addp-text-secondary); font-weight: 600; }
:deep(.explanation-row dd) { min-width: 0; color: var(--addp-text-primary); overflow-wrap: anywhere; }
:deep(.el-table) { --el-table-bg-color: var(--addp-bg-primary); --el-table-tr-bg-color: var(--addp-bg-primary); }
@media (max-width: 640px) {
  .quality-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  :deep(.explanation-list) { display: block; }
  :deep(.explanation-row) { display: block; }
  :deep(.explanation-row dt) { padding-bottom: 2px; }
  :deep(.explanation-row dd) { padding-top: 2px; border-top: 0; }
}
</style>
