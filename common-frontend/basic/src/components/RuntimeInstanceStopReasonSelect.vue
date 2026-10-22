<template>
  <el-select v-model="selection" :empty-values="[null, undefined]" :aria-label="t('common.runtimeInstanceStopReason.label')" @change="emit('change')">
    <el-option value="" :label="t('common.runtimeInstanceStopReason.all')" />
    <el-option v-for="reason in runtimeInstanceStopReasons" :key="reason" :value="reason" :label="t(runtimeInstanceStopReasonLabelKey(reason))" />
  </el-select>
</template>

<script setup>
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { runtimeInstanceStopReasons, runtimeInstanceStopReasonLabelKey } from '../utils/runtimeInstanceStopReason.mjs'
const props = defineProps({ modelValue: { type: String, default: '' } })
const emit = defineEmits(['update:modelValue', 'change'])
const { t } = useI18n()
const selection = computed({ get: () => props.modelValue, set: value => emit('update:modelValue', value) })
</script>
