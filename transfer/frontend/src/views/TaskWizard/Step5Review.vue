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

    <div class="review-grid review-grid--summary">
      <!-- 任务基本信息 -->
      <el-card class="config-card summary-card">
      <template #header>
        <div class="card-header">
          <span>{{ t('transfer.taskWizard.reviewTaskBasicInfo') }}</span>
        </div>
      </template>
      <el-descriptions class="summary-description" :column="1" border>
        <el-descriptions-item :label="t('transfer.taskWizard.reviewTaskName')">
          {{ wizardState.taskName.value }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('transfer.taskWizard.reviewSchedule')">
          {{ wizardState.schedule.value || t('transfer.taskWizard.reviewScheduleOnce') }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('transfer.taskWizard.reviewTaskDesc')">
          {{ wizardState.taskDescription.value || t('transfer.taskWizard.reviewNone') }}
        </el-descriptions-item>
      </el-descriptions>
      </el-card>

      <el-card class="config-card summary-card">
      <template #header>
        <div class="card-header">
          <span>{{ t('transfer.taskWizard.reviewLoadConfig') }}</span>
        </div>
      </template>
      <el-descriptions class="summary-description" :column="1" border>
        <el-descriptions-item :label="t('transfer.taskWizard.runtimeBoundaryLabel')">
          {{ runtimeBoundaryLabel }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('transfer.taskWizard.loadModeLabel')">
          {{ loadModeLabel }}
        </el-descriptions-item>
        <template v-if="wizardState.isContinuousTask.value">
          <el-descriptions-item :label="t('transfer.taskWizard.continuousKeyFieldsLabel')">
            {{ wizardState.continuousKeyFields.value.join(', ') }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('transfer.taskWizard.continuousTargetKeysLabel')">
            {{ wizardState.continuousTargetKeys.value.join(', ') }}
          </el-descriptions-item>
					<el-descriptions-item v-if="wizardState.isKafkaContinuousTask.value" :label="t('transfer.taskWizard.continuousInitialPositionLabel')">
            {{ continuousInitialPositionLabel }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('transfer.taskWizard.continuousPollBatchSizeLabel')">
            {{ wizardState.continuousPollBatchSize.value }}
          </el-descriptions-item>
        </template>
        <template v-if="wizardState.isWatermarkIncremental.value">
          <el-descriptions-item :label="t('transfer.taskWizard.watermarkFieldLabel')">
            {{ wizardState.watermarkField.value }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('transfer.taskWizard.tieBreakerLabel')">
            {{ wizardState.watermarkTieBreakers.value.join(', ') }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('transfer.taskWizard.targetKeysLabel')">
            {{ wizardState.targetKeys.value.join(', ') }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('transfer.taskWizard.incrementalRecoveryLabel')">
            {{ t('transfer.taskWizard.incrementalRecoveryResume') }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('transfer.taskWizard.incrementalDeleteLabel')">
            {{ t('transfer.taskWizard.incrementalDeleteUnsupported') }}
          </el-descriptions-item>
        </template>
      </el-descriptions>
      </el-card>
    </div>

    <div class="review-grid review-grid--endpoints">
      <!-- 数据源配置 -->
      <el-card v-if="!wizardState.isRawCopyTask.value" class="config-card">
      <template #header>
        <div class="card-header">
          <span>{{ t('transfer.taskWizard.reviewSourceConfig') }}</span>
        </div>
      </template>
      <el-descriptions class="endpoint-description" :column="1" border>
        <el-descriptions-item :label="t('transfer.taskWizard.reviewEngine')">
          {{ sourceEngineName }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('transfer.taskWizard.dataType')">
							{{ wizardState.isKafkaContinuousTask.value ? t('transfer.taskWizard.kafkaTopicLabel') : dataTypeLabel(wizardState.sourceDataType.value) }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('transfer.taskWizard.representation')">
          {{ representationLabel(wizardState.sourceRepresentation.value) }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('transfer.taskWizard.format')">
          {{ formatLabel(wizardState.sourceFormat.value) }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('transfer.taskWizard.reviewResourcePath')">
          {{ sourceLocatorPath }}
        </el-descriptions-item>
        <template v-if="wizardState.sourceQueryEnabled.value">
          <el-descriptions-item :label="t('transfer.taskWizard.queryLanguageLabel')">
            {{ wizardState.sourceQueryLanguage.value.toUpperCase() }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('transfer.taskWizard.querySourceLabel')">
            <el-button link type="primary" @click="showSourceQuery = !showSourceQuery">
              {{ showSourceQuery ? t('transfer.taskWizard.reviewHideDetails') : t('transfer.taskWizard.reviewShowDetails') }}
            </el-button>
            <pre v-if="showSourceQuery" class="query-preview">{{ wizardState.sourceQueryStatement.value }}</pre>
          </el-descriptions-item>
        </template>
      </el-descriptions>
      </el-card>

      <!-- 目标配置 -->
      <el-card class="config-card">
      <template #header>
        <div class="card-header">
          <span>{{ t('transfer.taskWizard.reviewTargetConfig') }}</span>
        </div>
      </template>
      <el-descriptions class="endpoint-description" :column="1" border>
        <el-descriptions-item v-if="!isRuntimeTarget" :label="t('transfer.taskWizard.reviewEngine')">
          {{ targetEngineName }}
        </el-descriptions-item>
        <el-descriptions-item v-else :label="t('transfer.taskWizard.targetBindingLabel')">
          {{ t('transfer.taskWizard.runtimeTargetReview') }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('transfer.taskWizard.representation')">
          {{ representationLabel(wizardState.targetRepresentation.value) }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('transfer.taskWizard.format')">
          {{ formatLabel(wizardState.targetConfig.value.format) }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('transfer.taskWizard.writeModeLabel')">
          {{ targetApplyModeLabel }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('transfer.taskWizard.reviewResourcePath')">
          {{ targetResourcePath }}
        </el-descriptions-item>
      </el-descriptions>
      </el-card>
    </div>

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
        <el-table-column prop="source_field" :label="t('transfer.taskWizard.reviewSourceFieldCol')" min-width="220" />
        <el-table-column prop="target_field" :label="t('transfer.taskWizard.reviewTargetFieldCol')" min-width="220" />
        <el-table-column :label="t('transfer.taskWizard.reviewTypeCol')" min-width="140">
          <template #default="{ row }">
            {{ mappedTypeLabel(row) }}
          </template>
        </el-table-column>
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
        class="transform-tag"
      >
        {{ transform.type }}
      </el-tag>
    </el-card>

    <!-- 配置JSON预览 -->
    <el-card class="config-card">
      <template #header>
        <div class="card-header">
          <span>{{ t('transfer.taskWizard.reviewFullConfig') }}</span>
          <div class="card-actions">
            <el-button link type="primary" @click="showFullConfig = !showFullConfig">
              {{ showFullConfig ? t('transfer.taskWizard.reviewHideDetails') : t('transfer.taskWizard.reviewShowDetails') }}
            </el-button>
            <el-button size="small" @click="copyConfig">{{ t('transfer.taskWizard.reviewCopyConfig') }}</el-button>
          </div>
        </div>
      </template>
      <pre v-if="showFullConfig" class="json-preview">{{ JSON.stringify(wizardState.taskConfig.value, null, 2) }}</pre>
    </el-card>

    <!-- 警告提示 -->
    <el-alert
      v-if="hasWarnings"
      :title="t('transfer.taskWizard.reviewWarnings')"
      type="warning"
      :closable="false"
      class="review-warning"
    >
      <ul class="warning-list">
        <li v-for="warning in warnings" :key="warning">{{ warning }}</li>
      </ul>
    </el-alert>
  </div>
</template>

<script setup>
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { formatLocatorDisplayPath } from '@addp/common-frontend'
import { systemEnginesAPI } from '@/api/systemEngines'
import { engineNameForID } from '@/utils/engineDisplay.mjs'
import { dataTypeLabel, formatLabel, representationLabel, writeModeLabel } from '@/utils/transferDisplay'

const { t } = useI18n()
const engines = ref([])
const showSourceQuery = ref(false)
const showFullConfig = ref(false)

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
    if (props.wizardState.isRawCopyTask.value) {
      return warns
    }
    warns.push(t('transfer.taskWizard.warningNoMapping'))
  }

  if (!props.wizardState.schedule.value &&
      !props.wizardState.isContinuousTask.value &&
      props.wizardState.targetBinding.value !== 'runtime') {
    warns.push(t('transfer.taskWizard.warningNoSchedule'))
  }

  return warns
})

const hasWarnings = computed(() => warnings.value.length > 0)
const isRuntimeTarget = computed(() => props.wizardState.targetBinding.value === 'runtime')
const sourceEngineName = computed(() => engineNameForID(engines.value, props.wizardState.sourceEngineID.value))
const targetEngineName = computed(() => engineNameForID(engines.value, props.wizardState.targetEngineID.value))

onMounted(async () => {
  try {
    const response = await systemEnginesAPI.list()
    engines.value = response?.data || response || []
  } catch (error) {
    console.error('加载引擎名称失败:', error)
  }
})

const loadModeLabel = computed(() => {
	if (props.wizardState.isDatabaseCDCTask.value) {
		return t('transfer.taskWizard.databaseCDCLoad')
	}
  if (props.wizardState.isContinuousTask.value) {
    return t('transfer.taskWizard.continuousIncrementalLoad')
  }
  return props.wizardState.isWatermarkIncremental.value
    ? t('transfer.taskWizard.watermarkIncrementalLoad')
    : t('transfer.taskWizard.snapshotLoad')
})

const runtimeBoundaryLabel = computed(() => {
  return props.wizardState.isContinuousTask.value
    ? t('transfer.taskWizard.runtimeContinuous')
    : t('transfer.taskWizard.runtimeBounded')
})

const continuousInitialPositionLabel = computed(() => {
  return props.wizardState.continuousInitialPosition.value === 'latest'
    ? t('transfer.taskWizard.continuousInitialLatest')
    : t('transfer.taskWizard.continuousInitialEarliest')
})

const targetApplyModeLabel = computed(() => {
	if (isRuntimeTarget.value) {
		return writeModeLabel('append')
	}
	if (props.wizardState.isDatabaseCDCTask.value) {
		return t('transfer.taskWizard.applyModeUpsertDelete')
	}
  if (props.wizardState.isContinuousTask.value || props.wizardState.isWatermarkIncremental.value) {
    return t('transfer.taskWizard.applyModeUpsert')
  }
  return writeModeLabel(props.wizardState.targetConfig.value.writeMode)
})

const sourceLocatorPath = computed(() => {
  const config = props.wizardState.sourceConfig.value || {}
  return config.sourceLabel || formatLocatorDisplayPath(props.wizardState.sourceLocator.value, props.wizardState.sourceRepresentation.value) || '-'
})

const targetResourcePath = computed(() => {
  if (isRuntimeTarget.value) {
    return t('transfer.taskWizard.runtimeTargetPath')
  }
  const config = props.wizardState.targetConfig.value || {}
  if (props.wizardState.targetRepresentation.value === 'native') {
    return [props.wizardState.targetSchema.value, props.wizardState.targetTable.value].filter(Boolean).join('.') || '-'
  }
  return [config.resourcePath, config.resourceFile].filter(Boolean).join('/') || '-'
})

function mappedTypeLabel(row) {
  if (row?.target_type !== 'decimal' || !Number.isInteger(row?.precision) || !Number.isInteger(row?.scale)) {
    return row?.target_type || '-'
  }
  return `decimal(${row.precision},${row.scale})`
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
  width: min(100%, 1280px);
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
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 12px;
  flex-shrink: 0;
}

.review-grid {
  display: grid;
  gap: 20px;
  margin-bottom: 20px;
}

.review-grid--summary,
.review-grid--endpoints {
  grid-template-columns: repeat(2, minmax(0, 1fr));
}

.review-grid .config-card {
  min-width: 0;
  margin-bottom: 0;
}

.config-card {
  margin-bottom: 20px;
}

.summary-description :deep(.el-descriptions__label),
.endpoint-description :deep(.el-descriptions__label) {
  width: 132px;
  white-space: nowrap;
}

.summary-description :deep(.el-descriptions__content),
.endpoint-description :deep(.el-descriptions__content) {
  min-width: 0;
  word-break: break-word;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 16px;
}

.card-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}

.json-preview {
  background: var(--addp-bg-secondary);
  padding: 16px;
  border-radius: 4px;
  overflow-x: auto;
  font-size: 12px;
  max-height: 400px;
}

.query-preview {
  margin: 8px 0 0;
  max-height: 240px;
  overflow: auto;
  white-space: pre-wrap;
  word-break: break-word;
}

.warning-list {
  margin: 0;
  padding-left: 20px;
}

.warning-list li {
  margin: 4px 0;
}

.transform-tag {
  margin-right: 8px;
  margin-bottom: 8px;
}

.review-warning {
  margin-top: 20px;
}

@media (max-width: 960px) {
  .review-header {
    flex-direction: column;
    gap: 16px;
  }

  .action-buttons {
    justify-content: flex-start;
  }

  .review-grid--summary,
  .review-grid--endpoints {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
