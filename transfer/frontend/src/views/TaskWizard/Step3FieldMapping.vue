<template>
  <div class="step3-field-mapping">
    <h3>{{ t('transfer.taskWizard.fieldMappingPage') }}</h3>
    <p class="step-description">{{ t('transfer.taskWizard.fieldMappingPageDesc') }}</p>

    <el-alert
      v-if="wizardState.isRawCopyTask.value"
      type="info"
      :closable="false"
      :title="t('transfer.taskWizard.rawCopyNoMappingTitle')"
      :description="t('transfer.taskWizard.rawCopyNoMappingDesc')"
    />

    <template v-else>
    <el-alert
      v-if="wizardState.isContinuousTask.value"
      type="info"
      :closable="false"
      :title="t('transfer.taskWizard.continuousMappingTitle')"
      :description="t('transfer.taskWizard.continuousMappingDesc')"
      class="continuous-mapping-alert"
    />
    <div class="mapping-controls">
      <el-button v-if="!wizardState.isContinuousTask.value" type="primary" @click="autoMap">{{ t('transfer.taskWizard.autoMap') }}</el-button>
      <el-button @click="addMapping">{{ t('transfer.taskWizard.addMapping') }}</el-button>
      <el-button @click="clearMappings">{{ t('transfer.taskWizard.clearAll') }}</el-button>
    </div>

    <el-table :data="wizardState.fieldMappings.value" border style="margin-top: 20px">
      <el-table-column :label="t('transfer.taskWizard.sourceFieldCol')" width="200">
        <template #default="{ row, $index }">
          <el-select
            v-model="row.source_field"
            :placeholder="t('transfer.taskWizard.selectSourceField')"
            filterable
            :allow-create="wizardState.isContinuousTask.value"
            :default-first-option="wizardState.isContinuousTask.value"
            @change="handleMappingChange($index)"
          >
            <el-option
              v-for="field in wizardState.sourceFields.value"
              :key="field.name"
              :label="fieldOptionLabel(field)"
              :value="field.name"
            />
          </el-select>
        </template>
      </el-table-column>

      <el-table-column :label="t('transfer.taskWizard.sourceTypeCol')" width="120">
        <template #default="{ row }">
          <el-tag v-if="sourceFieldType(row.source_field)" size="small">
            {{ sourceFieldType(row.source_field) }}
          </el-tag>
          <span v-else>-</span>
        </template>
      </el-table-column>

      <el-table-column :label="t('transfer.taskWizard.targetFieldCol')" width="200">
        <template #default="{ row, $index }">
          <el-select
            v-model="row.target_field"
            :placeholder="t('transfer.taskWizard.selectTargetField')"
            filterable
            allow-create
            @change="handleMappingChange($index)"
          >
            <el-option
              v-for="field in wizardState.targetFields.value"
              :key="field.name"
              :label="fieldOptionLabel(field)"
              :value="field.name"
            />
          </el-select>
        </template>
      </el-table-column>

      <el-table-column :label="t('transfer.taskWizard.dataTypeCol')" width="140">
        <template #default="{ row, $index }">
          <el-select
            v-model="row.target_type"
            :placeholder="t('transfer.taskWizard.selectTargetType')"
            @change="handleMappingChange($index)"
          >
            <el-option
              v-for="option in targetTypeOptions"
              :key="option.value"
              :label="option.label"
              :value="option.value"
            />
          </el-select>
        </template>
      </el-table-column>

      <el-table-column :label="t('transfer.taskWizard.precisionCol')" width="120">
        <template #default="{ row, $index }">
          <el-input-number
            v-if="row.target_type === 'decimal'"
            v-model="row.precision"
            class="decimal-number-input"
            :min="1"
            :max="decimalPrecisionMax"
            controls-position="right"
            @change="handleMappingChange($index)"
          />
          <span v-else>-</span>
        </template>
      </el-table-column>

      <el-table-column :label="t('transfer.taskWizard.scaleCol')" width="120">
        <template #default="{ row, $index }">
          <el-input-number
            v-if="row.target_type === 'decimal'"
            v-model="row.scale"
            class="decimal-number-input"
            :min="0"
            :max="decimalScaleMax(row)"
            controls-position="right"
            @change="handleMappingChange($index)"
          />
          <span v-else>-</span>
        </template>
      </el-table-column>

      <el-table-column :label="t('transfer.taskWizard.defaultValueCol')" width="150">
        <template #default="{ row, $index }">
          <el-input
            v-model="row.default_value"
            :placeholder="t('transfer.taskWizard.defaultValuePlaceholder')"
            @input="handleMappingChange($index)"
          />
        </template>
      </el-table-column>

      <el-table-column v-if="!wizardState.isContinuousTask.value" :label="t('transfer.taskWizard.formatCol')" width="140">
        <template #default="{ row, $index }">
          <el-input
            v-model="row.format"
            :placeholder="t('transfer.taskWizard.formatPlaceholder')"
            @input="handleMappingChange($index)"
          />
        </template>
      </el-table-column>

      <el-table-column :label="t('transfer.taskWizard.nullableCol')" width="90" align="center">
        <template #default="{ row, $index }">
          <el-switch
            v-model="row.nullable"
            @change="handleMappingChange($index)"
          />
        </template>
      </el-table-column>

      <el-table-column :label="t('transfer.taskWizard.actionsCol')" width="100" fixed="right">
        <template #default="{ $index }">
          <el-button
            type="danger"
            size="small"
            @click="removeMapping($index)"
          >
            {{ t('transfer.taskWizard.deleteMappingBtn') }}
          </el-button>
        </template>
      </el-table-column>
    </el-table>

    <div v-if="wizardState.fieldMappings.value.length === 0" class="empty-hint">
      <el-empty :description="t('transfer.taskWizard.emptyMappingHint')" />
    </div>
    </template>
  </div>
</template>

<script setup>
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { CONTINUOUS_FIELD_TYPES, databaseCDCFieldTypes } from './continuousTask.mjs'

const { t } = useI18n()

const props = defineProps({
  wizardState: {
    type: Object,
    required: true
  }
})

const targetTypeOptions = computed(() => {
  const types = props.wizardState.isDatabaseCDCTask.value
    ? databaseCDCFieldTypes(props.wizardState.sourceEngineType.value)
    : props.wizardState.isContinuousTask.value
    ? CONTINUOUS_FIELD_TYPES
    : ['string', 'bool', 'int', 'bigint', 'float', 'double', 'decimal', 'date', 'time', 'timestamp', 'json', 'uuid', 'geometry']
  return types.map(value => ({
    value,
    label: t(`transfer.taskWizard.fieldType.${value}`)
  }))
})

const isMySQLTarget = computed(() => String(props.wizardState.targetEngineType.value || '').toLowerCase().includes('mysql'))
const decimalPrecisionMax = computed(() => isMySQLTarget.value ? 65 : 1000)

function decimalScaleMax(row) {
  const precision = Number(row?.precision)
  const engineMaximum = isMySQLTarget.value ? 30 : 1000
  return Number.isInteger(precision) && precision > 0 ? Math.min(engineMaximum, precision) : engineMaximum
}

function autoMap() {
  props.wizardState.autoGenerateFieldMappings()
  ElMessage.success(t('transfer.taskWizard.autoMapSuccess'))
}

function addMapping() {
  props.wizardState.addFieldMapping()
}

function removeMapping(index) {
  props.wizardState.removeFieldMapping(index)
}

async function clearMappings() {
  try {
    await ElMessageBox.confirm(
      t('transfer.taskWizard.clearMappingsConfirm'),
      t('transfer.taskWizard.clearMappingsConfirmTitle'),
      {
        confirmButtonText: t('transfer.taskWizard.confirmOk'),
        cancelButtonText: t('transfer.taskWizard.cancel'),
        type: 'warning'
      }
    )

    while (props.wizardState.fieldMappings.value.length > 0) {
      props.wizardState.removeFieldMapping(0)
    }
    ElMessage.success(t('transfer.taskWizard.clearMappingsSuccess'))
  } catch (error) {
    // 用户取消
  }
}

function handleMappingChange(index) {
  const mapping = props.wizardState.fieldMappings.value[index]
  props.wizardState.updateFieldMapping(index, mapping)
}

function fieldOptionLabel(field) {
  const name = String(field?.name || '').trim()
  const type = standardFieldType(field)
  return type ? `${name} (${type})` : name
}

function standardFieldType(field) {
  return String(field?.type || '').trim()
}

function sourceFieldType(fieldName) {
  const name = String(fieldName || '').trim()
  if (!name) return ''
  const field = props.wizardState.sourceFields.value.find(item => item?.name === name)
  return standardFieldType(field)
}
</script>

<style scoped>
.step3-field-mapping {
  max-width: 1440px;
  margin: 0 auto;
}

.step-description {
  color: var(--addp-text-secondary);
  margin-bottom: 20px;
}

.mapping-controls {
  display: flex;
  gap: 12px;
}

.continuous-mapping-alert {
  margin-bottom: 16px;
}

.empty-hint {
  margin-top: 40px;
}

.decimal-number-input {
  width: 100%;
}
</style>
