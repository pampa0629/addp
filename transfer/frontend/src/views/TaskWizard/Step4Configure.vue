<template>
  <div class="step4-configure">
    <h3>{{ t('transfer.taskWizard.configPage') }}</h3>
    <p class="step-description">{{ t('transfer.taskWizard.configPageDesc') }}</p>

    <el-form :model="formData" label-width="120px" :rules="rules" ref="formRef">
      <!-- 任务名称 -->
      <el-form-item :label="t('transfer.taskWizard.taskNameLabel2')" prop="taskName" required>
        <el-input
          v-model="formData.taskName"
          :placeholder="t('transfer.taskWizard.taskNamePlaceholder2')"
          maxlength="50"
          show-word-limit
        />
      </el-form-item>

      <!-- 任务描述 -->
      <el-form-item :label="t('transfer.taskWizard.taskDescLabel2')" prop="taskDescription">
        <el-input
          v-model="formData.taskDescription"
          type="textarea"
          :rows="3"
          :placeholder="t('transfer.taskWizard.taskDescPlaceholder')"
          maxlength="200"
          show-word-limit
        />
      </el-form-item>

      <el-divider content-position="left">{{ t('transfer.taskWizard.loadSettings') }}</el-divider>

      <el-alert
        v-if="isContinuousTask"
				:title="isDatabaseCDCTask ? t('transfer.taskWizard.cdcSyncTitle') : t('transfer.taskWizard.continuousSyncTitle')"
				:description="isDatabaseCDCTask ? t('transfer.taskWizard.cdcSyncDesc') : t('transfer.taskWizard.continuousSyncDesc')"
        type="info"
        :closable="false"
        show-icon
        class="incremental-alert"
      />

			<el-form-item v-if="!isKafkaContinuousTask" :label="t('transfer.taskWizard.loadModeLabel')">
				<el-radio-group v-model="formData.loadMode">
					<el-radio value="snapshot">{{ t('transfer.taskWizard.snapshotLoad') }}</el-radio>
					<el-radio value="incremental" :disabled="!watermarkIncrementalSupported">
						{{ t('transfer.taskWizard.watermarkIncrementalLoad') }}
					</el-radio>
					<el-radio value="cdc" :disabled="!databaseCDCSupported">
						{{ t('transfer.taskWizard.databaseCDCLoad') }}
					</el-radio>
				</el-radio-group>
				<ul v-if="databaseCDCUnavailableReasons.length" class="field-hint block-hint cdc-unavailable-reasons">
					<li v-for="reason in databaseCDCUnavailableReasons" :key="reason.code">
						{{ databaseCDCReasonText(reason) }}
					</li>
				</ul>
			</el-form-item>

      <template v-if="isContinuousTask">
        <el-form-item :label="t('transfer.taskWizard.continuousKeyFieldsLabel')" required>
          <el-select
            v-model="formData.continuousKeyFields"
            multiple
            filterable
            :placeholder="t('transfer.taskWizard.continuousKeyFieldsPlaceholder')"
						:disabled="isDatabaseCDCTask"
            class="field-select"
          >
            <el-option
              v-for="field in continuousSourceFieldOptions"
              :key="field.value"
              :label="field.label"
              :value="field.value"
            />
          </el-select>
          <div class="field-hint block-hint">{{ t('transfer.taskWizard.continuousKeyFieldsHint') }}</div>
        </el-form-item>

				<el-alert
					v-if="isDatabaseCDCTask"
					:title="t('transfer.taskWizard.cdcLifecycleWarningTitle')"
					:description="t('transfer.taskWizard.cdcLifecycleWarning')"
					type="warning"
					:closable="false"
					show-icon
					class="incremental-alert"
				/>

        <el-form-item :label="t('transfer.taskWizard.continuousTargetKeysLabel')">
          <div class="derived-value">{{ continuousTargetKeyText }}</div>
          <div class="field-hint block-hint">{{ t('transfer.taskWizard.continuousTargetKeysHint') }}</div>
        </el-form-item>

				<el-form-item v-if="isKafkaContinuousTask" :label="t('transfer.taskWizard.continuousInitialPositionLabel')" required>
          <el-radio-group v-model="formData.continuousInitialPosition">
            <el-radio value="earliest">{{ t('transfer.taskWizard.continuousInitialEarliest') }}</el-radio>
            <el-radio value="latest">{{ t('transfer.taskWizard.continuousInitialLatest') }}</el-radio>
          </el-radio-group>
          <div class="field-hint block-hint">{{ t('transfer.taskWizard.continuousInitialPositionHint') }}</div>
        </el-form-item>
      </template>

      <template v-if="!isContinuousTask && formData.loadMode === 'incremental'">
        <el-alert
          :title="t('transfer.taskWizard.watermarkIncrementalNoticeTitle')"
          :description="t('transfer.taskWizard.watermarkIncrementalNotice')"
          type="warning"
          :closable="false"
          show-icon
          class="incremental-alert"
        />

        <el-form-item :label="t('transfer.taskWizard.watermarkFieldLabel')" required>
          <el-select
            v-model="formData.watermarkField"
            filterable
            :placeholder="t('transfer.taskWizard.watermarkFieldPlaceholder')"
            class="field-select"
          >
            <el-option
              v-for="field in sourceFieldOptions"
              :key="field.value"
              :label="field.label"
              :value="field.value"
            />
          </el-select>
          <div class="field-hint block-hint">{{ t('transfer.taskWizard.watermarkFieldHint') }}</div>
          <div v-if="selectedWatermarkIsPrimaryKey" class="field-warning block-hint">
            {{ t('transfer.taskWizard.watermarkPrimaryKeyWarning') }}
          </div>
        </el-form-item>

        <el-form-item :label="t('transfer.taskWizard.tieBreakerLabel')" required>
          <el-select
            v-model="formData.watermarkTieBreakers"
            multiple
            filterable
            :placeholder="t('transfer.taskWizard.tieBreakerPlaceholder')"
            class="field-select"
          >
            <el-option
              v-for="field in sourceFieldOptions"
              :key="field.value"
              :label="field.label"
              :value="field.value"
              :disabled="field.value === formData.watermarkField"
            />
          </el-select>
          <div class="field-hint block-hint">{{ t('transfer.taskWizard.tieBreakerHint') }}</div>
        </el-form-item>

        <el-form-item :label="t('transfer.taskWizard.targetKeysLabel')" required>
          <el-select
            v-model="formData.targetKeys"
            multiple
            filterable
            :placeholder="t('transfer.taskWizard.targetKeysPlaceholder')"
            class="field-select"
          >
            <el-option
              v-for="field in targetFieldOptions"
              :key="field.value"
              :label="field.label"
              :value="field.value"
            />
          </el-select>
          <div class="field-hint block-hint">{{ t('transfer.taskWizard.targetKeysHint') }}</div>
        </el-form-item>
      </template>

      <!-- 调度计划 -->
      <el-form-item v-if="!isContinuousTask" :label="t('transfer.taskWizard.scheduleLabel2')">
        <el-radio-group v-model="scheduleMode">
          <el-radio value="once">{{ t('transfer.taskWizard.runOnce') }}</el-radio>
          <el-radio value="cron">{{ t('transfer.taskWizard.cronSchedule') }}</el-radio>
        </el-radio-group>
      </el-form-item>

      <el-form-item v-if="!isContinuousTask && scheduleMode === 'cron'" :label="t('transfer.taskWizard.cronExpression')">
        <ScheduleConfig
          v-model="formData.schedule"
          :preset-list="transferSchedulePresets"
          :allow-custom-cron="true"
        />
      </el-form-item>

      <el-form-item v-if="!isContinuousTask && scheduleMode === 'cron'" :label="t('transfer.taskWizard.enableScheduleLabel')">
        <el-switch v-model="formData.enabled" />
        <span class="form-hint">{{ t('transfer.taskWizard.enableScheduleHint') }}</span>
      </el-form-item>

      <!-- 高级选项 -->
      <el-divider content-position="left">{{ t('transfer.taskWizard.advancedOptions') }}</el-divider>

      <el-form-item v-if="!isContinuousTask" :label="t('transfer.taskWizard.batchProcessSize')">
        <el-input-number
          v-model="formData.batchSize"
          :min="100"
          :max="50000"
          :step="100"
        />
        <span class="form-hint">{{ t('transfer.taskWizard.batchProcessHint') }}</span>
      </el-form-item>

      <el-form-item v-else :label="t('transfer.taskWizard.continuousPollBatchSizeLabel')">
        <el-input-number
          v-model="formData.continuousPollBatchSize"
          :min="1"
          :max="50000"
          :step="100"
        />
        <span class="form-hint">{{ t('transfer.taskWizard.continuousPollBatchSizeHint') }}</span>
      </el-form-item>
    </el-form>
  </div>
</template>

<script setup>
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ScheduleConfig } from '@common-ui'

const { t } = useI18n()

const props = defineProps({
  wizardState: {
    type: Object,
    required: true
  }
})

const formRef = ref(null)
const scheduleMode = ref(props.wizardState.schedule.value ? 'cron' : 'once')
const formData = reactive({
  taskName: props.wizardState.taskName.value,
  taskDescription: props.wizardState.taskDescription.value,
  schedule: props.wizardState.schedule.value,
  enabled: props.wizardState.enabled.value,
  batchSize: props.wizardState.batchSize.value,
  loadMode: props.wizardState.loadMode.value,
  watermarkField: props.wizardState.watermarkField.value,
  watermarkTieBreakers: [...props.wizardState.watermarkTieBreakers.value],
  targetKeys: [...props.wizardState.targetKeys.value],
  continuousKeyFields: [...props.wizardState.continuousKeyFields.value],
  continuousInitialPosition: props.wizardState.continuousInitialPosition.value,
  continuousPollBatchSize: props.wizardState.continuousPollBatchSize.value
})

const isContinuousTask = computed(() => props.wizardState.isContinuousTask.value)
const isKafkaContinuousTask = computed(() => props.wizardState.isKafkaContinuousTask.value)
const isDatabaseCDCTask = computed(() => props.wizardState.isDatabaseCDCTask.value)
const watermarkIncrementalSupported = computed(() => props.wizardState.supportsWatermarkIncremental.value)
const databaseCDCSupported = computed(() => props.wizardState.supportsDatabaseCDC.value)
const databaseCDCUnavailableReasons = computed(() => props.wizardState.databaseCDCUnavailableReasons.value)

function databaseCDCReasonText(reason) {
	const key = `transfer.taskWizard.databaseCDCUnavailableReasons.${reason?.code || 'unknown'}`
	return t(key, { fields: Array.isArray(reason?.fields) ? reason.fields.join(', ') : '' })
}

const continuousSourceFieldOptions = computed(() => {
  return uniqueFieldOptions(
    props.wizardState.fieldMappings.value.map(mapping => ({
      value: String(mapping?.source_field || '').trim(),
      type: String(mapping?.target_type || '').trim()
    }))
  )
})

const continuousTargetKeyText = computed(() => {
  return props.wizardState.continuousTargetKeys.value.join(', ') || t('transfer.taskWizard.notConfigured')
})

const sourceFieldOptions = computed(() => {
  return uniqueFieldOptions(
    props.wizardState.sourceFields.value.map(field => ({
      value: String(field?.name || '').trim(),
      type: String(field?.type || '').trim(),
      primaryKey: isPrimaryKeyField(field)
    }))
  )
})

const selectedWatermarkIsPrimaryKey = computed(() => {
  return sourceFieldOptions.value.some(field => field.value === formData.watermarkField && field.primaryKey)
})

const targetFieldOptions = computed(() => {
  return uniqueFieldOptions(
    props.wizardState.fieldMappings.value.map(mapping => ({
      value: String(mapping?.target_field || '').trim(),
      type: String(mapping?.target_type || '').trim()
    }))
  )
})

function uniqueFieldOptions(fields) {
  const seen = new Set()
  return fields
    .filter(field => field.value)
    .filter(field => {
      const key = field.value.toLowerCase()
      if (seen.has(key)) return false
      seen.add(key)
      return true
    })
    .map(field => ({
      value: field.value,
      label: field.type
        ? `${field.value} (${field.type})${field.primaryKey ? ` · ${t('transfer.taskWizard.primaryKeyField')}` : ''}`
        : field.value,
      primaryKey: field.primaryKey === true
    }))
}

function isPrimaryKeyField(field) {
  return field?.primary_key === true ||
    field?.primaryKey === true ||
    field?.is_primary_key === true ||
		field?.isPrimaryKey === true ||
		String(field?.key || '').trim().toLowerCase() === 'pri'
}

const transferSchedulePresets = [
  {
    key: 'transfer-hourly',
    i18nKey: 'transferHourly',
    label: t('transfer.taskWizard.cronPresetEveryHour'),
    cron: '0 0 * * * *',
    description: t('transfer.taskWizard.cronPresetEveryHour')
  },
  {
    key: 'transfer-daily-midnight',
    i18nKey: 'transferDailyMidnight',
    label: t('transfer.taskWizard.cronPresetEveryDayMidnight'),
    cron: '0 0 0 * * *',
    description: t('transfer.taskWizard.cronPresetEveryDayMidnight')
  },
  {
    key: 'transfer-daily-8',
    i18nKey: 'transferDaily8',
    label: t('transfer.taskWizard.cronPresetEveryDay8'),
    cron: '0 0 8 * * *',
    description: t('transfer.taskWizard.cronPresetEveryDay8')
  },
  {
    key: 'transfer-weekly-monday',
    i18nKey: 'transferWeeklyMonday',
    label: t('transfer.taskWizard.cronPresetEveryMonday'),
    cron: '0 0 0 * * 1',
    description: t('transfer.taskWizard.cronPresetEveryMonday')
  },
  {
    key: 'transfer-monthly-first',
    i18nKey: 'transferMonthlyFirst',
    label: t('transfer.taskWizard.cronPresetEveryMonth1'),
    cron: '0 0 0 1 * *',
    description: t('transfer.taskWizard.cronPresetEveryMonth1')
  }
]

const rules = {
  taskName: [
    { required: true, message: t('transfer.taskWizard.taskNameRequired2'), trigger: 'blur' },
    { min: 2, max: 50, message: t('transfer.taskWizard.taskNameLengthRule'), trigger: 'blur' }
  ]
}

watch(
  formData,
  (value) => {
    props.wizardState.taskName.value = value.taskName
    props.wizardState.taskDescription.value = value.taskDescription
    props.wizardState.schedule.value = scheduleMode.value === 'cron' ? value.schedule : ''
    props.wizardState.enabled.value = scheduleMode.value === 'cron' ? value.enabled : false
    props.wizardState.batchSize.value = value.batchSize
    props.wizardState.watermarkField.value = value.watermarkField
    props.wizardState.watermarkTieBreakers.value = [...value.watermarkTieBreakers]
    props.wizardState.targetKeys.value = [...value.targetKeys]
    props.wizardState.updateContinuousKeyFields(value.continuousKeyFields)
    props.wizardState.continuousInitialPosition.value = value.continuousInitialPosition
    props.wizardState.continuousPollBatchSize.value = value.continuousPollBatchSize
  },
  { deep: true, immediate: true }
)

watch(scheduleMode, (mode) => {
  props.wizardState.schedule.value = mode === 'cron' ? formData.schedule : ''
  props.wizardState.enabled.value = mode === 'cron' ? formData.enabled : false
})

watch(isContinuousTask, (continuous) => {
  if (!continuous) return
  scheduleMode.value = 'once'
  formData.schedule = ''
  formData.enabled = false
  props.wizardState.schedule.value = ''
  props.wizardState.enabled.value = false
}, { immediate: true })

watch(
  () => props.wizardState.continuousKeyFields.value,
  (fields) => {
    formData.continuousKeyFields = [...fields]
  },
  { deep: true }
)

watch(
  () => formData.loadMode,
  (mode) => {
		props.wizardState.setLoadMode(mode)
    if (!isContinuousTask.value && mode === 'incremental') {
      props.wizardState.initializeIncrementalDefaults()
      formData.watermarkField = props.wizardState.watermarkField.value
      formData.watermarkTieBreakers = [...props.wizardState.watermarkTieBreakers.value]
      formData.targetKeys = [...props.wizardState.targetKeys.value]
    }
  }
)

watch(watermarkIncrementalSupported, (supported) => {
  if (!supported && formData.loadMode === 'incremental') {
    formData.loadMode = 'snapshot'
  }
})

watch(databaseCDCSupported, (supported) => {
	if (!supported && formData.loadMode === 'cdc') {
		formData.loadMode = 'snapshot'
	}
})

watch(
  () => formData.watermarkField,
  (field) => {
    formData.watermarkTieBreakers = formData.watermarkTieBreakers.filter(item => item !== field)
    props.wizardState.watermarkField.value = field
    props.wizardState.watermarkTieBreakers.value = [...formData.watermarkTieBreakers]
    props.wizardState.initializeIncrementalDefaults()
    formData.watermarkTieBreakers = [...props.wizardState.watermarkTieBreakers.value]
    formData.targetKeys = [...props.wizardState.targetKeys.value]
  }
)
</script>

<style scoped>
.step4-configure {
  max-width: 800px;
  margin: 0 auto;
}

.cdc-unavailable-reasons {
	margin: 6px 0 0;
	padding-left: 20px;
}

.step-description {
  color: var(--addp-text-secondary);
  margin-bottom: 30px;
}

.form-hint {
  margin-left: 12px;
  font-size: 12px;
  color: var(--addp-text-tertiary);
}

.field-select {
  width: 100%;
}

.field-hint {
  font-size: 12px;
  color: var(--addp-text-tertiary);
  line-height: 1.5;
}

.field-warning {
  font-size: 12px;
  color: var(--el-color-warning);
  line-height: 1.5;
}

.block-hint {
  width: 100%;
  margin-top: 6px;
}

.incremental-alert {
  margin-bottom: 18px;
}

.derived-value {
  min-height: 32px;
  line-height: 32px;
  color: var(--addp-text-primary);
}
</style>
