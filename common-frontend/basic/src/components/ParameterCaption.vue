<template>
  <span class="parameter-caption">
    <span>{{ parameterLabel(parameter, locale) }}</span>
    <span v-if="parameterDescription(parameter, locale)" class="parameter-help">{{ parameterDescription(parameter, locale) }}</span>
  </span>
</template>

<script setup>
import { useI18n } from 'vue-i18n'
import { parameterLabel, parameterDescription } from '../utils/parameterInput.mjs'
defineProps({ parameter: { type: Object, required: true } })
const { locale } = useI18n()
</script>

<style scoped>
.parameter-caption { display: flex; flex-direction: column; gap: 4px; white-space: normal; overflow-wrap: anywhere; }
.parameter-help { font-size: 12px; font-weight: normal; color: var(--addp-text-secondary); line-height: 1.5; }
</style>
