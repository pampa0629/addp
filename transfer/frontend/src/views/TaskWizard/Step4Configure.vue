<template>
  <div class="step4-configure">
    <h3>{{ t('transfer.taskWizard.configPage') }}</h3>
    <p class="step-description">{{ t('transfer.taskWizard.configPageDesc') }}</p>

    <el-form :model="wizardState" label-width="120px" :rules="rules" ref="formRef">
      <!-- 任务名称 -->
      <el-form-item :label="t('transfer.taskWizard.taskNameLabel2')" prop="taskName" required>
        <el-input
          v-model="wizardState.taskName.value"
          :placeholder="t('transfer.taskWizard.taskNamePlaceholder2')"
          maxlength="50"
          show-word-limit
        />
      </el-form-item>

      <!-- 任务描述 -->
      <el-form-item :label="t('transfer.taskWizard.taskDescLabel2')" prop="taskDescription">
        <el-input
          v-model="wizardState.taskDescription.value"
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
        <div class="cron-config">
          <el-input
            v-model="wizardState.schedule.value"
            :placeholder="t('transfer.taskWizard.cronPlaceholder')"
            style="margin-bottom: 12px"
          />
          <div class="cron-presets">
            <el-tag
              v-for="preset in cronPresets"
              :key="preset.value"
              @click="wizardState.schedule.value = preset.value"
              style="cursor: pointer; margin-right: 8px; margin-bottom: 8px"
            >
              {{ preset.label }}
            </el-tag>
          </div>
          <div class="cron-hint">
            <el-alert
              :title="t('transfer.taskWizard.cronFormat')"
              type="info"
              :closable="false"
            />
          </div>
        </div>
      </el-form-item>

      <el-form-item v-if="scheduleMode === 'cron'" :label="t('transfer.taskWizard.enableScheduleLabel')">
        <el-switch v-model="wizardState.enabled.value" />
        <span class="form-hint">{{ t('transfer.taskWizard.enableScheduleHint') }}</span>
      </el-form-item>

      <!-- 高级选项 -->
      <el-divider content-position="left">{{ t('transfer.taskWizard.advancedOptions') }}</el-divider>

      <el-form-item :label="t('transfer.taskWizard.batchProcessSize')">
        <el-input-number
          v-model="wizardState.batchSize.value"
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
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

const props = defineProps({
  wizardState: {
    type: Object,
    required: true
  }
})

const formRef = ref(null)
const scheduleMode = ref('once')

const cronPresets = [
  { label: t('transfer.taskWizard.cronPresetEveryHour'), value: '0 0 * * * *' },
  { label: t('transfer.taskWizard.cronPresetEveryDayMidnight'), value: '0 0 0 * * *' },
  { label: t('transfer.taskWizard.cronPresetEveryDay8'), value: '0 0 8 * * *' },
  { label: t('transfer.taskWizard.cronPresetEveryMonday'), value: '0 0 0 * * 1' },
  { label: t('transfer.taskWizard.cronPresetEveryMonth1'), value: '0 0 0 1 * *' }
]

const rules = {
  taskName: [
    { required: true, message: t('transfer.taskWizard.taskNameRequired2'), trigger: 'blur' },
    { min: 2, max: 50, message: t('transfer.taskWizard.taskNameLengthRule'), trigger: 'blur' }
  ]
}
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

.cron-config {
  width: 100%;
}

.cron-presets {
  margin-bottom: 12px;
}

.cron-hint {
  margin-top: 8px;
}

.form-hint {
  margin-left: 12px;
  font-size: 12px;
  color: var(--addp-text-tertiary);
}
</style>
