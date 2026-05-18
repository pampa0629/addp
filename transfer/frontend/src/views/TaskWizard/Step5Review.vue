<template>
  <div class="step5-review">
    <div class="review-header">
      <div>
        <h3>{{ t('transfer.taskWizard.reviewPage') }}</h3>
        <p class="step-description">{{ t('transfer.taskWizard.reviewPageDesc') }}</p>
      </div>
      <div class="action-buttons">
        <el-button @click="$emit('prev')">
          {{ t('transfer.taskWizard.previousStep') }}
        </el-button>
        <el-button
          type="success"
          @click="$emit('submit')"
          :loading="submitting"
        >
          {{ isEditMode ? t('transfer.taskWizard.updateTask') : t('transfer.taskWizard.createTask2') }}
        </el-button>
        <el-button @click="$emit('cancel')">
          {{ t('transfer.taskWizard.cancel') }}
        </el-button>
      </div>
    </div>

    <!-- 任务基本信息 -->
    <el-card class="config-card">
      <template #header>
        <div class="card-header">
          <span>{{ t('transfer.taskWizard.reviewTaskBasicInfo') }}</span>
        </div>
      </template>
      <el-descriptions :column="2" border>
        <el-descriptions-item :label="t('transfer.taskWizard.reviewTaskName')">
          {{ wizardState.taskName.value }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('transfer.taskWizard.reviewSchedule')">
          {{ wizardState.schedule.value || t('transfer.taskWizard.reviewScheduleOnce') }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('transfer.taskWizard.reviewTaskDesc')" :span="2">
          {{ wizardState.taskDescription.value || t('transfer.taskWizard.reviewNone') }}
        </el-descriptions-item>
      </el-descriptions>
    </el-card>

    <!-- 数据源配置 -->
    <el-card class="config-card">
      <template #header>
        <div class="card-header">
          <span>{{ t('transfer.taskWizard.reviewSourceConfig') }}</span>
        </div>
      </template>
      <el-descriptions :column="2" border>
        <el-descriptions-item :label="t('transfer.taskWizard.reviewEngineId')">
          {{ wizardState.sourceEngineID.value }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('transfer.taskWizard.dataType')">
          {{ dataTypeLabel(wizardState.sourceDataType.value) }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('transfer.taskWizard.representation')">
          {{ representationLabel(wizardState.sourceRepresentation.value) }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('transfer.taskWizard.format')">
          {{ formatLabel(wizardState.sourceFormat.value) }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('transfer.taskWizard.reviewResourcePath')" :span="2">
          {{ sourceResourcePath }}
        </el-descriptions-item>
      </el-descriptions>
    </el-card>

    <!-- 目标配置 -->
    <el-card class="config-card">
      <template #header>
        <div class="card-header">
          <span>{{ t('transfer.taskWizard.reviewTargetConfig') }}</span>
        </div>
      </template>
      <el-descriptions :column="2" border>
        <el-descriptions-item :label="t('transfer.taskWizard.reviewEngineId')">
          {{ wizardState.targetEngineID.value }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('transfer.taskWizard.representation')">
          {{ representationLabel(wizardState.targetRepresentation.value) }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('transfer.taskWizard.format')">
          {{ formatLabel(wizardState.targetConfig.value.format) }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('transfer.taskWizard.writeModeLabel')">
          {{ writeModeLabel(wizardState.targetConfig.value.writeMode) }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('transfer.taskWizard.reviewResourcePath')" :span="2">
          {{ targetResourcePath }}
        </el-descriptions-item>
      </el-descriptions>
    </el-card>

    <!-- 字段映射 -->
    <el-card class="config-card">
      <template #header>
        <div class="card-header">
          <span>{{ t('transfer.taskWizard.reviewFieldMapping', { count: wizardState.fieldMappings.value.length }) }}</span>
        </div>
      </template>
      <el-table
        :data="wizardState.fieldMappings.value"
        border
        size="small"
        max-height="300"
      >
        <el-table-column prop="source_field" :label="t('transfer.taskWizard.reviewSourceFieldCol')" width="200" />
        <el-table-column prop="target_field" :label="t('transfer.taskWizard.reviewTargetFieldCol')" width="200" />
        <el-table-column prop="target_type" :label="t('transfer.taskWizard.reviewTypeCol')" width="120" />
        <el-table-column :label="t('transfer.taskWizard.reviewFormatDefaultCol')">
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
          <span>{{ t('transfer.taskWizard.reviewDataTransforms', { count: wizardState.transforms.value.length }) }}</span>
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
          <span>{{ t('transfer.taskWizard.reviewFullConfig') }}</span>
          <el-button size="small" @click="copyConfig">{{ t('transfer.taskWizard.reviewCopyConfig') }}</el-button>
        </div>
      </template>
      <pre class="json-preview">{{ JSON.stringify(wizardState.taskConfig.value, null, 2) }}</pre>
    </el-card>

    <!-- 警告提示 -->
    <el-alert
      v-if="hasWarnings"
      :title="t('transfer.taskWizard.reviewWarnings')"
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
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { dataTypeLabel, formatLabel, representationLabel, writeModeLabel } from '@/utils/transferDisplay'

const { t } = useI18n()

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

  if (props.wizardState.fieldMappings.value.length === 0) {
    warns.push(t('transfer.taskWizard.warningNoMapping'))
  }

  if (!props.wizardState.schedule.value) {
    warns.push(t('transfer.taskWizard.warningNoSchedule'))
  }

  return warns
})

const hasWarnings = computed(() => warnings.value.length > 0)

const sourceResourcePath = computed(() => {
  const config = props.wizardState.sourceConfig.value || {}
  return config.sourceLabel || resourcePath(props.wizardState.sourceResource.value) || '-'
})

const targetResourcePath = computed(() => {
  const config = props.wizardState.targetConfig.value || {}
  if (props.wizardState.targetRepresentation.value === 'native') {
    return [props.wizardState.targetSchema.value, props.wizardState.targetTable.value].filter(Boolean).join('.') || '-'
  }
  return [config.resourcePath, config.resourceFile].filter(Boolean).join('/') || '-'
})

function resourcePath(resource) {
  const path = resource?.path || {}
  if (resource?.kind === 'native_table') {
    return [path.schema, path.table || path.name].filter(Boolean).join('.')
  }
  if (resource?.kind === 'object') {
    return [path.bucket, path.path].filter(Boolean).join('/')
  }
  if (resource?.kind === 'file') {
    return path.path || path.name || ''
  }
  return ''
}

function copyConfig() {
  const config = JSON.stringify(props.wizardState.taskConfig.value, null, 2)

  if (navigator.clipboard && navigator.clipboard.writeText) {
    navigator.clipboard.writeText(config).then(() => {
      ElMessage.success(t('transfer.taskWizard.reviewConfigCopied'))
    }).catch(() => {
      fallbackCopyToClipboard(config)
    })
  } else {
    fallbackCopyToClipboard(config)
  }
}

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
      ElMessage.success(t('transfer.taskWizard.reviewConfigCopied'))
    } else {
      ElMessage.error(t('transfer.taskWizard.reviewCopyFailed'))
    }
  } catch (err) {
    console.error('复制失败:', err)
    ElMessage.error(t('transfer.taskWizard.reviewCopyFailed'))
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
  border-bottom: 1px solid var(--addp-border-color);
}

.review-header h3 {
  margin: 0 0 8px 0;
}

.step-description {
  color: var(--addp-text-secondary);
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
  background: var(--addp-bg-secondary);
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
