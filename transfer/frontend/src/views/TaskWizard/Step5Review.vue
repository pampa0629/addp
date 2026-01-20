<template>
  <div class="step5-review">
    <div class="review-header">
      <div>
        <h3>确认创建</h3>
        <p class="step-description">请确认以下任务配置，确认无误后点击"创建任务"</p>
      </div>
      <div class="action-buttons">
        <el-button @click="$emit('prev')">
          上一步
        </el-button>
        <el-button
          type="success"
          @click="$emit('submit')"
          :loading="submitting"
        >
          {{ isEditMode ? '更新任务' : '创建任务' }}
        </el-button>
        <el-button @click="$emit('cancel')">
          取消
        </el-button>
      </div>
    </div>

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
        <el-table-column prop="source_field" label="源字段" width="200" />
        <el-table-column prop="target_field" label="目标字段" width="200" />
        <el-table-column prop="field_type" label="类型" width="120" />
        <el-table-column label="格式/默认值">
          <template #default="{ row }">
            {{ row.format || row.default_value || '-' }}
          </template>
        </el-table-column>
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
  },
  isEditMode: {
    type: Boolean,
    default: false
  },
  submitting: {
    type: Boolean,
    default: false
  }
})

defineEmits(['prev', 'submit', 'cancel'])

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

  // 尝试使用现代 Clipboard API
  if (navigator.clipboard && navigator.clipboard.writeText) {
    navigator.clipboard.writeText(config).then(() => {
      ElMessage.success('配置已复制到剪贴板')
    }).catch(() => {
      // Clipboard API 失败，使用 fallback
      fallbackCopyToClipboard(config)
    })
  } else {
    // 浏览器不支持 Clipboard API，使用 fallback
    fallbackCopyToClipboard(config)
  }
}

// 兼容旧浏览器的复制方法
function fallbackCopyToClipboard(text) {
  const textArea = document.createElement('textarea')
  textArea.value = text
  textArea.style.position = 'fixed'
  textArea.style.top = '0'
  textArea.style.left = '0'
  textArea.style.width = '2em'
  textArea.style.height = '2em'
  textArea.style.padding = '0'
  textArea.style.border = 'none'
  textArea.style.outline = 'none'
  textArea.style.boxShadow = 'none'
  textArea.style.background = 'transparent'
  document.body.appendChild(textArea)
  textArea.focus()
  textArea.select()

  try {
    const successful = document.execCommand('copy')
    if (successful) {
      ElMessage.success('配置已复制到剪贴板')
    } else {
      ElMessage.error('复制失败，请手动复制')
    }
  } catch (err) {
    console.error('复制失败:', err)
    ElMessage.error('复制失败，请手动复制')
  }

  document.body.removeChild(textArea)
}
</script>

<style scoped>
.step5-review {
  max-width: 1000px;
  margin: 0 auto;
}

.review-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  margin-bottom: 20px;
  padding-bottom: 20px;
  border-bottom: 1px solid #e4e7ed;
}

.review-header h3 {
  margin: 0 0 8px 0;
}

.step-description {
  color: #606266;
  margin: 0;
}

.action-buttons {
  display: flex;
  gap: 12px;
  flex-shrink: 0;
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
