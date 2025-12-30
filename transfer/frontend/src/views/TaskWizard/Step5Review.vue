<template>
  <div class="step5-review">
    <h3>确认创建</h3>
    <p class="step-description">请确认以下任务配置，确认无误后点击"创建任务"</p>

    <!-- 任务基本信息 -->
    <el-card class="config-card">
      <template #header>
        <div class="card-header">
          <span>任务基本信息</span>
        </div>
      </template>
      <el-descriptions :column="2" border>
        <el-descriptions-item label="任务名称">
          {{ wizardState.taskName.value }}
        </el-descriptions-item>
        <el-descriptions-item label="调度计划">
          {{ wizardState.schedule.value || '立即执行一次' }}
        </el-descriptions-item>
        <el-descriptions-item label="任务描述" :span="2">
          {{ wizardState.taskDescription.value || '无' }}
        </el-descriptions-item>
      </el-descriptions>
    </el-card>

    <!-- 数据源配置 -->
    <el-card class="config-card">
      <template #header>
        <div class="card-header">
          <span>数据源配置</span>
        </div>
      </template>
      <el-descriptions :column="2" border>
        <el-descriptions-item label="引擎ID">
          {{ wizardState.sourceEngineID.value }}
        </el-descriptions-item>
        <el-descriptions-item label="Schema">
          {{ wizardState.sourceSchema.value }}
        </el-descriptions-item>
        <el-descriptions-item label="表名" :span="2">
          {{ wizardState.sourceTable.value }}
        </el-descriptions-item>
      </el-descriptions>
    </el-card>

    <!-- 目标配置 -->
    <el-card class="config-card">
      <template #header>
        <div class="card-header">
          <span>目标配置</span>
        </div>
      </template>
      <el-descriptions :column="2" border>
        <el-descriptions-item label="引擎ID">
          {{ wizardState.targetEngineID.value }}
        </el-descriptions-item>
        <el-descriptions-item label="Schema">
          {{ wizardState.targetSchema.value }}
        </el-descriptions-item>
        <el-descriptions-item label="表名" :span="2">
          {{ wizardState.targetTable.value }}
        </el-descriptions-item>
      </el-descriptions>
    </el-card>

    <!-- 字段映射 -->
    <el-card class="config-card">
      <template #header>
        <div class="card-header">
          <span>字段映射 ({{ wizardState.fieldMappings.value.length }} 个)</span>
        </div>
      </template>
      <el-table
        :data="wizardState.fieldMappings.value"
        border
        size="small"
        max-height="300"
      >
        <el-table-column prop="sourceField" label="源字段" width="200" />
        <el-table-column prop="targetField" label="目标字段" width="200" />
        <el-table-column prop="fieldType" label="类型" width="120" />
        <el-table-column prop="format" label="格式/默认值" />
      </el-table>
    </el-card>

    <!-- 转换配置 -->
    <el-card v-if="wizardState.transforms.value.length > 0" class="config-card">
      <template #header>
        <div class="card-header">
          <span>数据转换 ({{ wizardState.transforms.value.length }} 个)</span>
        </div>
      </template>
      <el-tag
        v-for="(transform, index) in wizardState.transforms.value"
        :key="index"
        style="margin-right: 8px"
      >
        {{ transform.type }}
      </el-tag>
    </el-card>

    <!-- 配置JSON预览 -->
    <el-card class="config-card">
      <template #header>
        <div class="card-header">
          <span>完整配置（JSON）</span>
          <el-button size="small" @click="copyConfig">复制配置</el-button>
        </div>
      </template>
      <pre class="json-preview">{{ JSON.stringify(wizardState.taskConfig.value, null, 2) }}</pre>
    </el-card>

    <!-- 警告提示 -->
    <el-alert
      v-if="hasWarnings"
      title="注意事项"
      type="warning"
      :closable="false"
      style="margin-top: 20px"
    >
      <ul class="warning-list">
        <li v-for="warning in warnings" :key="warning">{{ warning }}</li>
      </ul>
    </el-alert>
  </div>
</template>

<script setup>
import { computed } from 'vue'
import { ElMessage } from 'element-plus'

const props = defineProps({
  wizardState: {
    type: Object,
    required: true
  }
})

const warnings = computed(() => {
  const warns = []

  // 检查是否有字段映射
  if (props.wizardState.fieldMappings.value.length === 0) {
    warns.push('未配置字段映射，将尝试自动映射同名字段')
  }

  // 检查调度计划
  if (!props.wizardState.schedule.value) {
    warns.push('未配置调度计划，任务将立即执行一次')
  }

  // 检查目标表是否存在
  // 这里可以添加更多验证逻辑

  return warns
})

const hasWarnings = computed(() => warnings.value.length > 0)

function copyConfig() {
  const config = JSON.stringify(props.wizardState.taskConfig.value, null, 2)
  navigator.clipboard.writeText(config).then(() => {
    ElMessage.success('配置已复制到剪贴板')
  }).catch(() => {
    ElMessage.error('复制失败，请手动复制')
  })
}
</script>

<style scoped>
.step5-review {
  max-width: 1000px;
  margin: 0 auto;
}

.step-description {
  color: #606266;
  margin-bottom: 30px;
}

.config-card {
  margin-bottom: 20px;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.json-preview {
  background: #f5f7fa;
  padding: 16px;
  border-radius: 4px;
  overflow-x: auto;
  font-size: 12px;
  max-height: 400px;
}

.warning-list {
  margin: 0;
  padding-left: 20px;
}

.warning-list li {
  margin: 4px 0;
}
</style>
