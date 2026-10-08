<template>
  <el-select
    :model-value="modelValue"
    :placeholder="t('manager.model3DUnit.defaultMeters')"
    :aria-label="t('manager.model3DUnit.label')"
    clearable
    @update:model-value="emit('update:modelValue', $event || '')"
  >
    <el-option v-for="unit in units" :key="unit" :value="unit" :label="t(`manager.model3DUnit.${unit}`)" />
  </el-select>
</template>

<script setup>
import { useI18n } from 'vue-i18n'
defineProps({ modelValue: { type: String, default: '' } })
const emit = defineEmits(['update:modelValue'])
const { t } = useI18n()
const units = ['mm', 'cm', 'm', 'km', 'in', 'ft', 'mi']
</script>
