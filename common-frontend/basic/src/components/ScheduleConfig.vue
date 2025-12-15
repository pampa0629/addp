<template>
  <div class="schedule-config">
    <!-- 快捷选择 -->
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

    <!-- 操作按钮 -->
    <div class="schedule-actions">
      <el-button type="primary" size="small" @click="openDialog">
        自定义时间
      </el-button>
      <el-button
        v-if="modelValue"
        size="small"
        text
        @click="clearSchedule"
      >
        清除调度
      </el-button>
    </div>

    <!-- 当前配置描述 -->
    <div class="schedule-result">
      {{ description || '尚未设置,将按需手动执行' }}
    </div>

    <!-- 提示信息 -->
    <div class="hint">
      设置后系统会按照上方的文字说明自动运行;清除后仅支持手动触发。
    </div>

    <!-- 自定义配置对话框 -->
    <el-dialog
      v-model="dialogVisible"
      title="设置定时调度"
      width="520px"
    >
      <el-form :model="customForm" label-width="100px">
        <el-form-item label="调度类型">
          <el-radio-group v-model="customForm.mode">
            <el-radio label="daily">每天</el-radio>
            <el-radio label="weekly">每周</el-radio>
            <el-radio label="monthly">每月</el-radio>
            <el-radio v-if="allowCustomCron" label="cron">自定义Cron</el-radio>
          </el-radio-group>
        </el-form-item>

        <!-- 每周选择 -->
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

        <!-- 每月选择 -->
        <el-form-item v-if="customForm.mode === 'monthly'" label="日期">
          <el-input-number
            v-model="customForm.dayOfMonth"
            :min="1"
            :max="31"
            controls-position="right"
          />
          <span class="schedule-dialog__tip">如遇当月无该日期,将在最后一天执行</span>
        </el-form-item>

        <!-- 执行时间 -->
        <el-form-item v-if="customForm.mode !== 'cron'" label="执行时间">
          <el-time-picker
            v-model="customForm.time"
            placeholder="选择时间"
            format="HH:mm"
            value-format="HH:mm"
          />
        </el-form-item>

        <!-- 自定义 Cron 表达式 -->
        <el-form-item v-if="customForm.mode === 'cron'" label="Cron 表达式">
          <el-input
            v-model="customForm.cronExpr"
            placeholder="如: */5 * * * * (每5分钟)"
          />
          <div class="schedule-dialog__tip">
            支持标准 5 字段 Cron 表达式
            <el-link
              href="https://crontab.guru/"
              target="_blank"
              type="primary"
              :underline="false"
            >
              在线生成器
            </el-link>
          </div>
        </el-form-item>

        <!-- 预览 -->
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
import { ref, computed } from 'vue'
import { ElMessage } from 'element-plus'
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

const props = defineProps({
  modelValue: {
    type: String,
    default: ''
  },
  allowCustomCron: {
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
  return describeCron(props.modelValue)
})

// 自定义配置预览
const customPreview = computed(() => {
  if (customForm.value.mode === 'cron') {
    if (!customForm.value.cronExpr) {
      return '请输入 Cron 表达式'
    }
    return describeCron(customForm.value.cronExpr)
  }
  return generateScheduleDescription(customForm.value)
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
      ElMessage.warning('请输入 Cron 表达式')
      return
    }
    if (!validateCron(customForm.value.cronExpr)) {
      ElMessage.warning('Cron 表达式格式无效')
      return
    }
    emit('update:modelValue', customForm.value.cronExpr.trim())
    dialogVisible.value = false
    return
  }

  // 从表单构建调度配置
  const result = buildScheduleFromForm(customForm.value)
  if (!result) {
    if (customForm.value.mode === 'weekly') {
      ElMessage.warning('请至少选择一个执行日')
    } else {
      ElMessage.warning('请选择有效的执行时间')
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

.hint {
  font-size: 12px;
  color: #909399;
  line-height: 1.6;
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
</style>
