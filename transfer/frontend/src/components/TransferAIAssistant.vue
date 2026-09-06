<template>
  <div class="transfer-ai-assistant">
    <transition name="transfer-ai-slide">
      <section v-if="visible" class="transfer-ai-panel" aria-live="polite">
        <header class="transfer-ai-panel-header">
          <div>
            <div class="transfer-ai-panel-title">{{ t('transfer.taskAssistant.title') }}</div>
            <div class="transfer-ai-panel-subtitle">{{ t('transfer.taskAssistant.subtitle') }}</div>
          </div>
          <el-button circle text size="small" :aria-label="t('transfer.taskAssistant.close')" :disabled="busy" @click="close">
            <el-icon><Close /></el-icon>
          </el-button>
        </header>

        <el-steps :active="stageIndex" simple class="transfer-ai-steps">
          <el-step :title="t('transfer.taskAssistant.stepRequest')" />
          <el-step :title="t('transfer.taskAssistant.stepSource')" />
          <el-step :title="t('transfer.taskAssistant.stepTarget')" />
          <el-step :title="t('transfer.taskAssistant.stepFields')" />
          <el-step :title="t('transfer.taskAssistant.stepReview')" />
        </el-steps>

        <div v-if="stage === 'request'" class="transfer-ai-stage">
          <el-alert :title="t('transfer.taskAssistant.boundaryTitle')" :description="t('transfer.taskAssistant.boundaryDescription')" type="info" :closable="false" show-icon />
          <el-input
            v-model="prompt"
            class="transfer-ai-prompt"
            type="textarea"
            :rows="4"
            :placeholder="t('transfer.taskAssistant.promptPlaceholder')"
            :disabled="busy"
            @keydown.ctrl.enter="discoverSource"
            @keydown.meta.enter="discoverSource"
          />
        </div>

        <div v-else-if="stage === 'source'" class="transfer-ai-stage">
          <el-alert :title="t('transfer.taskAssistant.sourceConfirmTitle')" :description="t('transfer.taskAssistant.sourceConfirmDescription')" type="info" :closable="false" show-icon />
          <div v-for="group in candidateGroups" :key="group.role" class="transfer-ai-candidate-group">
            <div class="transfer-ai-group-title">{{ t('transfer.taskAssistant.sourceRole') }}</div>
            <el-radio-group v-model="selectedByRole[group.role]" class="transfer-ai-candidate-options">
              <el-radio v-for="candidate in group.candidates" :key="candidateKey(candidate)" :value="candidateKey(candidate)" class="transfer-ai-candidate" :disabled="busy">
                <span class="transfer-ai-candidate-content">
                  <span class="transfer-ai-candidate-heading">
                    <span class="transfer-ai-candidate-name">{{ candidate.name || candidate.full_name || t('transfer.taskAssistant.unnamedResource') }}</span>
                    <el-tag v-if="candidate.recommended" size="small" type="success" effect="plain">{{ t('transfer.taskAssistant.recommended') }}</el-tag>
                  </span>
                  <span class="transfer-ai-candidate-engine">{{ candidate.engine_name || candidate.engine_type || '-' }}</span>
                  <span v-if="candidateDisplayPath(candidate) && candidateDisplayPath(candidate) !== candidate.name" class="transfer-ai-candidate-path">{{ candidateDisplayPath(candidate) }}</span>
                  <span v-if="candidateFacts(candidate)" class="transfer-ai-candidate-facts">{{ candidateFacts(candidate) }}</span>
                  <span v-if="candidate.recommendation_reason" class="transfer-ai-candidate-reason">{{ candidate.recommendation_reason }}</span>
                </span>
              </el-radio>
            </el-radio-group>
          </div>
        </div>

        <div v-else-if="stage === 'target'" class="transfer-ai-stage">
          <el-alert :title="t('transfer.taskAssistant.targetConfirmTitle')" :description="t('transfer.taskAssistant.targetConfirmDescription')" type="info" :closable="false" show-icon />
          <el-form label-position="top" class="transfer-ai-target-form">
            <el-form-item :label="t('transfer.taskAssistant.targetEngineLabel')" required>
              <el-select v-model="targetEngineId" filterable class="full-width" :disabled="busy" @change="handleTargetEngineChange" @visible-change="handleTargetEngineDropdownVisible">
                <el-option v-for="engine in targetEngines" :key="engine.id" :label="engineLabel(engine)" :value="Number(engine.id)" :disabled="!isEngineSelectable(engine)" />
              </el-select>
            </el-form-item>
            <el-form-item :label="t('transfer.taskAssistant.targetParentLabel')" required>
              <ResourceTreePicker
                v-model="targetParentSelection"
                class="full-width"
                api-base-url="/api/v1/meta"
                :engine-id="targetEngineId"
                mode="node"
                :show-engine-selector="false"
                :selectable-filter="isTargetParentSelectable"
                :tree-height="230"
                :show-selection-summary="false"
                :show-count="false"
                :disabled="!targetEngineId || busy"
                @select="handleTargetParentSelect"
              />
            </el-form-item>
            <el-form-item :label="t('transfer.taskAssistant.targetTableLabel')" required>
              <el-input v-model="targetTable" class="full-width" :disabled="!targetParentLocator || busy" :placeholder="t('transfer.taskAssistant.targetTablePlaceholder')" />
            </el-form-item>
            <el-divider content-position="left">{{ t('transfer.taskWizard.loadSettings') }}</el-divider>
            <el-form-item :label="t('transfer.taskWizard.loadModeLabel')" required>
              <el-radio-group v-model="syncMode" class="transfer-ai-sync-modes" :disabled="busy" @change="applySyncMode">
                <template v-if="isKafkaSource">
                  <el-radio value="kafka">{{ t('transfer.taskWizard.continuousIncrementalLoad') }}</el-radio>
                </template>
                <template v-else>
                  <el-radio value="snapshot">{{ t('transfer.taskWizard.snapshotLoad') }}</el-radio>
                  <el-radio value="incremental" :disabled="!wizardState.supportsWatermarkIncremental.value">{{ t('transfer.taskWizard.watermarkIncrementalLoad') }}</el-radio>
                  <el-radio value="cdc" :disabled="!wizardState.supportsDatabaseCDC.value">{{ t('transfer.taskWizard.databaseCDCLoad') }}</el-radio>
                </template>
              </el-radio-group>
            </el-form-item>
            <el-alert
              v-if="syncMode === 'incremental'"
              :title="t('transfer.taskWizard.watermarkIncrementalNoticeTitle')"
              :description="wizardState.supportsWatermarkIncremental.value ? t('transfer.taskWizard.watermarkIncrementalNotice') : t('transfer.taskWizard.watermarkIncrementalUnsupported')"
              :type="wizardState.supportsWatermarkIncremental.value ? 'warning' : 'error'"
              :closable="false"
              show-icon
              class="transfer-ai-mode-alert"
            />
            <el-alert
              v-if="syncMode === 'cdc'"
              :title="wizardState.supportsDatabaseCDC.value ? t('transfer.taskWizard.cdcSyncTitle') : t('transfer.taskWizard.databaseCDCUnavailableTitle')"
              :description="wizardState.supportsDatabaseCDC.value ? t('transfer.taskWizard.cdcSyncDesc') : databaseCDCUnavailableText"
              :type="wizardState.supportsDatabaseCDC.value ? 'warning' : 'error'"
              :closable="false"
              show-icon
              class="transfer-ai-mode-alert"
            />
            <el-alert
              v-if="syncMode === 'kafka'"
              :title="t('transfer.taskWizard.continuousSyncTitle')"
              :description="t('transfer.taskWizard.continuousSyncDesc')"
              type="info"
              :closable="false"
              show-icon
              class="transfer-ai-mode-alert"
            />
            <template v-if="syncMode === 'incremental' && wizardState.supportsWatermarkIncremental.value">
              <el-form-item :label="t('transfer.taskWizard.watermarkFieldLabel')" required>
                <el-select v-model="wizardState.watermarkField.value" filterable class="full-width" :placeholder="t('transfer.taskWizard.watermarkFieldPlaceholder')" :disabled="busy" @change="handleWatermarkFieldChange">
                  <el-option v-for="field in sourceFieldOptions" :key="field.value" :label="field.label" :value="field.value" />
                </el-select>
                <div class="transfer-ai-form-hint">{{ t('transfer.taskWizard.watermarkFieldHint') }}</div>
              </el-form-item>
              <el-form-item :label="t('transfer.taskWizard.tieBreakerLabel')" required>
                <el-select v-model="wizardState.watermarkTieBreakers.value" multiple filterable class="full-width" :placeholder="t('transfer.taskWizard.tieBreakerPlaceholder')" :disabled="busy">
                  <el-option v-for="field in sourceFieldOptions" :key="field.value" :label="field.label" :value="field.value" :disabled="field.value === wizardState.watermarkField.value" />
                </el-select>
              </el-form-item>
              <el-form-item :label="t('transfer.taskWizard.targetKeysLabel')" required>
                <el-select v-model="wizardState.targetKeys.value" multiple filterable class="full-width" :placeholder="t('transfer.taskWizard.targetKeysPlaceholder')" :disabled="busy">
                  <el-option v-for="field in targetFieldOptions" :key="field.value" :label="field.label" :value="field.value" />
                </el-select>
              </el-form-item>
            </template>
            <template v-if="syncMode === 'kafka'">
              <el-form-item :label="t('transfer.taskWizard.continuousKeyFieldsLabel')" required>
                <el-select v-model="wizardState.continuousKeyFields.value" multiple filterable allow-create default-first-option class="full-width" :placeholder="t('transfer.taskWizard.continuousKeyFieldsPlaceholder')" :disabled="busy" @change="wizardState.updateContinuousKeyFields">
                  <el-option v-for="field in sourceFieldOptions" :key="field.value" :label="field.label" :value="field.value" />
                </el-select>
              </el-form-item>
              <el-form-item :label="t('transfer.taskWizard.continuousInitialPositionLabel')" required>
                <el-radio-group v-model="wizardState.continuousInitialPosition.value" :disabled="busy">
                  <el-radio value="earliest">{{ t('transfer.taskWizard.continuousInitialEarliest') }}</el-radio>
                  <el-radio value="latest">{{ t('transfer.taskWizard.continuousInitialLatest') }}</el-radio>
                </el-radio-group>
              </el-form-item>
            </template>
            <el-alert
              v-if="syncMode === 'cdc' && wizardState.supportsDatabaseCDC.value"
              :title="t('transfer.taskWizard.cdcLifecycleWarningTitle')"
              :description="t('transfer.taskWizard.cdcLifecycleWarning')"
              type="warning"
              :closable="false"
              show-icon
              class="transfer-ai-mode-alert"
            />
            <el-form-item v-if="syncMode === 'snapshot'" :label="t('transfer.taskAssistant.writeModeLabel')">
              <el-select v-model="writeMode" class="full-width" :disabled="busy">
                <el-option :label="t('transfer.taskAssistant.overwrite')" value="overwrite" />
                <el-option :label="t('transfer.taskAssistant.append')" value="append" />
              </el-select>
            </el-form-item>
          </el-form>
        </div>

        <div v-else-if="stage === 'fields'" class="transfer-ai-stage">
          <el-alert :title="t('transfer.taskAssistant.fieldsConfirmTitle')" :description="t('transfer.taskAssistant.fieldsConfirmDescription')" type="info" :closable="false" show-icon />
          <el-table :data="wizardState.fieldMappings.value" border size="small" class="transfer-ai-field-table" max-height="300">
            <el-table-column prop="source_field" :label="t('transfer.taskAssistant.sourceField')" min-width="130" />
            <el-table-column prop="target_field" :label="t('transfer.taskAssistant.targetField')" min-width="130" />
            <el-table-column :label="t('transfer.taskAssistant.targetType')" width="130">
              <template #default="{ row }"><el-select v-model="row.target_type" size="small" :disabled="busy"><el-option v-for="type in fieldTypes" :key="type" :label="type" :value="type" /></el-select></template>
            </el-table-column>
            <el-table-column v-if="isMysqlTarget" :label="t('transfer.taskAssistant.precision')" width="112">
              <template #default="{ row }"><el-input-number v-if="row.target_type === 'decimal'" v-model="row.precision" size="small" :min="1" :max="65" controls-position="right" :disabled="busy" /><span v-else>-</span></template>
            </el-table-column>
            <el-table-column v-if="isMysqlTarget" :label="t('transfer.taskAssistant.scale')" width="100">
              <template #default="{ row }"><el-input-number v-if="row.target_type === 'decimal'" v-model="row.scale" size="small" :min="0" :max="decimalScaleMax(row)" controls-position="right" :disabled="busy" /><span v-else>-</span></template>
            </el-table-column>
          </el-table>
          <el-alert v-if="decimalIssues.length" class="transfer-ai-decimal-alert" type="warning" :closable="false" :title="t('transfer.taskAssistant.decimalRequired', { fields: decimalIssueNames })" />
          <el-alert
            v-if="(syncMode === 'cdc' || syncMode === 'kafka') && wizardState.continuousConfigIssues.value.length"
            class="transfer-ai-decimal-alert"
            type="error"
            :closable="false"
            :title="t('transfer.taskWizard.continuousConfigInvalidTitle')"
            :description="continuousConfigIssueText"
          />
          <div v-if="decimalScanRows !== null" class="transfer-ai-fact-note">{{ t('transfer.taskAssistant.decimalScanned', { rows: decimalScanRows }) }}</div>
        </div>

        <div v-else class="transfer-ai-stage">
          <el-alert :title="t('transfer.taskAssistant.reviewTitle')" :description="t('transfer.taskAssistant.reviewDescription')" type="info" :closable="false" show-icon />
          <el-form label-position="top" class="transfer-ai-review-form">
            <el-form-item :label="t('transfer.taskAssistant.taskNameLabel')" required><el-input v-model="wizardState.taskName.value" :disabled="busy" /></el-form-item>
            <el-form-item :label="t('transfer.taskAssistant.taskDescriptionLabel')"><el-input v-model="wizardState.taskDescription.value" type="textarea" :rows="2" :disabled="busy" /></el-form-item>
          </el-form>
          <el-descriptions :column="1" border size="small">
            <el-descriptions-item :label="t('transfer.taskAssistant.sourceSummary')">{{ sourceSummary }}</el-descriptions-item>
            <el-descriptions-item :label="t('transfer.taskAssistant.targetSummary')">{{ targetSummary }}</el-descriptions-item>
            <el-descriptions-item :label="t('transfer.taskAssistant.syncModeSummary')">{{ syncModeLabel }}</el-descriptions-item>
            <el-descriptions-item :label="t('transfer.taskAssistant.fieldsSummary')">
              {{ t('transfer.taskAssistant.fieldsConfirmed', { count: wizardState.fieldMappings.value.length }) }}
            </el-descriptions-item>
          </el-descriptions>
        </div>

        <el-alert v-if="message" class="transfer-ai-message" :type="status === 'success' ? 'success' : 'warning'" :title="message" :closable="false" />
        <footer class="transfer-ai-panel-footer">
          <el-button :disabled="busy" @click="close">{{ t('transfer.taskAssistant.cancel') }}</el-button>
          <el-button v-if="stage !== 'request'" :disabled="busy" @click="previousStage">{{ t('transfer.taskAssistant.previous') }}</el-button>
          <el-button type="primary" :loading="busy" :disabled="!canAdvance" @click="advance">{{ stage === 'review' ? t('transfer.taskAssistant.createTask') : t('transfer.taskAssistant.confirmAndContinue') }}</el-button>
        </footer>
      </section>
    </transition>
    <el-tooltip :content="t('transfer.taskAssistant.title')" placement="left" :disabled="visible">
      <el-button class="transfer-ai-fab" circle type="primary" :aria-label="t('transfer.taskAssistant.title')" :disabled="busy" @click="open">
        <el-icon><MagicStick /></el-icon>
      </el-button>
    </el-tooltip>
  </div>
</template>

<script setup>
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { Close, MagicStick } from '@element-plus/icons-vue'
import { defaultResourceCandidatesByRole, engineSelectionState, isEngineSelectable, normalizeFieldType, ResourceTreePicker } from '@addp/common-frontend'
import { transferCopilotAPI } from '@/api/copilot'
import { taskAPI, fieldDefinitionRecommendationAPI } from '@/api/tasks'
import { systemEnginesAPI } from '@/api/systemEngines'
import { capabilitiesAPI } from '@/api/capabilities'
import { getItemFieldsByID } from '@/api/meta'
import { getManagerPreview } from '@/api/managerPreview'
import { parseTransferLocator } from '@/utils/resourceLocator'
import { useTaskWizardState } from '../views/TaskWizard/useTaskWizardState.js'
import { hasNativeTableWriteCapability, hasStorageCapability, isNativeTableEngine } from '@/utils/transferDisplay'
import { groupResourceCandidates, inferSourceEngineFromPrompt, inferSourceEnginesFromPrompt, inferTargetEngineFromPrompt, inferTransferSyncMode, resolveAuthoritativeSourceFields, resourceCandidateKey as candidateKey, resourceFact } from '../utils/transferCopilot.mjs'
import { mysqlDecimalMappingIssues } from '../views/TaskWizard/decimalMapping.mjs'
import { CONTINUOUS_FIELD_TYPES, databaseCDCFieldTypes, isKafkaTopicSource } from '../views/TaskWizard/continuousTask.mjs'
import { inferTopicFieldRecommendations } from '../views/TaskWizard/topicFieldRecommendations.mjs'

const emit = defineEmits(['task-created'])
const { t } = useI18n()
const wizardState = useTaskWizardState()
const visible = ref(false)
const busy = ref(false)
const prompt = ref('')
const stage = ref('request')
const status = ref('')
const message = ref('')
const candidates = ref([])
const selectedByRole = ref({})
const engines = ref([])
const targetEngineId = ref(null)
const targetParentSelection = ref(null)
const targetParentLocator = ref('')
const targetTable = ref('')
const writeMode = ref('overwrite')
const syncMode = ref('snapshot')
const decimalScanRows = ref(null)

const candidateGroups = computed(() => groupResourceCandidates(candidates.value))
const selectedSource = computed(() => candidates.value.find(item => selectedByRole.value[item.role] === candidateKey(item)) || null)
const stageIndex = computed(() => ({ request: 0, source: 1, target: 2, fields: 3, review: 4 })[stage.value])
const targetEngines = computed(() => engines.value.filter(engine =>
  engine?.id &&
  hasStorageCapability(engine) &&
  isNativeTableEngine(engine) &&
  hasNativeTableWriteCapability(engine)
))
const selectedTargetEngine = computed(() => engines.value.find(engine => Number(engine.id) === Number(targetEngineId.value)) || null)
const isMysqlTarget = computed(() => String(selectedTargetEngine.value?.engine_type || '').toLowerCase().includes('mysql'))
const isKafkaSource = computed(() => isKafkaTopicSource(wizardState.sourceEngineType.value, wizardState.sourceLocator.value))
const decimalIssues = computed(() => mysqlDecimalMappingIssues(wizardState.fieldMappings.value, wizardState.sourceFields.value, [], selectedTargetEngine.value?.engine_type, wizardState.targetRepresentation.value))
const decimalIssueNames = computed(() => decimalIssues.value.map(item => item.targetField || item.sourceField).filter(Boolean).join(', '))
const fieldTypes = computed(() => syncMode.value === 'cdc'
  ? databaseCDCFieldTypes(wizardState.sourceEngineType.value)
  : syncMode.value === 'kafka'
    ? CONTINUOUS_FIELD_TYPES
    : ['string', 'bool', 'int', 'bigint', 'float', 'double', 'decimal', 'date', 'time', 'timestamp', 'json', 'uuid', 'geometry'])
const sourceFieldOptions = computed(() => fieldOptions(wizardState.sourceFields.value.map(field => ({
  name: field?.name,
  type: normalizeFieldType(field),
  primaryKey: isPrimaryKeyField(field)
}))))
const targetFieldOptions = computed(() => fieldOptions(wizardState.fieldMappings.value.map(mapping => ({
  name: mapping?.target_field,
  type: mapping?.target_type
}))))
const sourceSummary = computed(() => selectedSource.value ? `${selectedSource.value.engine_name || ''} / ${candidateDisplayPath(selectedSource.value)}` : '')
const targetSummary = computed(() => `${selectedTargetEngine.value?.name || selectedTargetEngine.value?.engine_type || ''} / ${targetParentSelection.value?.display?.path || ''} / ${targetTable.value}`)
const syncModeLabel = computed(() => ({
  snapshot: t('transfer.taskWizard.snapshotLoad'),
  incremental: t('transfer.taskWizard.watermarkIncrementalLoad'),
  cdc: t('transfer.taskWizard.databaseCDCLoad'),
  kafka: t('transfer.taskWizard.continuousIncrementalLoad')
})[syncMode.value] || '-')
const databaseCDCUnavailableText = computed(() => wizardState.databaseCDCUnavailableReasons.value
  .map(reason => t(`transfer.taskWizard.databaseCDCUnavailableReasons.${reason?.code || 'unknown'}`, {
    fields: Array.isArray(reason?.fields) ? reason.fields.join(', ') : ''
  }))
  .join('；'))
const continuousConfigIssueText = computed(() => wizardState.continuousConfigIssues.value
  .map(issue => t(`transfer.taskWizard.continuousConfigIssues.${issue?.code || 'mappingsInvalid'}`, {
    fields: Array.isArray(issue?.fields) ? issue.fields.join(', ') : ''
  }))
  .join('；'))
const syncModeAvailable = computed(() => {
  if (syncMode.value === 'incremental') return wizardState.supportsWatermarkIncremental.value
  if (syncMode.value === 'cdc') return wizardState.supportsDatabaseCDC.value
  if (syncMode.value === 'kafka') return isKafkaSource.value && wizardState.supportsContinuousTarget.value
  return true
})
const selectedModeConfigValid = computed(() => {
  if (syncMode.value === 'incremental') return wizardState.watermarkIncrementalValid.value
  if (syncMode.value === 'cdc' || syncMode.value === 'kafka') return wizardState.continuousConfigValid.value
  return true
})
const canAdvance = computed(() => {
  if (stage.value === 'request') return !!prompt.value.trim()
  if (stage.value === 'source') return !!selectedSource.value
  if (stage.value === 'target') return isEngineSelectable(selectedTargetEngine.value) && !!targetParentLocator.value && !!targetTable.value.trim() && syncModeAvailable.value && (syncMode.value !== 'incremental' || wizardState.watermarkIncrementalValid.value)
  if (stage.value === 'fields') return (wizardState.fieldMappings.value.length > 0 || wizardState.sourceFields.value.length === 0) && decimalIssues.value.length === 0 && selectedModeConfigValid.value
  return !!wizardState.taskName.value.trim() && decimalIssues.value.length === 0 && selectedModeConfigValid.value
})

function open() { visible.value = true }
function close() {
  if (busy.value) return
  visible.value = false
  reset()
}
function reset() {
  prompt.value = ''; stage.value = 'request'; status.value = ''; message.value = ''; candidates.value = []; selectedByRole.value = {}
  engines.value = []; targetEngineId.value = null; targetParentSelection.value = null; targetParentLocator.value = ''; targetTable.value = ''; writeMode.value = 'overwrite'; syncMode.value = 'snapshot'; decimalScanRows.value = null
  wizardState.reset()
}
watch(visible, value => { if (!value && !busy.value) reset() })

async function loadEngines() {
  const response = await systemEnginesAPI.list()
  engines.value = Array.isArray(response?.data) ? response.data : (response || [])
  return engines.value
}

async function loadTransferCapabilities() {
  const response = await capabilitiesAPI.get()
  const data = response?.data || response || {}
  wizardState.updateFormatCapabilities({
    databaseCDC: data?.continuous?.database_cdc || data?.continuous?.databaseCDC || null
  })
}

async function discoverSource() {
  if (busy.value || !prompt.value.trim()) return
  busy.value = true; message.value = ''
  try {
    const [list] = await Promise.all([loadEngines(), loadTransferCapabilities()])
    const sourceEngine = inferSourceEngineFromPrompt(prompt.value, list)
    const result = await transferCopilotAPI.generate({ query: prompt.value.trim(), ...(sourceEngine ? { source_engine_id: Number(sourceEngine.id) } : {}) })
    if (result?.status !== 'need_clarification' || !Array.isArray(result.data_source_candidates) || !result.data_source_candidates.length) throw new Error(result?.message || t('transfer.taskAssistant.sourceNotFound'))
    const sourceEngines = inferSourceEnginesFromPrompt(prompt.value, list)
    const sourceEngineIDs = new Set(sourceEngines.map(engine => Number(engine.id)))
    candidates.value = sourceEngineIDs.size
      ? result.data_source_candidates.filter(candidate => sourceEngineIDs.has(Number(candidate.engine_id)))
      : result.data_source_candidates
    if (!candidates.value.length) throw new Error(t('transfer.taskAssistant.sourceNotFound'))
    selectedByRole.value = defaultResourceCandidatesByRole(candidates.value)
    message.value = result.message || t('transfer.taskAssistant.sourceConfirmDescription')
    stage.value = 'source'
  } catch (error) { showError(error) } finally { busy.value = false }
}

async function confirmSource() {
  if (!selectedSource.value) return
  busy.value = true; message.value = ''
  try {
    const source = selectedSource.value
    const engine = engines.value.find(item => Number(item.id) === Number(source.engine_id))
    wizardState.applyAssistantSource(source, engine)
    const itemID = parseTransferLocator(source.locator).itemID
    let metadataFields = []
    if (itemID) {
      const response = await getItemFieldsByID(itemID)
      metadataFields = Array.isArray(response?.data) ? response.data : (response || [])
    }
    const fields = resolveAuthoritativeSourceFields(source.fields, metadataFields, itemID)
    wizardState.loadSourceFields(fields)
    if (isKafkaSource.value && wizardState.sourceFields.value.length === 0) {
      await recommendTopicFields()
    }
    await loadEngines()
    const inferredTarget = inferTargetEngineFromPrompt(prompt.value, engines.value)
    targetEngineId.value = inferredTarget ? Number(inferredTarget.id) : null
    targetTable.value = source.name || parseTransferLocator(source.locator).path.at(-1) || ''
    syncMode.value = inferTransferSyncMode(prompt.value, {
      sourceEngineType: wizardState.sourceEngineType.value,
      sourceLocator: wizardState.sourceLocator.value
    })
    applyTargetDraft()
    applySyncMode()
    stage.value = 'target'
  } catch (error) { showError(error) } finally { busy.value = false }
}

function handleTargetEngineChange() {
  targetParentSelection.value = null; targetParentLocator.value = ''
  wizardState.clearTarget(); wizardState.resetTargetFields()
  applyTargetDraft()
  applySyncMode()
}
function isTargetParentSelectable(node, context = {}) {
  const type = String(node?.type || '').toLowerCase()
  return ['schema', 'database', 'root'].includes(type) && !!context.locator?.nodeId
}
function handleTargetParentSelect(selection) {
  targetParentSelection.value = selection
  targetParentLocator.value = selection?.identity?.locator || ''
  applyTargetDraft()
  applySyncMode()
}
function handleTargetEngineDropdownVisible(visible) { if (visible) loadEngines() }
function engineLabel(engine) { return `${engine.name || engine.display_name || engine.engine_type} (${engine.engine_type || '-'}) · ${t(`common.engineStatus.${engineSelectionState(engine)}`)}` }
function candidateDisplayPath(candidate) { return candidate?.full_name || (Array.isArray(candidate?.ancestors) ? [...candidate.ancestors.map(item => item?.label), candidate?.name].filter(Boolean).join(' / ') : candidate?.name || '') }
function candidateFacts(candidate) { return [candidate?.data_type, candidate?.geometry_type, candidate?.crs].filter(Boolean).join(' · ') }
function decimalScaleMax(row) { const p = Number(row?.precision); return Number.isInteger(p) && p > 0 ? Math.min(30, p) : 30 }

function fieldOptions(fields) {
  const seen = new Set()
  return fields
    .map(field => ({
      value: String(field?.name || '').trim(),
      label: [String(field?.name || '').trim(), String(field?.type || '').trim() ? `(${field.type})` : '', field?.primaryKey ? `· ${t('transfer.taskWizard.primaryKeyField')}` : ''].filter(Boolean).join(' ')
    }))
    .filter(field => {
      const key = field.value.toLowerCase()
      if (!key || seen.has(key)) return false
      seen.add(key)
      return true
    })
}

function isPrimaryKeyField(field) {
  return field?.primary_key === true ||
    field?.primaryKey === true ||
    field?.is_primary_key === true ||
    field?.isPrimaryKey === true ||
    String(field?.key || '').trim().toLowerCase() === 'pri'
}

function applyTargetDraft() {
  const engine = selectedTargetEngine.value
  if (!engine) return
  wizardState.updateTarget({
    engineID: Number(engine.id),
    engineType: engine.engine_type,
    capabilities: engine.capabilities,
    targetType: engine.engine_type,
    representation: 'native',
    extra: {
      schema: '',
      table: targetTable.value.trim(),
      parentLocator: targetParentLocator.value,
      writeMode: writeMode.value
    }
  })
  wizardState.targetTable.value = targetTable.value.trim()
}

function applySyncMode() {
  if (isKafkaSource.value) {
    syncMode.value = 'kafka'
    wizardState.setLoadMode('incremental')
    return
  }
  wizardState.setLoadMode('snapshot')
  if (syncMode.value === 'incremental') {
    wizardState.setLoadMode('incremental')
    wizardState.initializeIncrementalDefaults()
  } else if (syncMode.value === 'cdc') {
    wizardState.setLoadMode('cdc')
  }
}

function handleWatermarkFieldChange() {
  wizardState.watermarkTieBreakers.value = wizardState.watermarkTieBreakers.value.filter(field => field !== wizardState.watermarkField.value)
  wizardState.initializeIncrementalDefaults()
}

async function recommendTopicFields() {
  const response = await getManagerPreview(wizardState.sourceLocator.value, 50)
  const preview = response?.preview_type && response?.data ? response.data : (response?.data || response)
  const recommendations = inferTopicFieldRecommendations(preview?.rows)
  if (!recommendations.length) return
  wizardState.applyTopicFieldRecommendations(recommendations.map(recommendation => ({
    ...recommendation,
    target_name: recommendation.name
  })))
}

async function confirmTarget() {
  applyTargetDraft()
  if (!syncModeAvailable.value) return
  if (!wizardState.taskName.value.trim()) {
    wizardState.taskName.value = t('transfer.taskAssistant.defaultTaskName', {
      source: selectedSource.value?.name || parseTransferLocator(selectedSource.value?.locator || '').path.at(-1) || '',
      target: selectedTargetEngine.value?.name || selectedTargetEngine.value?.engine_type || ''
    })
  }
  wizardState.autoGenerateFieldMappings()
  if (isMysqlTarget.value) await recommendDecimals()
  applySyncMode()
  if (syncMode.value === 'incremental' && !wizardState.watermarkIncrementalValid.value) return
  stage.value = 'fields'
}
async function recommendDecimals() {
  const decimalFields = wizardState.sourceFields.value.filter(field => normalizeFieldType(field) === 'decimal').map(field => field.name).filter(Boolean)
  if (!decimalFields.length) return
  const response = await fieldDefinitionRecommendationAPI.create({ source_locator: wizardState.sourceLocator.value, source_fields: decimalFields, target_engine_type: 'mysql' })
  const result = response?.data || response
  const fields = Array.isArray(result?.fields) ? result.fields.filter(item => item?.fits_target === true) : []
  wizardState.applyRecommendedDecimalDefinitions(fields)
  decimalScanRows.value = Number(result?.rows_scanned || 0).toLocaleString()
}

async function confirmFields() {
  if (!canAdvance.value) return
  stage.value = 'review'
}
async function createTask() {
  busy.value = true; message.value = ''
  try {
    const generated = await transferCopilotAPI.generate({ query: prompt.value.trim(), source_engine_id: Number(selectedSource.value.engine_id), resources: [resourceFact(selectedSource.value)], task: wizardState.taskConfig.value })
    if (generated?.status !== 'success' || !generated.task) throw new Error(generated?.message || t('transfer.taskAssistant.invalidDraft'))
    const task = generated.task
    const created = await taskAPI.create(task)
    const createdTask = created?.data || created
    status.value = 'success'; message.value = t('transfer.taskAssistant.created', { name: createdTask?.name || task.name })
    ElMessage.success(message.value)
    emit('task-created', createdTask)
    visible.value = false
    reset()
  } catch (error) { showError(error) } finally { busy.value = false }
}
function advance() { if (stage.value === 'request') return discoverSource(); if (stage.value === 'source') return confirmSource(); if (stage.value === 'target') return confirmTarget(); if (stage.value === 'fields') return confirmFields(); return createTask() }
function previousStage() { const previous = { source: 'request', target: 'source', fields: 'target', review: 'fields' }[stage.value]; if (previous) stage.value = previous }
function showError(error) {
  const detail = error.response?.data?.detail || error.response?.data?.error || error.message
  if (detail === 'transfer_inference_scenario_not_configured') {
    ElMessage.error(t('transfer.taskAssistant.inferenceNotConfigured'))
    return
  }
  if (detail === 'copilot_inference_runtime_not_initialized') {
    ElMessage.error(t('transfer.taskAssistant.inferenceUnavailable'))
    return
  }
  ElMessage.error(detail || t('transfer.taskAssistant.failed'))
}
</script>

<style scoped>
.transfer-ai-fab { position: fixed; right: 22px; bottom: 32px; z-index: 1000; width: 44px; height: 44px; box-shadow: var(--addp-shadow-hover); }
.transfer-ai-panel { position: fixed; right: 22px; bottom: 86px; z-index: 1000; width: min(720px, calc(100vw - 44px)); max-height: min(82vh, 820px); overflow: auto; padding: 16px; display: flex; flex-direction: column; gap: 12px; background: var(--addp-bg-primary); border: 1px solid var(--addp-border-color); border-radius: 8px; box-shadow: var(--addp-shadow-card); }
.transfer-ai-panel-header, .transfer-ai-panel-footer { display: flex; align-items: center; gap: 10px; }
.transfer-ai-panel-header { justify-content: space-between; }
.transfer-ai-panel-title { color: var(--addp-text-primary); font-size: 16px; font-weight: 600; line-height: 1.4; }
.transfer-ai-panel-subtitle { margin-top: 3px; color: var(--addp-text-secondary); font-size: 12px; line-height: 1.5; }
.transfer-ai-stage { min-width: 0; }
.transfer-ai-prompt { margin-top: 12px; }
.transfer-ai-candidate-group { margin-top: 12px; border: 1px solid var(--addp-border-color-light); border-radius: 6px; }
.transfer-ai-group-title { padding: 9px 12px; color: var(--addp-text-primary); font-size: 13px; font-weight: 600; border-bottom: 1px solid var(--addp-border-color-light); }
.transfer-ai-candidate-options { display: flex; width: 100%; flex-direction: column; }
.transfer-ai-candidate { width: 100%; height: auto; margin: 0; padding: 11px 12px; align-items: flex-start; }
.transfer-ai-candidate :deep(.el-radio__label) { min-width: 0; flex: 1; white-space: normal; }
.transfer-ai-candidate-content, .transfer-ai-candidate-heading, .transfer-ai-candidate-name, .transfer-ai-candidate-engine, .transfer-ai-candidate-path, .transfer-ai-candidate-facts, .transfer-ai-candidate-reason { display: block; min-width: 0; overflow-wrap: anywhere; }
.transfer-ai-candidate-heading { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; }
.transfer-ai-candidate-name { color: var(--addp-text-primary); font-weight: 600; }
.transfer-ai-candidate-engine, .transfer-ai-candidate-path, .transfer-ai-candidate-facts, .transfer-ai-candidate-reason { margin-top: 4px; color: var(--addp-text-secondary); font-size: 12px; line-height: 1.5; }
.transfer-ai-candidate-reason { color: var(--el-color-success); }
.transfer-ai-target-form, .transfer-ai-review-form { margin-top: 12px; }
.full-width { width: 100%; }
.transfer-ai-sync-modes { display: flex; flex-wrap: wrap; gap: 8px 18px; }
.transfer-ai-sync-modes :deep(.el-radio) { margin-right: 0; white-space: normal; }
.transfer-ai-mode-alert { margin: 0 0 12px; }
.transfer-ai-form-hint { margin-top: 5px; color: var(--addp-text-tertiary); font-size: 12px; line-height: 1.5; }
.transfer-ai-field-table { margin-top: 12px; }
.transfer-ai-decimal-alert { margin-top: 10px; }
.transfer-ai-fact-note { margin-top: 8px; color: var(--addp-text-secondary); font-size: 12px; }
.transfer-ai-panel-footer { justify-content: flex-end; flex-wrap: wrap; }
.transfer-ai-slide-enter-active, .transfer-ai-slide-leave-active { transition: opacity .16s ease, transform .16s ease; }
.transfer-ai-slide-enter-from, .transfer-ai-slide-leave-to { opacity: 0; transform: translateY(8px); }
@media (max-width: 760px) { .transfer-ai-fab { right: 14px; bottom: 18px; } .transfer-ai-panel { right: 14px; bottom: 72px; width: calc(100vw - 28px); max-height: min(84vh, 760px); } }
</style>
