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
    <div class="mapping-controls">
      <el-button type="primary" @click="autoMap">{{ t('transfer.taskWizard.autoMap') }}</el-button>
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
            placeholder="类型"
            @change="handleMappingChange($index)"
          >
            <el-option :label="t('transfer.taskWizard.typeString')" value="string" />
            <el-option :label="t('transfer.taskWizard.typeInteger')" value="integer" />
            <el-option :label="t('transfer.taskWizard.typeFloat')" value="float" />
            <el-option :label="t('transfer.taskWizard.typeDouble')" value="double" />
            <el-option :label="t('transfer.taskWizard.typeBoolean')" value="boolean" />
            <el-option :label="t('transfer.taskWizard.typeDate')" value="date" />
            <el-option :label="t('transfer.taskWizard.typeTimestamp')" value="timestamp" />
            <el-option :label="t('transfer.taskWizard.typeGeometry')" value="geometry" />
          </el-select>
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

      <el-table-column :label="t('transfer.taskWizard.formatCol')" width="140">
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
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'

const { t } = useI18n()

const props = defineProps({
  wizardState: {
    type: Object,
    required: true
  }
})

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
  max-width: 1200px;
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

.empty-hint {
  margin-top: 40px;
}
</style>
