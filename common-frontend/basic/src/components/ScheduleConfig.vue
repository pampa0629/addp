<template>
  <div
    class="schedule-config"
    :class="{
      'compact': compactMode,
      'readonly': readonly,
      'disabled': disabled
    }"
  >
    <!-- 快捷选择和操作按钮 -->
    <div v-if="showPresets" class="schedule-presets">
      <span class="schedule-presets__label">{{ t('schedule.title') }}</span>
      <span class="schedule-presets__sublabel">{{ t('schedule.quickSelect') }}</span>
      <el-button
        v-for="preset in effectivePresets"
        :key="preset.key"
        size="small"
        :type="isPresetSelected(preset.key) ? 'primary' : 'default'"
        :disabled="disabled || readonly"
        :title="t(`schedule.presetDesc.${preset.i18nKey}`, preset.description)"
        @click="handlePresetClick(preset.key)"
      >
        {{ t(`schedule.presetLabel.${preset.i18nKey}`, preset.label) }}
      </el-button>
      <el-button
        size="small"
        :type="isCustomSchedule ? 'primary' : 'default'"
        :disabled="disabled || readonly"
        :title="t('schedule.customTitle')"
        @click="openDialog"
      >
        {{ t('schedule.custom') }}
      </el-button>
      <el-button
        v-if="modelValue"
        size="small"
        text
        :disabled="disabled || readonly"
        :title="t('schedule.clearTitle')"
        @click="clearSchedule"
      >
        {{ t('schedule.clearSchedule') }}
      </el-button>
    </div>

    <!-- 当前配置显示 -->
    <div v-if="showPresets && modelValue" class="schedule-current">
      <el-icon class="schedule-current__icon"><Clock /></el-icon>
      <span class="schedule-current__label">{{ t('schedule.current') }}</span>
      <span class="schedule-current__value">{{ description }}</span>
    </div>

    <!-- 自定义配置对话框 -->
    <el-dialog
      v-model="dialogVisible"
      :title="t('schedule.dialogTitle')"
      width="520px"
    >
      <el-form :model="customForm" label-width="100px">
        <el-form-item :label="t('schedule.scheduleType')">
          <el-radio-group v-model="customForm.mode" :disabled="disabled">
            <el-radio value="daily">{{ t('schedule.mode.daily') }}</el-radio>
            <el-radio value="weekly">{{ t('schedule.mode.weekly') }}</el-radio>
            <el-radio value="monthly">{{ t('schedule.mode.monthly') }}</el-radio>
            <el-radio v-if="allowCustomCron" value="cron">{{ t('schedule.mode.cron') }}</el-radio>
          </el-radio-group>
        </el-form-item>

        <!-- 每周选择 -->
        <el-form-item v-if="customForm.mode === 'weekly'" :label="t('schedule.execDay')">
          <el-checkbox-group v-model="customForm.weekDays" :disabled="disabled">
            <el-checkbox
              v-for="day in weeklyOptions"
              :key="day.value"
              :label="day.value"
            >
              {{ t(`schedule.weekday.${day.value}`, day.label) }}
            </el-checkbox>
          </el-checkbox-group>
        </el-form-item>

        <!-- 每月选择 -->
        <el-form-item v-if="customForm.mode === 'monthly'" :label="t('schedule.date')">
          <el-input-number
            v-model="customForm.dayOfMonth"
            :min="1"
            :max="31"
            controls-position="right"
            :disabled="disabled"
          />
          <span class="schedule-dialog__tip">{{ t('schedule.monthlySuffix') }}</span>
        </el-form-item>

        <!-- 执行时间 -->
        <el-form-item v-if="customForm.mode !== 'cron'" :label="t('schedule.execTime')">
          <el-time-picker
            v-model="customForm.time"
            :placeholder="t('schedule.selectTime')"
            format="HH:mm"
            value-format="HH:mm"
            :disabled="disabled"
          />
        </el-form-item>

        <!-- 自定义 Cron 表达式 -->
        <el-form-item v-if="customForm.mode === 'cron'" :label="t('schedule.cronExpr')">
          <el-input
            v-model="customForm.cronExpr"
            :placeholder="t('schedule.cronPlaceholder')"
            :disabled="disabled"
          />
          <div class="schedule-dialog__tip">
            {{ t('schedule.cronTip') }}
            <el-link
              href="https://crontab.guru/"
              target="_blank"
              type="primary"
              :underline="false"
            >
              {{ t('schedule.cronGenerator') }}
            </el-link>
          </div>
        </el-form-item>

        <!-- 预览 -->
        <el-form-item :label="t('schedule.preview')">
          <div class="schedule-dialog__preview">
            {{ customPreview }}
          </div>
        </el-form-item>
      </el-form>

      <template #footer>
        <el-button @click="dialogVisible = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :disabled="disabled" @click="handleConfirm">{{ t('common.confirm') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { Clock } from '@element-plus/icons-vue'
import {
  presetOptions,
  presetOptionMapByKey,
  weeklyOptions,
  buildScheduleFromForm,
  generateScheduleDescription,
  decodeScheduleToForm,
  describeCron,
  validateCron
} from '../utils/schedule'

const { t } = useI18n()

const props = defineProps({
  modelValue: {
    type: String,
    default: ''
  },
  allowCustomCron: {
    type: Boolean,
    default: false
  },
  showPresets: {
    type: Boolean,
    default: true
  },
  presetList: {
    type: Array,
    default: null
  },
  compactMode: {
    type: Boolean,
    default: false
  },
  disabled: {
    type: Boolean,
    default: false
  },
  readonly: {
    type: Boolean,
    default: false
  }
})

const emit = defineEmits(['update:modelValue'])

const dialogVisible = ref(false)
const customForm = ref({
  mode: 'daily',
  time: '09:00',
  weekDays: ['1'],
  dayOfMonth: 1,
  cronExpr: ''
})

// 当前配置描述
const description = computed(() => {
  if (!props.modelValue) return ''
  return describeCron(props.modelValue, t)
})

// 自定义配置预览
const customPreview = computed(() => {
  if (customForm.value.mode === 'cron') {
    if (!customForm.value.cronExpr) {
      return t('schedule.error.enterCron')
    }
    return describeCron(customForm.value.cronExpr, t)
  }
  return generateScheduleDescription(customForm.value, t)
})

// 有效的预设列表（支持自定义）
const effectivePresets = computed(() => {
  return props.presetList || presetOptions
})

// 判断某个预设是否被选中
const isPresetSelected = (key) => {
  if (!props.modelValue) return false
  const preset = presetOptionMapByKey[key]
  return preset && preset.cron === props.modelValue
}

// 判断是否为自定义调度（不在预设列表中）
const isCustomSchedule = computed(() => {
  if (!props.modelValue) return false
  // 检查是否匹配任何预设
  const matchesPreset = effectivePresets.value.some(preset => preset.cron === props.modelValue)
  return !matchesPreset
})

// 点击预设选项
const handlePresetClick = (key) => {
  const option = presetOptionMapByKey[key]
  if (option) {
    emit('update:modelValue', option.cron)
  }
}

// 清除调度
const clearSchedule = () => {
  emit('update:modelValue', '')
}

// 打开对话框
const openDialog = () => {
  // 尝试解码现有配置
  const parsed = decodeScheduleToForm(props.modelValue)
  if (parsed) {
    customForm.value = { ...parsed, cronExpr: '' }
  } else if (props.modelValue && props.allowCustomCron) {
    // 如果是自定义 Cron 表达式
    customForm.value = {
      mode: 'cron',
      time: '09:00',
      weekDays: ['1'],
      dayOfMonth: 1,
      cronExpr: props.modelValue
    }
  } else {
    // 默认值
    customForm.value = {
      mode: 'daily',
      time: '09:00',
      weekDays: ['1'],
      dayOfMonth: 1,
      cronExpr: ''
    }
  }
  dialogVisible.value = true
}

// 确认配置
const handleConfirm = () => {
  if (customForm.value.mode === 'cron') {
    // 验证自定义 Cron 表达式
    if (!customForm.value.cronExpr) {
      ElMessage.warning(t('schedule.error.enterCron'))
      return
    }
    if (!validateCron(customForm.value.cronExpr)) {
      ElMessage.warning(t('schedule.error.invalidCron'))
      return
    }
    emit('update:modelValue', customForm.value.cronExpr.trim())
    dialogVisible.value = false
    return
  }

  // 从表单构建调度配置
  const result = buildScheduleFromForm(customForm.value, t)
  if (!result) {
    if (customForm.value.mode === 'weekly') {
      ElMessage.warning(t('schedule.error.selectDay'))
    } else {
      ElMessage.warning(t('schedule.error.selectTime'))
    }
    return
  }

  emit('update:modelValue', result.cron)
  dialogVisible.value = false
}
</script>

<style scoped>
.schedule-config {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

/* 紧凑模式 */
.schedule-config.compact {
  gap: 6px;
}

.schedule-config.compact .schedule-presets {
  gap: 6px;
}

/* 禁用状态 */
.schedule-config.disabled {
  opacity: 0.6;
  pointer-events: none;
}

/* 只读状态 */
.schedule-config.readonly .schedule-result {
  background-color: var(--addp-bg-secondary);
  padding: 8px 12px;
  border-radius: 4px;
}

.schedule-presets {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
}

.schedule-presets__label {
  color: var(--addp-text-primary);
  font-size: 14px;
  font-weight: 500;
}

.schedule-presets__sublabel {
  color: var(--addp-text-secondary);
  font-size: 13px;
  margin-left: 8px;
}

/* 当前配置显示 */
.schedule-current {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 12px;
  background-color: #f0f9ff;
  border: 1px solid #409eff;
  border-radius: 4px;
  margin-top: 4px;
}

.schedule-current__icon {
  color: #409eff;
  font-size: 16px;
  flex-shrink: 0;
}

.schedule-current__label {
  color: var(--addp-text-secondary);
  font-size: 13px;
  flex-shrink: 0;
}

.schedule-current__value {
  color: var(--addp-text-primary);
  font-size: 14px;
  font-weight: 500;
}

.schedule-dialog__tip {
  margin-left: 12px;
  font-size: 12px;
  color: var(--addp-text-tertiary);
}

.schedule-dialog__preview {
  font-size: 14px;
  color: var(--addp-text-primary);
}
</style>
