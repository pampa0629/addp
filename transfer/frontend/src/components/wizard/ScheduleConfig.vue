<template>
  <div class="schedule-settings">
    <div class="schedule-presets">
      <span class="schedule-presets__label">快捷选择:</span>
      <el-button
        v-for="preset in presetOptions"
        :key="preset.key"
        size="small"
        @click="handlePresetClick(preset.key)"
      >
        {{ preset.label }}
      </el-button>
    </div>
    <div class="schedule-actions">
      <el-button type="primary" size="small" @click="openDialog">自定义时间</el-button>
      <el-button
        v-if="schedule"
        size="small"
        text
        @click="clearSchedule"
      >
        清除调度
      </el-button>
    </div>
    <div class="schedule-result">
      {{ description || '尚未设置,将按需手动执行' }}
    </div>
    <div class="hint">
      设置后系统会按照上方的文字说明自动运行;清除后仅支持手动触发。
    </div>

    <el-dialog
      v-model="dialogVisible"
      title="设置定时调度"
      width="520px"
    >
      <el-form :model="customForm" label-width="100px">
        <el-form-item label="执行频率">
          <el-radio-group v-model="customForm.mode">
            <el-radio label="daily">每天</el-radio>
            <el-radio label="weekly">每周</el-radio>
            <el-radio label="monthly">每月</el-radio>
          </el-radio-group>
        </el-form-item>

        <el-form-item v-if="customForm.mode === 'weekly'" label="执行日">
          <el-checkbox-group v-model="customForm.weekDays">
            <el-checkbox
              v-for="day in weeklyOptions"
              :key="day.value"
              :label="day.value"
            >
              {{ day.label }}
            </el-checkbox>
          </el-checkbox-group>
        </el-form-item>

        <el-form-item v-if="customForm.mode === 'monthly'" label="日期">
          <el-input-number
            v-model="customForm.dayOfMonth"
            :min="1"
            :max="31"
            controls-position="right"
          />
          <span class="schedule-dialog__tip">如遇当月无该日期,将在最后一天执行</span>
        </el-form-item>

        <el-form-item label="执行时间">
          <el-time-picker
            v-model="customForm.time"
            placeholder="选择时间"
            format="HH:mm"
            value-format="HH:mm"
          />
        </el-form-item>

        <el-form-item label="说明">
          <div class="schedule-dialog__preview">
            {{ customPreview }}
          </div>
        </el-form-item>
      </el-form>

      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" @click="handleConfirm">确定</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, watch } from 'vue'
import { ElMessage } from 'element-plus'
import {
  presetOptions,
  presetOptionMapByKey,
  weeklyOptions,
  buildScheduleFromForm,
  generateScheduleDescription,
  decodeScheduleToForm,
  describeCron
} from '@/utils/schedule'

const props = defineProps({
  schedule: {
    type: String,
    default: ''
  }
})

const emit = defineEmits(['update:schedule'])

const dialogVisible = ref(false)
const customForm = ref({
  mode: 'daily',
  time: '09:00',
  weekDays: ['1'],
  dayOfMonth: 1
})

const description = computed(() => {
  if (!props.schedule) return ''
  return describeCron(props.schedule)
})

const customPreview = computed(() => generateScheduleDescription(customForm.value))

const handlePresetClick = (key) => {
  const option = presetOptionMapByKey[key]
  if (option) {
    emit('update:schedule', option.cron)
  }
}

const clearSchedule = () => {
  emit('update:schedule', '')
}

const openDialog = () => {
  const parsed = decodeScheduleToForm(props.schedule)
  if (parsed) {
    customForm.value = { ...parsed }
  } else {
    customForm.value = {
      mode: 'daily',
      time: '09:00',
      weekDays: ['1'],
      dayOfMonth: 1
    }
  }
  dialogVisible.value = true
}

const handleConfirm = () => {
  const result = buildScheduleFromForm(customForm.value)
  if (!result) {
    if (customForm.value.mode === 'weekly') {
      ElMessage.warning('请至少选择一个执行日')
    } else {
      ElMessage.warning('请选择有效的执行时间')
    }
    return
  }
  emit('update:schedule', result.cron)
  dialogVisible.value = false
}
</script>

<style scoped>
.schedule-settings {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.schedule-presets {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
}

.schedule-presets__label {
  color: #606266;
  font-size: 13px;
}

.schedule-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}

.schedule-result {
  font-size: 13px;
  color: #303133;
}

.schedule-dialog__tip {
  margin-left: 12px;
  font-size: 12px;
  color: #909399;
}

.schedule-dialog__preview {
  font-size: 14px;
  color: #303133;
}

.hint {
  font-size: 12px;
  color: #909399;
  line-height: 1.6;
}
</style>
