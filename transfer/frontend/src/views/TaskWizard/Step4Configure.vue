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

      <!-- 高级选项 -->
      <el-divider content-position="left">高级选项</el-divider>

      <el-form-item label="批量大小">
        <el-input-number
          v-model="batchSize"
          :min="100"
          :max="10000"
          :step="100"
        />
        <span class="form-hint">每批处理的记录数</span>
      </el-form-item>

      <el-form-item label="并发度">
        <el-input-number
          v-model="concurrency"
          :min="1"
          :max="10"
        />
        <span class="form-hint">并行执行的线程数</span>
      </el-form-item>

      <el-form-item label="失败重试">
        <el-switch v-model="enableRetry" />
        <span class="form-hint">任务失败时自动重试</span>
      </el-form-item>

      <el-form-item v-if="enableRetry" label="重试次数">
        <el-input-number
          v-model="retryCount"
          :min="1"
          :max="5"
        />
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
const batchSize = ref(1000)
const concurrency = ref(1)
const enableRetry = ref(false)
const retryCount = ref(3)

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
  color: #606266;
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
  color: #909399;
}
</style>
