<template>
  <el-input-number v-if="controlType === 'number'" :model-value="modelValue" :controls="false" @update:model-value="update" />
  <el-switch v-else-if="controlType === 'checkbox'" :model-value="Boolean(modelValue)" @update:model-value="update" />
  <el-date-picker
    v-else-if="controlType === 'date' || controlType === 'datetime'"
    :model-value="modelValue"
    :type="controlType === 'datetime' ? 'datetime' : 'date'"
    :value-format="controlType === 'datetime' ? 'YYYY-MM-DDTHH:mm:ssZ' : 'YYYY-MM-DD'"
    @update:model-value="update"
  />
  <el-select v-else-if="controlType === 'select'" :model-value="modelValue" clearable @update:model-value="update">
    <el-option :value="true" :label="t('workbench.booleanValues.true')" />
    <el-option :value="false" :label="t('workbench.booleanValues.false')" />
  </el-select>
  <el-select
    v-else-if="controlType === 'multiselect'"
    :model-value="modelValue"
    multiple
    filterable
    allow-create
    default-first-option
    @update:model-value="update"
  />
  <el-input v-else :model-value="modelValue" @update:model-value="update" />
</template>

<script setup>
import { useI18n } from 'vue-i18n'

defineProps({ modelValue: { default: '' }, controlType: { type: String, required: true } })
const emit = defineEmits(['update:modelValue'])
const { t } = useI18n()
const update = (value) => emit('update:modelValue', value)
</script>
