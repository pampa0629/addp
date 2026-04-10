<template>
  <div class="source-config-step">
    <div class="step-section">
      <h3 class="step-section__title">{{ t('transfer.taskWizard.selectSourceDataSource') }}</h3>
      <el-form label-width="120px">
        <el-form-item :label="t('transfer.taskWizard.sourceDataType')">
          <el-radio-group v-model="connectorType" @change="handleTypeChange">
            <el-radio-button value="postgresql">PostgreSQL</el-radio-button>
            <el-radio-button value="mysql">MySQL</el-radio-button>
            <el-radio-button value="spatialite">SpatiaLite/SQLite</el-radio-button>
            <el-radio-button value="s3">S3/MinIO</el-radio-button>
          </el-radio-group>
        </el-form-item>

        <el-form-item :label="t('transfer.taskWizard.selectSourceEngine')">
          <el-select
            v-model="selectedValue"
            :placeholder="t('transfer.taskWizard.selectSourceEngine')"
            style="width: 100%"
            filterable
            :loading="loading"
          >
            <el-option
              v-for="option in sourceOptions"
              :key="option.value"
              :label="`${option.name} (${option.resource_type})`"
              :value="option.value"
            />
          </el-select>
          <div class="hint">
            <p>
              {{ t('transfer.taskWizard.systemEngineGlobal') }}
              <el-link type="primary" @click="$emit('open-system-engines')">{{ t('transfer.taskWizard.browse') }}</el-link>
            </p>
            <p>
              {{ t('transfer.taskWizard.localEngineTransfer') }}
              <el-link type="primary" @click="$emit('open-local-resource')">{{ t('transfer.taskWizard.browse') }}</el-link>
            </p>
          </div>
        </el-form-item>

        <el-alert
          v-if="selectedResource"
          type="info"
          :closable="false"
          style="margin-top: 20px"
        >
          <template #title>
            {{ t('transfer.taskWizard.selectSourceDataSource') }}:{{ selectedResource.name }}
          </template>
          <div style="margin-top: 10px; font-size: 13px;">
            <p v-if="selectedResource.connection_info?.host">
              {{ t('transfer.taskForm.host') }}:{{ selectedResource.connection_info.host }}:{{ selectedResource.connection_info.port }}
            </p>
            <p v-if="selectedResource.connection_info?.database">
              {{ t('transfer.taskForm.database') }}:{{ selectedResource.connection_info.database }}
            </p>
            <p v-if="selectedResource.connection_info?.bucket">
              {{ t('transfer.taskDetail.bucket') }}:{{ selectedResource.connection_info.bucket }}
            </p>
          </div>
        </el-alert>
      </el-form>
    </div>

    <div class="step-section">
      <h3 class="step-section__title">{{ t('transfer.taskWizard.configReadParams') }}</h3>
      <el-alert type="info" :closable="false" style="margin-bottom: 20px">
        {{ t('transfer.taskWizard.readParamsHint') }}
      </el-alert>

      <slot name="config-form"></slot>
    </div>
  </div>
</template>

<script setup>
import { ref, watch, computed } from 'vue'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

const props = defineProps({
  connectorType: {
    type: String,
    required: true
  },
  selectedValue: {
    type: String,
    default: null
  },
  sourceOptions: {
    type: Array,
    default: () => []
  },
  loading: {
    type: Boolean,
    default: false
  }
})

const emit = defineEmits([
  'update:connectorType',
  'update:selectedValue',
  'type-change',
  'open-system-engines',
  'open-local-resource'
])

const connectorType = ref(props.connectorType)
const selectedValue = ref(props.selectedValue)

watch(() => props.connectorType, (val) => connectorType.value = val)
watch(() => props.selectedValue, (val) => selectedValue.value = val)

watch(connectorType, (val) => emit('update:connectorType', val))
watch(selectedValue, (val) => emit('update:selectedValue', val))

const handleTypeChange = () => {
  emit('type-change')
}

const selectedResource = computed(() => {
  const option = props.sourceOptions.find(opt => opt.value === selectedValue.value)
  return option?.resource || null
})
</script>

<style scoped>
.step-section {
  margin-bottom: 32px;
}

.step-section__title {
  font-size: 16px;
  font-weight: 500;
  margin-bottom: 16px;
  color: var(--addp-text-primary);
}

.hint {
  font-size: 12px;
  color: var(--addp-text-tertiary);
  margin-top: 5px;
  line-height: 1.6;
}

.hint p {
  margin: 2px 0;
}
</style>
