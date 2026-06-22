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

      <!-- 调度计划 -->
      <el-form-item :label="t('transfer.taskWizard.scheduleLabel2')">
        <el-radio-group v-model="scheduleMode">
          <el-radio value="once">{{ t('transfer.taskWizard.runOnce') }}</el-radio>
          <el-radio value="cron">{{ t('transfer.taskWizard.cronSchedule') }}</el-radio>
        </el-radio-group>
      </el-form-item>

      <el-form-item v-if="scheduleMode === 'cron'" :label="t('transfer.taskWizard.cronExpression')">
        <ScheduleConfig
          v-model="formData.schedule"
          :preset-list="transferSchedulePresets"
          :allow-custom-cron="true"
        />
      </el-form-item>

      <el-form-item v-if="scheduleMode === 'cron'" :label="t('transfer.taskWizard.enableScheduleLabel')">
        <el-switch v-model="formData.enabled" />
        <span class="form-hint">{{ t('transfer.taskWizard.enableScheduleHint') }}</span>
      </el-form-item>

      <!-- 高级选项 -->
      <el-divider content-position="left">{{ t('transfer.taskWizard.advancedOptions') }}</el-divider>

      <el-form-item :label="t('transfer.taskWizard.batchProcessSize')">
        <el-input-number
          v-model="formData.batchSize"
          :min="100"
          :max="50000"
          :step="100"
        />
        <span class="form-hint">{{ t('transfer.taskWizard.batchProcessHint') }}</span>
      </el-form-item>
    </el-form>
  </div>
</template>

<script setup>
import { reactive, ref, watch } from 'vue'
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
const scheduleMode = ref('once')
const formData = reactive({
  taskName: props.wizardState.taskName.value,
  taskDescription: props.wizardState.taskDescription.value,
  schedule: props.wizardState.schedule.value,
  enabled: props.wizardState.enabled.value,
  batchSize: props.wizardState.batchSize.value
})

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
  },
  { deep: true, immediate: true }
)

watch(scheduleMode, (mode) => {
  props.wizardState.schedule.value = mode === 'cron' ? formData.schedule : ''
  props.wizardState.enabled.value = mode === 'cron' ? formData.enabled : false
})
</script>

<style scoped>
.step4-configure {
  max-width: 800px;
  margin: 0 auto;
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
</style>
