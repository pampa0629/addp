<template>
  <el-card shadow="never" v-loading="loading">
    <template #header>{{ t('model.structural_constraints.title') }}</template>
    <el-alert v-if="error" :title="error" type="error" :closable="false" />
    <template v-else-if="constraints">
      <p class="constraint-hint">{{ t('model.structural_constraints.hint') }}</p>
      <el-descriptions :column="1" border>
        <el-descriptions-item :label="t('model.structural_constraints.primary_key')">
          {{ constraints.primary_key.map(field => field.column_name).join(' + ') || t('model.structural_constraints.none') }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('model.structural_constraints.required_fields')">
          {{ constraints.required_fields.map(field => field.column_name).join(', ') || t('model.structural_constraints.none') }}
        </el-descriptions-item>
      </el-descriptions>
      <el-table :data="constraints.foreign_keys" class="constraint-relations" :empty-text="t('model.structural_constraints.no_foreign_keys')">
        <el-table-column :label="t('model.structural_constraints.source_field')" prop="source_field_code" min-width="150" />
        <el-table-column :label="t('model.structural_constraints.target_table')" prop="target_table_code" min-width="180" />
        <el-table-column :label="t('model.structural_constraints.target_field')" prop="target_field_code" min-width="150" />
      </el-table>
    </template>
  </el-card>
</template>

<script setup>
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { logicalTableAPI } from '../api/model'
import { getModelErrorMessage } from '../utils/apiError'

const props = defineProps({ tableId: { type: Number, required: true }, version: { type: Number, required: true } })
const { t } = useI18n()
const constraints = ref(null)
const loading = ref(false)
const error = ref('')

watch(() => [props.tableId, props.version], async (_, __, onCleanup) => {
  let active = true
  onCleanup(() => { active = false })
  loading.value = true
  error.value = ''
  constraints.value = null
  try {
    const result = await logicalTableAPI.get(props.tableId)
    if (active) constraints.value = result.structural_constraints
  } catch (cause) {
    if (active) error.value = getModelErrorMessage(cause, t, 'model.common.load_failed')
  } finally {
    if (active) loading.value = false
  }
}, { immediate: true })
</script>

<style scoped>
.constraint-hint { margin: 0 0 16px; color: var(--addp-text-secondary); }
.constraint-relations { margin-top: 16px; }
:deep(.el-descriptions__content) { overflow-wrap: anywhere; }
</style>
