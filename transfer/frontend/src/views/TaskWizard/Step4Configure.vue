<template>
  <div class="step4-configure">
    <h3>任务配置</h3>
    <p class="step-description">设置任务名称、描述和调度计划</p>

    <el-form :model="wizardState" label-width="120px" :rules="rules" ref="formRef">
      <!-- 任务名称 -->
      <el-form-item label="任务名称" prop="taskName" required>
        <el-input
          v-model="wizardState.taskName.value"
          placeholder="请输入任务名称"
          maxlength="50"
          show-word-limit
        />
      </el-form-item>

      <!-- 任务描述 -->
      <el-form-item label="任务描述" prop="taskDescription">
        <el-input
          v-model="wizardState.taskDescription.value"
          type="textarea"
          :rows="3"
          placeholder="请输入任务描述（可选）"
          maxlength="200"
          show-word-limit
        />
      </el-form-item>

      <!-- 调度计划 -->
      <el-form-item label="调度计划">
        <el-radio-group v-model="scheduleMode">
          <el-radio label="once">立即执行一次</el-radio>
          <el-radio label="cron">定时调度</el-radio>
        </el-radio-group>
      </el-form-item>

      <el-form-item v-if="scheduleMode === 'cron'" label="Cron表达式">
        <div class="cron-config">
          <el-input
            v-model="wizardState.schedule.value"
            placeholder="例如: 0 0 * * * (每天0点)"
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
              title="Cron格式：秒 分 时 日 月 周"
              type="info"
              :closable="false"
            />
          </div>
        </div>
      </el-form-item>

      <el-form-item v-if="scheduleMode === 'cron'" label="定时任务启用">
        <el-switch v-model="wizardState.enabled.value" />
        <span class="form-hint">启用后定时任务将按计划自动执行</span>
      </el-form-item>

      <!-- 高级选项 -->
      <el-divider content-position="left">高级选项</el-divider>

      <el-form-item label="批处理大小">
        <el-input-number
          v-model="wizardState.batchSize.value"
          :min="100"
          :max="50000"
          :step="100"
        />
        <span class="form-hint">单次读写的记录数（推荐：小数据 1000，中等数据 5000，大数据 10000）</span>
      </el-form-item>
    </el-form>
  </div>
</template>

<script setup>
import { ref } from 'vue'

const props = defineProps({
  wizardState: {
    type: Object,
    required: true
  }
})

const formRef = ref(null)
const scheduleMode = ref('once')

const cronPresets = [
  { label: '每小时', value: '0 0 * * * *' },
  { label: '每天0点', value: '0 0 0 * * *' },
  { label: '每天8点', value: '0 0 8 * * *' },
  { label: '每周一', value: '0 0 0 * * 1' },
  { label: '每月1号', value: '0 0 0 1 * *' }
]

const rules = {
  taskName: [
    { required: true, message: '请输入任务名称', trigger: 'blur' },
    { min: 2, max: 50, message: '长度在 2 到 50 个字符', trigger: 'blur' }
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
