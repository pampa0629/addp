<template>
  <el-dialog v-model="visible" title="定时调度配置" width="700px" @close="handleClose">
    <el-tabs v-model="activeTab" type="border-card">
      <!-- 常用预设 -->
      <el-tab-pane label="常用预设" name="preset">
        <el-space direction="vertical" :size="10" style="width: 100%">
          <el-card v-for="preset in presets" :key="preset.value" shadow="hover" class="preset-card"
            @click="handleSelectPreset(preset)">
            <div class="preset-header">
              <span class="preset-label">{{ preset.label }}</span>
              <el-tag size="small">{{ preset.value }}</el-tag>
            </div>
            <div class="preset-desc">{{ preset.description }}</div>
          </el-card>
        </el-space>
      </el-tab-pane>

      <!-- 自定义 -->
      <el-tab-pane label="自定义" name="custom">
        <el-form label-width="100px" label-position="left">
          <el-form-item label="分钟">
            <el-radio-group v-model="customSchedule.minuteType" @change="updateExpression">
              <el-radio label="every">每分钟</el-radio>
              <el-radio label="interval">
                每
                <el-input-number v-model="customSchedule.minuteInterval" :min="1" :max="59" size="small"
                  @change="updateExpression" />
                分钟
              </el-radio>
              <el-radio label="specific">
                指定
                <el-select v-model="customSchedule.minuteValues" multiple placeholder="选择分钟" size="small"
                  style="width: 200px" @change="updateExpression">
                  <el-option v-for="i in 60" :key="i-1" :label="i-1" :value="i-1" />
                </el-select>
              </el-radio>
            </el-radio-group>
          </el-form-item>

          <el-form-item label="小时">
            <el-radio-group v-model="customSchedule.hourType" @change="updateExpression">
              <el-radio label="every">每小时</el-radio>
              <el-radio label="interval">
                每
                <el-input-number v-model="customSchedule.hourInterval" :min="1" :max="23" size="small"
                  @change="updateExpression" />
                小时
              </el-radio>
              <el-radio label="specific">
                指定
                <el-select v-model="customSchedule.hourValues" multiple placeholder="选择小时" size="small"
                  style="width: 200px" @change="updateExpression">
                  <el-option v-for="i in 24" :key="i-1" :label="i-1" :value="i-1" />
                </el-select>
              </el-radio>
            </el-radio-group>
          </el-form-item>

          <el-form-item label="日期">
            <el-radio-group v-model="customSchedule.dayType" @change="updateExpression">
              <el-radio label="every">每天</el-radio>
              <el-radio label="interval">
                每
                <el-input-number v-model="customSchedule.dayInterval" :min="1" :max="31" size="small"
                  @change="updateExpression" />
                天
              </el-radio>
              <el-radio label="specific">
                指定
                <el-select v-model="customSchedule.dayValues" multiple placeholder="选择日期" size="small"
                  style="width: 200px" @change="updateExpression">
                  <el-option v-for="i in 31" :key="i" :label="i" :value="i" />
                </el-select>
              </el-radio>
            </el-radio-group>
          </el-form-item>

          <el-form-item label="月份">
            <el-radio-group v-model="customSchedule.monthType" @change="updateExpression">
              <el-radio label="every">每月</el-radio>
              <el-radio label="specific">
                指定
                <el-select v-model="customSchedule.monthValues" multiple placeholder="选择月份" size="small"
                  style="width: 200px" @change="updateExpression">
                  <el-option v-for="i in 12" :key="i" :label="i + '月'" :value="i" />
                </el-select>
              </el-radio>
            </el-radio-group>
          </el-form-item>

          <el-form-item label="星期">
            <el-radio-group v-model="customSchedule.weekType" @change="updateExpression">
              <el-radio label="any">不限</el-radio>
              <el-radio label="specific">
                指定
                <el-select v-model="customSchedule.weekValues" multiple placeholder="选择星期" size="small"
                  style="width: 200px" @change="updateExpression">
                  <el-option label="周日" :value="0" />
                  <el-option label="周一" :value="1" />
                  <el-option label="周二" :value="2" />
                  <el-option label="周三" :value="3" />
                  <el-option label="周四" :value="4" />
                  <el-option label="周五" :value="5" />
                  <el-option label="周六" :value="6" />
                </el-select>
              </el-radio>
            </el-radio-group>
          </el-form-item>
        </el-form>
      </el-tab-pane>

      <!-- 高级输入 -->
      <el-tab-pane label="高级输入" name="manual">
        <el-input v-model="manualExpression" placeholder="输入调度表达式，如：0 0 * * *" />
        <div class="hint">
          <p><strong>调度表达式格式:</strong> 分 时 日 月 周</p>
          <p>示例：</p>
          <ul>
            <li><code>0 0 * * *</code> - 每天零点</li>
            <li><code>0 */2 * * *</code> - 每2小时</li>
            <li><code>*/15 * * * *</code> - 每15分钟</li>
            <li><code>0 9 * * 1-5</code> - 工作日上午9点</li>
          </ul>
          <p class="hint-note">💡 调度表达式基于 Cron 标准格式,但您无需了解技术细节</p>
        </div>
      </el-tab-pane>
    </el-tabs>

    <div class="preview-section">
      <el-divider />
      <div class="preview-label">生成的调度表达式：</div>
      <el-input v-model="currentExpression" readonly>
        <template #append>
          <el-button @click="handleCopyExpression">
            <el-icon><DocumentCopy /></el-icon>
          </el-button>
        </template>
      </el-input>
      <div class="preview-description" v-if="expressionDescription">
        {{ expressionDescription }}
      </div>
    </div>

    <template #footer>
      <el-button @click="handleClose">取消</el-button>
      <el-button type="primary" @click="handleConfirm">确定</el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { ref, computed } from 'vue'
import { ElMessage } from 'element-plus'
import { DocumentCopy } from '@element-plus/icons-vue'

const props = defineProps({
  modelValue: {
    type: Boolean,
    default: false
  }
})

const emit = defineEmits(['update:modelValue', 'select'])

const visible = computed({
  get: () => props.modelValue,
  set: (val) => emit('update:modelValue', val)
})

const activeTab = ref('preset')
const manualExpression = ref('')

// 常用预设
const presets = [
  { label: '每分钟', value: '* * * * *', description: '每分钟执行一次' },
  { label: '每5分钟', value: '*/5 * * * *', description: '每5分钟执行一次' },
  { label: '每15分钟', value: '*/15 * * * *', description: '每15分钟执行一次' },
  { label: '每30分钟', value: '*/30 * * * *', description: '每30分钟执行一次' },
  { label: '每小时', value: '0 * * * *', description: '每小时整点执行' },
  { label: '每2小时', value: '0 */2 * * *', description: '每2小时整点执行' },
  { label: '每天零点', value: '0 0 * * *', description: '每天凌晨0点执行' },
  { label: '每天凌晨2点', value: '0 2 * * *', description: '每天凌晨2点执行（推荐）' },
  { label: '每天上午9点', value: '0 9 * * *', description: '每天上午9点执行' },
  { label: '每天中午12点', value: '0 12 * * *', description: '每天中午12点执行' },
  { label: '工作日上午9点', value: '0 9 * * 1-5', description: '周一到周五上午9点执行' },
  { label: '每周一零点', value: '0 0 * * 1', description: '每周一凌晨0点执行' },
  { label: '每月1号零点', value: '0 0 1 * *', description: '每月1号凌晨0点执行' }
]

// 自定义配置
const customSchedule = ref({
  minuteType: 'every',
  minuteInterval: 5,
  minuteValues: [],
  hourType: 'every',
  hourInterval: 1,
  hourValues: [],
  dayType: 'every',
  dayInterval: 1,
  dayValues: [],
  monthType: 'every',
  monthValues: [],
  weekType: 'any',
  weekValues: []
})

// 当前表达式
const currentExpression = computed(() => {
  if (activeTab.value === 'preset') {
    return ''
  } else if (activeTab.value === 'manual') {
    return manualExpression.value
  } else {
    return buildExpression()
  }
})

// 表达式描述
const expressionDescription = computed(() => {
  if (!currentExpression.value) return ''

  const matched = presets.find(p => p.value === currentExpression.value)
  if (matched) {
    return matched.description
  }

  return describeExpression(currentExpression.value)
})

// 构建调度表达式
const buildExpression = () => {
  const parts = []

  // 分钟
  if (customSchedule.value.minuteType === 'every') {
    parts.push('*')
  } else if (customSchedule.value.minuteType === 'interval') {
    parts.push(`*/${customSchedule.value.minuteInterval}`)
  } else {
    parts.push(customSchedule.value.minuteValues.join(',') || '*')
  }

  // 小时
  if (customSchedule.value.hourType === 'every') {
    parts.push('*')
  } else if (customSchedule.value.hourType === 'interval') {
    parts.push(`*/${customSchedule.value.hourInterval}`)
  } else {
    parts.push(customSchedule.value.hourValues.join(',') || '*')
  }

  // 日期
  if (customSchedule.value.dayType === 'every') {
    parts.push('*')
  } else if (customSchedule.value.dayType === 'interval') {
    parts.push(`*/${customSchedule.value.dayInterval}`)
  } else {
    parts.push(customSchedule.value.dayValues.join(',') || '*')
  }

  // 月份
  if (customSchedule.value.monthType === 'every') {
    parts.push('*')
  } else {
    parts.push(customSchedule.value.monthValues.join(',') || '*')
  }

  // 星期
  if (customSchedule.value.weekType === 'any') {
    parts.push('*')
  } else {
    parts.push(customSchedule.value.weekValues.join(',') || '*')
  }

  return parts.join(' ')
}

// 描述调度表达式
const describeExpression = (expr) => {
  try {
    const parts = expr.split(' ')
    if (parts.length !== 5) return ''

    const [minute, hour, day, month, week] = parts
    const desc = []

    if (minute === '*') {
      desc.push('每分钟')
    } else if (minute.startsWith('*/')) {
      desc.push(`每${minute.slice(2)}分钟`)
    }

    if (hour === '*') {
      if (minute !== '*') desc.push('每小时')
    } else if (hour.startsWith('*/')) {
      desc.push(`每${hour.slice(2)}小时`)
    } else {
      desc.push(`${hour}点`)
    }

    if (day === '*' && week === '*') {
      desc.push('每天')
    } else if (week !== '*') {
      const weekMap = ['周日', '周一', '周二', '周三', '周四', '周五', '周六']
      if (week.includes('-')) {
        const [start, end] = week.split('-')
        desc.push(`${weekMap[start]}-${weekMap[end]}`)
      } else {
        desc.push(weekMap[week])
      }
    }

    return desc.join(' ') + ' 执行'
  } catch {
    return ''
  }
}

// 更新表达式
const updateExpression = () => {
  // 触发计算属性更新
}

// 选择预设
const handleSelectPreset = (preset) => {
  emit('select', preset.value)
  visible.value = false
  ElMessage.success(`已选择：${preset.label}`)
}

// 复制表达式
const handleCopyExpression = () => {
  navigator.clipboard.writeText(currentExpression.value)
  ElMessage.success('已复制到剪贴板')
}

// 确认
const handleConfirm = () => {
  if (!currentExpression.value) {
    ElMessage.warning('请配置定时调度')
    return
  }
  emit('select', currentExpression.value)
  visible.value = false
}

// 关闭
const handleClose = () => {
  visible.value = false
}
</script>

<style scoped>
.preset-card {
  cursor: pointer;
  transition: all 0.3s;
}

.preset-card:hover {
  transform: translateY(-2px);
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.15);
}

.preset-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 8px;
}

.preset-label {
  font-weight: 500;
  font-size: 15px;
}

.preset-desc {
  color: #909399;
  font-size: 13px;
}

.hint {
  margin-top: 10px;
  padding: 10px;
  background: #f5f7fa;
  border-radius: 4px;
  font-size: 13px;
  color: #606266;
}

.hint p {
  margin: 5px 0;
}

.hint ul {
  margin: 5px 0;
  padding-left: 20px;
}

.hint code {
  background: #e4e7ed;
  padding: 2px 6px;
  border-radius: 3px;
  font-family: 'Courier New', monospace;
}

.hint-note {
  margin-top: 8px;
  color: #67c23a;
  font-size: 12px;
}

.preview-section {
  margin-top: 20px;
}

.preview-label {
  font-weight: 500;
  margin-bottom: 10px;
}

.preview-description {
  margin-top: 10px;
  color: #67c23a;
  font-size: 14px;
}

:deep(.el-radio) {
  display: block;
  margin: 10px 0;
}

:deep(.el-tabs__content) {
  min-height: 300px;
}
</style>
