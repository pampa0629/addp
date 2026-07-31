<template>
  <div class="data-profile-panel">
    <div class="profile-toolbar">
      <div class="profile-toolbar-status">
        <span class="profile-title">{{ t('manager.explorer.profile.title') }}</span>
        <el-tag v-if="profile" size="small" effect="plain" :type="freshnessTagType">
          {{ freshnessLabel }}
        </el-tag>
      </div>
      <el-button
        v-if="profile || latestExecution"
        size="small"
        :icon="Refresh"
        :loading="submitting || executionActive"
        @click="refreshProfile"
      >
        {{ t('manager.explorer.profile.refresh') }}
      </el-button>
    </div>

    <section v-if="profile" class="scope-band" :aria-label="t('manager.explorer.profile.dataScope')">
      <div class="scope-mode-row">
        <span class="scope-label">{{ t('manager.explorer.profile.dataScope') }}</span>
        <el-segmented
          v-model="scopeMode"
          size="small"
          :options="scopeModeOptions"
          :disabled="submitting || executionActive || !conditionSupported"
          @change="handleScopeModeChange"
        />
      </div>
      <template v-if="scopeMode === 'condition' && conditionSupported">
        <div class="condition-toolbar">
          <el-segmented v-model="conditionLogic" size="small" :options="conditionLogicOptions" />
          <el-button
            :icon="Plus"
            size="small"
            circle
            :disabled="conditions.length >= 8"
            :aria-label="t('manager.explorer.profile.addCondition')"
            @click="addCondition"
          />
        </div>
        <div class="condition-list">
          <div v-for="(condition, index) in conditions" :key="condition.id" class="condition-row">
            <el-select v-model="condition.field" filterable @change="resetCondition(condition)">
              <el-option
                v-for="field in allFields"
                :key="field.name"
                :label="field.name"
                :value="field.name"
              />
            </el-select>
            <el-select v-model="condition.operator" @change="resetConditionValue(condition)">
              <el-option
                v-for="operator in conditionOperators(condition)"
                :key="operator"
                :label="t(`manager.explorer.profile.operators.${operator}`)"
                :value="operator"
              />
            </el-select>
            <div class="condition-values">
              <template v-if="condition.operator === 'between'">
                <el-input-number
                  v-if="isNumericCondition(condition)"
                  v-model="condition.values[0]"
                  :controls="false"
                  :placeholder="t('manager.explorer.profile.rangeStart')"
                />
                <el-input v-else v-model="condition.values[0]" :placeholder="t('manager.explorer.profile.rangeStart')" />
                <span class="range-separator">-</span>
                <el-input-number
                  v-if="isNumericCondition(condition)"
                  v-model="condition.values[1]"
                  :controls="false"
                  :placeholder="t('manager.explorer.profile.rangeEnd')"
                />
                <el-input v-else v-model="condition.values[1]" :placeholder="t('manager.explorer.profile.rangeEnd')" />
              </template>
              <el-select
                v-else-if="condition.operator === 'in' || condition.operator === 'not_in'"
                v-model="condition.values"
                multiple
                filterable
                allow-create
                default-first-option
                :placeholder="t('manager.explorer.profile.multipleValues')"
              />
              <span v-else-if="isNullOperator(condition.operator)" class="no-value">
                {{ t('manager.explorer.profile.noValueRequired') }}
              </span>
              <el-select v-else-if="conditionField(condition)?.type === 'bool'" v-model="condition.value">
                <el-option :label="t('manager.explorer.booleanYes')" :value="true" />
                <el-option :label="t('manager.explorer.booleanNo')" :value="false" />
              </el-select>
              <el-input-number
                v-else-if="isNumericCondition(condition)"
                v-model="condition.value"
                :controls="false"
                :placeholder="t('manager.explorer.profile.value')"
              />
              <el-input v-else v-model="condition.value" :placeholder="conditionValuePlaceholder(condition)" />
            </div>
            <el-button
              :icon="Delete"
              size="small"
              text
              :disabled="conditions.length === 1"
              :aria-label="t('manager.explorer.profile.removeCondition')"
              @click="removeCondition(index)"
            />
          </div>
        </div>
        <div class="condition-actions">
          <el-button type="primary" size="small" :icon="Filter" :loading="submitting" @click="applyConditions">
            {{ t('manager.explorer.profile.applyConditions') }}
          </el-button>
        </div>
      </template>
    </section>

    <el-alert
      v-if="errorText"
      type="error"
      :title="errorText"
      show-icon
      :closable="false"
      class="profile-alert"
    />
    <el-alert
      v-if="current?.stale && profile"
      type="warning"
      :title="t('manager.explorer.profile.staleTitle')"
      :description="t('manager.explorer.profile.staleDescription')"
      show-icon
      :closable="false"
      class="profile-alert"
    />

    <div v-if="executionActive" class="execution-strip">
      <div>
        <div class="execution-title">{{ t('manager.explorer.profile.running') }}</div>
        <div class="execution-id">{{ activeExecution.execution_id }}</div>
      </div>
      <el-progress
        :percentage="Math.max(1, Number(activeExecution.progress || 0))"
        :indeterminate="Number(activeExecution.progress || 0) === 0"
        :show-text="Number(activeExecution.progress || 0) > 0"
        :stroke-width="8"
      />
    </div>

    <div v-if="loading && !current" class="profile-loading">
      <el-skeleton :rows="8" animated />
    </div>

    <template v-else-if="profile">
      <section class="summary-strip" :aria-label="t('manager.explorer.profile.summary')">
        <div v-for="item in summaryItems" :key="item.key" class="summary-item">
          <span class="summary-label">{{ item.label }}</span>
          <span class="summary-value" :title="item.title || item.value">{{ item.value }}</span>
        </div>
      </section>

      <section v-if="profileObservations.length" class="observation-band">
        <div class="section-heading">{{ t('manager.explorer.profile.observations') }}</div>
        <div class="observation-list">
          <el-tag
            v-for="observation in profileObservations"
            :key="`${observation.field}-${observation.code}`"
            size="small"
            effect="plain"
            :type="observationTagType(observation)"
          >
            {{ observationLabel(observation) }}
          </el-tag>
        </div>
      </section>

      <div class="field-toolbar">
        <el-input
          v-model="fieldSearch"
          :placeholder="t('manager.explorer.profile.searchFields')"
          clearable
          :prefix-icon="Search"
        />
        <el-select
          v-model="fieldTypeFilter"
          :placeholder="t('manager.explorer.profile.allTypes')"
          clearable
        >
          <el-option v-for="type in fieldTypes" :key="type" :label="type" :value="type" />
        </el-select>
        <el-select
          v-model="observationFilter"
          :placeholder="t('manager.explorer.profile.allObservations')"
          clearable
        >
          <el-option
            v-for="code in observationCodes"
            :key="code"
            :label="t(`manager.explorer.profile.observationCodes.${code}`)"
            :value="code"
          />
        </el-select>
      </div>

      <div class="profile-workspace">
        <div class="field-list">
          <el-table
            :data="filteredFields"
            size="small"
            height="100%"
            highlight-current-row
            :current-row-key="selectedField?.name"
            row-key="name"
            @row-click="selectField"
          >
            <el-table-column prop="name" :label="t('manager.explorer.profile.field')" min-width="150" show-overflow-tooltip />
            <el-table-column prop="type" :label="t('manager.explorer.profile.canonicalType')" width="110" show-overflow-tooltip />
            <el-table-column :label="t('manager.explorer.profile.nullRate')" width="90" align="right">
              <template #default="{ row }">{{ formatPercent(row.null_rate) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.explorer.profile.distinct')" width="90" align="right">
              <template #default="{ row }">{{ formatInteger(row.distinct_count) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.explorer.profile.observation')" width="110">
              <template #default="{ row }">
                <el-tag v-if="row.observations?.length" size="small" effect="plain" type="warning">
                  {{ row.observations.length }}
                </el-tag>
                <span v-else>-</span>
              </template>
            </el-table-column>
          </el-table>
        </div>

        <div v-if="selectedField" class="field-detail">
          <div class="field-detail-header">
            <div class="field-name" :title="selectedField.name">{{ selectedField.name }}</div>
            <div class="field-tags">
              <el-tag size="small" effect="plain" :title="`${t('manager.explorer.profile.canonicalType')}: ${selectedField.type}`">
                {{ t('manager.explorer.profile.canonicalType') }}: {{ selectedField.type }}
              </el-tag>
              <el-tag
                v-if="selectedField.native_type"
                size="small"
                effect="plain"
                type="info"
                :title="`${t('manager.explorer.profile.nativeType')}: ${selectedField.native_type}`"
              >
                {{ t('manager.explorer.profile.nativeType') }}: {{ selectedField.native_type }}
              </el-tag>
              <el-tag v-if="selectedField.primary_key" size="small" effect="plain" type="success">
                {{ t('manager.explorer.profile.primaryKey') }}
              </el-tag>
              <el-tag v-if="selectedField.distinct_approximate" size="small" effect="plain" type="info">
                {{ t('manager.explorer.profile.approximate') }}
              </el-tag>
            </div>
          </div>

          <div class="metric-grid">
            <div v-for="metric in selectedMetrics" :key="metric.key" class="metric-item">
              <span class="metric-label">{{ metric.label }}</span>
              <span class="metric-value" :title="metric.value">{{ metric.value }}</span>
            </div>
          </div>

          <div v-if="selectedField.observations?.length" class="field-observations">
            <el-tag
              v-for="observation in selectedField.observations"
              :key="observation.code"
              size="small"
              effect="plain"
              :type="observationTagType(observation)"
            >
              {{ observationLabel(observation, false) }}
            </el-tag>
          </div>

          <div v-if="hasSelectedChart" class="chart-section">
            <div class="section-heading">{{ chartTitle }}</div>
            <DataProfileChart :field="selectedField" :null-label="t('manager.explorer.profile.nullValue')" />
          </div>
          <el-empty v-else :description="t('manager.explorer.profile.noDistribution')" :image-size="56" />
        </div>
      </div>
    </template>

    <div v-else-if="executionActive || submitting" class="empty-state">
      <el-empty :description="t('manager.explorer.profile.waitingForFirstResult')" />
    </div>
    <div v-else class="empty-state">
      <el-empty :description="latestFailureText || t('manager.explorer.profile.noResult')">
        <el-button type="primary" :icon="Refresh" :loading="submitting" @click="refreshProfile">
          {{ t('manager.explorer.profile.retry') }}
        </el-button>
      </el-empty>
    </div>
  </div>
</template>

<script setup>
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { Delete, Filter, Plus, Refresh, Search } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import DataProfileChart from '@/components/explorer/DataProfileChart.vue'
import { dataExplorerAPI } from '@/api/dataExplorer'
import {
  buildProfileDataScope,
  isNumericProfileType,
  newProfileCondition,
  operatorsForProfileField
} from '@/utils/dataProfileScope'

const POLL_INTERVAL_MS = 2000
const ACTIVE_STATUSES = new Set(['pending', 'running'])

const props = defineProps({
  locator: { type: String, required: true },
  childName: { type: String, default: '' },
  refPath: { type: String, default: '' },
  nestedChildPath: { type: String, default: '' }
})

const { t, locale } = useI18n()
const current = ref(null)
const loading = ref(false)
const submitting = ref(false)
const errorText = ref('')
const fieldSearch = ref('')
const fieldTypeFilter = ref('')
const observationFilter = ref('')
const selectedFieldName = ref('')
const autoRequested = ref(false)
const scopeMode = ref('all')
const conditionLogic = ref('and')
const conditions = ref([])
const appliedScope = ref({ kind: 'all' })
const activeConfigHash = ref('')
let conditionID = 0
let pollTimer = null
let requestSequence = 0

const selection = computed(() => ({
  childName: props.childName,
  refPath: props.refPath,
  nestedChildPath: props.nestedChildPath
}))
const targetKey = computed(() => [props.locator, props.childName, props.refPath, props.nestedChildPath].join('|'))
const profile = computed(() => current.value?.profile || null)
const activeExecution = computed(() => current.value?.active_execution || null)
const latestExecution = computed(() => current.value?.latest_execution || null)
const profileExecution = computed(() => current.value?.profile_execution || null)
const executionActive = computed(() => ACTIVE_STATUSES.has(activeExecution.value?.status))
const allFields = computed(() => Array.isArray(profile.value?.fields) ? profile.value.fields : [])
const conditionSupported = computed(() => Boolean(current.value?.condition_supported && allFields.value.length))
const scopeModeOptions = computed(() => [
  { label: t('manager.explorer.profile.allData'), value: 'all' },
  { label: t('manager.explorer.profile.conditionRange'), value: 'condition', disabled: !conditionSupported.value }
])
const conditionLogicOptions = computed(() => [
  { label: 'AND', value: 'and' },
  { label: 'OR', value: 'or' }
])
const fieldTypes = computed(() => [...new Set(allFields.value.map(field => field.type).filter(Boolean))].sort())
const observationCodes = computed(() => [...new Set(
  allFields.value.flatMap(field => field.observations || []).map(item => item.code).filter(Boolean)
)].sort())
const filteredFields = computed(() => {
  const keyword = fieldSearch.value.trim().toLowerCase()
  return allFields.value.filter(field => {
    if (keyword && !String(field.name || '').toLowerCase().includes(keyword)) return false
    if (fieldTypeFilter.value && field.type !== fieldTypeFilter.value) return false
    if (observationFilter.value && !(field.observations || []).some(item => item.code === observationFilter.value)) return false
    return true
  })
})
const selectedField = computed(() => {
  return filteredFields.value.find(field => field.name === selectedFieldName.value) || filteredFields.value[0] || null
})
const profileObservations = computed(() => Array.isArray(profile.value?.observations) ? profile.value.observations : [])
const hasSelectedChart = computed(() => {
  return Boolean(selectedField.value?.distribution?.length || selectedField.value?.top_values?.length)
})
const chartTitle = computed(() => selectedField.value?.distribution?.length
  ? t('manager.explorer.profile.distribution')
  : t('manager.explorer.profile.topValues'))
const freshnessLabel = computed(() => current.value?.stale
  ? t('manager.explorer.profile.stale')
  : t('manager.explorer.profile.current'))
const freshnessTagType = computed(() => current.value?.stale ? 'warning' : 'success')
const latestFailureText = computed(() => {
  if (!['failed', 'timeout'].includes(latestExecution.value?.status)) return ''
  return t('manager.explorer.profile.latestFailed')
})

const formatInteger = value => {
  const number = Number(value)
  return Number.isFinite(number) ? Math.round(number).toLocaleString(locale.value) : '-'
}
const formatNumber = (value, digits = 2) => {
  const number = Number(value)
  return Number.isFinite(number)
    ? number.toLocaleString(locale.value, { maximumFractionDigits: digits })
    : '-'
}
const formatPercent = value => {
  const number = Number(value)
  return Number.isFinite(number) ? `${(number * 100).toFixed(1)}%` : '-'
}
const formatDateTime = value => {
  if (!value) return '-'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? '-' : new Intl.DateTimeFormat(locale.value, {
    year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit'
  }).format(date)
}
const shortHash = value => {
  const text = String(value || '')
  return text.length > 14 ? `${text.slice(0, 14)}...` : (text || '-')
}
const executionDuration = execution => {
  if (!execution?.started_at || !execution?.completed_at) return '-'
  const milliseconds = new Date(execution.completed_at).getTime() - new Date(execution.started_at).getTime()
  if (!Number.isFinite(milliseconds) || milliseconds < 0) return '-'
  return milliseconds < 1000
    ? t('manager.explorer.profile.durationMilliseconds', { value: milliseconds })
    : t('manager.explorer.profile.durationSeconds', { value: (milliseconds / 1000).toFixed(1) })
}

const summaryItems = computed(() => [
  { key: 'scope', label: t('manager.explorer.profile.dataScope'), value: scopeSummary.value },
  { key: 'rows', label: t('manager.explorer.profile.rows'), value: profile.value?.row_count == null ? '-' : formatInteger(profile.value.row_count) },
  { key: 'fields', label: t('manager.explorer.profile.fields'), value: formatInteger(profile.value?.field_count) },
  { key: 'mode', label: t('manager.explorer.profile.mode'), value: t('manager.explorer.profile.sampleMode') },
  { key: 'sample', label: t('manager.explorer.profile.sampleSize'), value: formatInteger(profile.value?.sample_size) },
  { key: 'scanned', label: t('manager.explorer.profile.rowsScanned'), value: formatInteger(profile.value?.rows_scanned) },
  { key: 'duration', label: t('manager.explorer.profile.duration'), value: executionDuration(profileExecution.value) },
  { key: 'profiled', label: t('manager.explorer.profile.profiledAt'), value: formatDateTime(profile.value?.profiled_at) },
  { key: 'source', label: t('manager.explorer.profile.sourceVersion'), value: shortHash(current.value?.stored_source_version), title: current.value?.stored_source_version }
])

const scopeSummary = computed(() => {
  const scope = profile.value?.data_scope
  if (scope?.kind !== 'condition') return t('manager.explorer.profile.allData')
  return t('manager.explorer.profile.conditionCount', {
    logic: String(scope.logic || 'and').toUpperCase(),
    count: scope.conditions?.length || 0
  })
})

const metric = (key, value, formatter = formatNumber) => ({
  key,
  label: t(`manager.explorer.profile.metrics.${key}`),
  value: formatter(value)
})
const selectedMetrics = computed(() => {
  const field = selectedField.value
  if (!field) return []
  const metrics = [
    metric('valueCount', field.value_count, formatInteger),
    metric('nullCount', field.null_count, formatInteger),
    metric('nullRate', field.null_rate, formatPercent),
    metric('distinctCount', field.distinct_count, formatInteger),
    metric('uniqueRate', field.unique_rate, formatPercent)
  ]
  if (field.numeric) {
    metrics.push(
      metric('min', field.numeric.min), metric('max', field.numeric.max), metric('mean', field.numeric.mean),
      metric('median', field.numeric.median), metric('p25', field.numeric.p25), metric('p75', field.numeric.p75),
      metric('p95', field.numeric.p95), metric('stddev', field.numeric.stddev),
      metric('zeroCount', field.numeric.zero_count, formatInteger), metric('negativeCount', field.numeric.negative_count, formatInteger)
    )
  }
  if (field.text) {
    metrics.push(
      metric('emptyCount', field.text.empty_count, formatInteger), metric('blankCount', field.text.blank_count, formatInteger),
      metric('minLength', field.text.min_length, formatInteger), metric('maxLength', field.text.max_length, formatInteger),
      metric('avgLength', field.text.avg_length)
    )
  }
  if (field.temporal) {
    metrics.push(metric('min', field.temporal.min, String), metric('max', field.temporal.max, String))
  }
  if (field.boolean) {
    metrics.push(
      metric('trueCount', field.boolean.true_count, formatInteger),
      metric('falseCount', field.boolean.false_count, formatInteger)
    )
  }
  if (field.spatial) {
    metrics.push(
      metric('validGeometryCount', field.spatial.valid_geometry_count, formatInteger),
      metric('invalidGeometryCount', field.spatial.invalid_geometry_count, formatInteger),
      metric('emptyGeometryCount', field.spatial.empty_geometry_count, formatInteger)
    )
  }
  return metrics
})

const observationTagType = observation => observation?.severity === 'warning' ? 'warning' : 'info'
const observationLabel = (observation, includeField = true) => {
  const label = t(`manager.explorer.profile.observationCodes.${observation.code}`)
  return includeField && observation.field ? `${observation.field}: ${label}` : label
}
const selectField = field => {
  selectedFieldName.value = field?.name || ''
}
const clearPoll = () => {
  if (pollTimer) window.clearTimeout(pollTimer)
  pollTimer = null
}
const schedulePoll = () => {
  clearPoll()
  pollTimer = window.setTimeout(() => loadCurrent(false), POLL_INTERVAL_MS)
}

const createEditorCondition = () => ({ id: ++conditionID, ...newProfileCondition(allFields.value) })
const conditionField = condition => allFields.value.find(field => field.name === condition.field)
const conditionOperators = condition => operatorsForProfileField(conditionField(condition))
const isNumericCondition = condition => isNumericProfileType(conditionField(condition)?.type)
const isNullOperator = operator => ['is_null', 'is_not_null'].includes(operator)
const conditionValuePlaceholder = condition => {
  const type = conditionField(condition)?.type
  if (['date', 'time', 'timestamp'].includes(type)) return t('manager.explorer.profile.isoValue')
  return t('manager.explorer.profile.value')
}
const resetConditionValue = condition => {
  const type = conditionField(condition)?.type
  condition.value = type === 'bool' ? true : (isNumericProfileType(type) ? null : '')
  condition.values = condition.operator === 'between'
    ? (isNumericProfileType(type) ? [null, null] : ['', ''])
    : []
}
const resetCondition = condition => {
  condition.operator = conditionOperators(condition)[0] || 'is_null'
  resetConditionValue(condition)
}
const addCondition = () => {
  if (conditions.value.length < 8) conditions.value.push(createEditorCondition())
}
const removeCondition = index => {
  if (conditions.value.length > 1) conditions.value.splice(index, 1)
}

const handleScopeModeChange = mode => {
  if (mode !== 'all') {
    if (!conditions.value.length) conditions.value = [createEditorCondition()]
    return
  }
  appliedScope.value = { kind: 'all' }
  activeConfigHash.value = ''
  current.value = null
  selectedFieldName.value = ''
  clearPoll()
  loadCurrent(true)
}

const applyConditions = async () => {
  try {
    const scope = buildProfileDataScope(allFields.value, conditionLogic.value, conditions.value)
    await startExecution(true, scope)
  } catch {
    ElMessage.warning(t('manager.explorer.profile.invalidConditions'))
  }
}

const startExecution = async (manual = false, scope = appliedScope.value) => {
  if (!props.locator || submitting.value || executionActive.value) return
  submitting.value = true
  errorText.value = ''
  try {
    const response = await dataExplorerAPI.createDataProfileExecution(props.locator, selection.value, scope)
    activeConfigHash.value = response?.profile_config_hash || ''
    appliedScope.value = response?.data_scope || scope
    current.value = {
      ...(current.value || {}),
      active_execution: response?.execution || null,
      latest_execution: response?.execution || current.value?.latest_execution || null
    }
    if (manual) ElMessage.success(t('manager.explorer.profile.refreshSubmitted'))
    schedulePoll()
  } catch (error) {
    const message = error.response?.data?.error || error.message || t('manager.explorer.profile.requestFailed')
    errorText.value = message
    if (manual) ElMessage.error(message)
  } finally {
    submitting.value = false
  }
}

const loadCurrent = async (allowAutoStart = true) => {
  const sequence = ++requestSequence
  if (!current.value) loading.value = true
  errorText.value = ''
  try {
    const response = await dataExplorerAPI.getDataProfileCurrent(props.locator, selection.value, activeConfigHash.value)
    if (sequence !== requestSequence) return
    if (activeConfigHash.value && !response?.profile && ['failed', 'timeout'].includes(response?.latest_execution?.status)) {
      const message = response.latest_execution.error || t('manager.explorer.profile.latestFailed')
      activeConfigHash.value = ''
      appliedScope.value = { kind: 'all' }
      scopeMode.value = 'all'
      clearPoll()
      ElMessage.error(message)
      await loadCurrent(false)
      return
    }
    current.value = response
    if (response?.profile?.data_scope?.kind === 'condition') {
      scopeMode.value = 'condition'
      appliedScope.value = response.profile.data_scope
    }
    if (!selectedFieldName.value && response?.profile?.fields?.length) {
      selectedFieldName.value = response.profile.fields[0].name
    }
    if (ACTIVE_STATUSES.has(response?.active_execution?.status)) {
      schedulePoll()
    } else {
      clearPoll()
      if (allowAutoStart && !activeConfigHash.value && !response?.profile && !autoRequested.value) {
        autoRequested.value = true
        await startExecution(false)
      }
    }
  } catch (error) {
    if (sequence !== requestSequence) return
    clearPoll()
    errorText.value = error.response?.data?.error || error.message || t('manager.explorer.profile.requestFailed')
  } finally {
    if (sequence === requestSequence) loading.value = false
  }
}

const refreshProfile = () => {
  autoRequested.value = true
  startExecution(true)
}

watch(targetKey, () => {
  clearPoll()
  requestSequence += 1
  current.value = null
  errorText.value = ''
  selectedFieldName.value = ''
  autoRequested.value = false
  scopeMode.value = 'all'
  conditions.value = []
  conditionLogic.value = 'and'
  appliedScope.value = { kind: 'all' }
  activeConfigHash.value = ''
  loadCurrent(true)
})

onMounted(() => loadCurrent(true))
onBeforeUnmount(() => {
  requestSequence += 1
  clearPoll()
})
</script>

<style scoped>
.data-profile-panel {
  height: 100%;
  min-height: 0;
  overflow: auto;
  display: flex;
  flex-direction: column;
  background: var(--addp-bg-primary) !important;
}

.profile-toolbar {
  min-height: 48px;
  padding: 8px 14px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  border-bottom: 1px solid var(--addp-border-color);
}

.profile-toolbar-status,
.field-detail-header,
.field-tags,
.observation-list,
.field-observations {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
  flex-wrap: wrap;
}

.field-tags {
  min-width: 0;
  flex-wrap: wrap;
}

.field-tags :deep(.el-tag) {
  max-width: 100%;
}

.field-tags :deep(.el-tag__content) {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.profile-title,
.section-heading {
  color: var(--addp-text-primary);
  font-size: 14px;
  font-weight: 600;
}

.scope-band {
  padding: 10px 14px;
  display: flex;
  flex-direction: column;
  gap: 10px;
  border-bottom: 1px solid var(--addp-border-color);
  background: var(--addp-bg-secondary) !important;
}

.scope-mode-row,
.condition-toolbar,
.condition-actions {
  display: flex;
  align-items: center;
  gap: 10px;
}

.scope-mode-row {
  justify-content: space-between;
}

.scope-label {
  color: var(--addp-text-primary);
  font-size: 13px;
  font-weight: 600;
}

.condition-toolbar {
  justify-content: flex-end;
}

.condition-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.condition-row {
  min-width: 0;
  display: grid;
  grid-template-columns: minmax(150px, 1fr) 150px minmax(220px, 2fr) 32px;
  align-items: center;
  gap: 8px;
}

.condition-values {
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 8px;
}

.condition-values > :deep(.el-input),
.condition-values > :deep(.el-input-number),
.condition-values > :deep(.el-select) {
  width: 100%;
}

.range-separator,
.no-value {
  flex: 0 0 auto;
  color: var(--addp-text-secondary);
  font-size: 12px;
}

.condition-actions {
  justify-content: flex-end;
}

.profile-alert {
  margin: 10px 14px 0;
  width: auto;
}

.execution-strip {
  padding: 10px 14px;
  display: grid;
  grid-template-columns: minmax(180px, 280px) minmax(220px, 1fr);
  align-items: center;
  gap: 18px;
  border-bottom: 1px solid var(--addp-border-color-light);
  background: var(--addp-bg-secondary) !important;
}

.execution-title {
  color: var(--addp-text-primary);
  font-size: 13px;
  font-weight: 600;
}

.execution-id {
  margin-top: 2px;
  overflow: hidden;
  color: var(--addp-text-secondary);
  font-family: monospace;
  font-size: 11px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.profile-loading,
.empty-state {
  padding: 28px;
}

.summary-strip {
  display: grid;
  grid-template-columns: repeat(4, minmax(130px, 1fr));
  border-bottom: 1px solid var(--addp-border-color);
}

.summary-item {
  min-width: 0;
  padding: 10px 14px;
  display: flex;
  flex-direction: column;
  gap: 3px;
  border-right: 1px solid var(--addp-border-color-light);
}

.summary-label,
.metric-label {
  color: var(--addp-text-secondary);
  font-size: 12px;
}

.summary-value,
.metric-value {
  overflow: hidden;
  color: var(--addp-text-primary);
  font-size: 14px;
  font-weight: 600;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.observation-band {
  padding: 10px 14px;
  display: flex;
  align-items: flex-start;
  gap: 14px;
  border-bottom: 1px solid var(--addp-border-color-light);
}

.observation-band .section-heading {
  flex: 0 0 auto;
  line-height: 24px;
}

.field-toolbar {
  padding: 10px 14px;
  display: grid;
  grid-template-columns: minmax(220px, 1fr) 160px 190px;
  gap: 10px;
  border-bottom: 1px solid var(--addp-border-color);
}

.profile-workspace {
  min-height: 520px;
  flex: 1;
  display: grid;
  grid-template-columns: minmax(460px, 44%) minmax(360px, 1fr);
}

.field-list {
  min-height: 520px;
  border-right: 1px solid var(--addp-border-color);
}

.field-detail {
  min-width: 0;
  padding: 14px;
  overflow: auto;
}

.field-detail-header {
  justify-content: space-between;
  margin-bottom: 14px;
}

.field-name {
  min-width: 0;
  overflow: hidden;
  color: var(--addp-text-primary);
  font-size: 16px;
  font-weight: 600;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.metric-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(100px, 1fr));
  border-top: 1px solid var(--addp-border-color-light);
  border-left: 1px solid var(--addp-border-color-light);
}

.metric-item {
  min-width: 0;
  padding: 9px 10px;
  display: flex;
  flex-direction: column;
  gap: 3px;
  border-right: 1px solid var(--addp-border-color-light);
  border-bottom: 1px solid var(--addp-border-color-light);
}

.field-observations {
  margin-top: 14px;
}

.chart-section {
  margin-top: 18px;
}

.chart-section .section-heading {
  margin-bottom: 6px;
}

@media (max-width: 1100px) {
  .summary-strip {
    grid-template-columns: repeat(2, minmax(130px, 1fr));
  }

  .profile-workspace {
    grid-template-columns: 1fr;
  }

  .field-list {
    height: 440px;
    min-height: 440px;
    border-right: 0;
    border-bottom: 1px solid var(--addp-border-color);
  }
}

@media (max-width: 720px) {
  .execution-strip,
  .field-toolbar {
    grid-template-columns: 1fr;
  }

  .metric-grid {
    grid-template-columns: repeat(2, minmax(100px, 1fr));
  }

  .scope-mode-row {
    align-items: flex-start;
    flex-direction: column;
  }

  .condition-row {
    grid-template-columns: 1fr 1fr 32px;
  }

  .condition-values {
    grid-column: 1 / -1;
    grid-row: 2;
  }
}
</style>
