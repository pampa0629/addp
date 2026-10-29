<template>
  <p v-if="invalid" role="alert">{{ t('common.cleanupObservation.invalid') }}</p>
  <div v-else-if="admission" class="cleanup-admission">
    <strong>{{ t(`common.cleanupObservation.states.${admission.state}`) }}</strong>
    <p>{{ t(`common.cleanupObservation.explanations.${admission.state}`) }}</p>
    <small>{{ t('common.cleanupObservation.observedAt') }}: {{ new Date(admission.observed_at).toLocaleString() }}</small>
  </div>
</template>
<script setup>
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { parseCleanupAdmission } from '../utils/cleanupObservation.mjs'
const props = defineProps({ execution: { type: Object, required: true } })
const { t } = useI18n()
const parsed = computed(() => { try { return { value: parseCleanupAdmission(props.execution.cleanup_admission, props.execution) } } catch { return { invalid: true } } })
const admission = computed(() => parsed.value.value)
const invalid = computed(() => parsed.value.invalid)
</script>
<style scoped>
.cleanup-admission { color: var(--el-text-color-secondary); padding: 8px 0; }
p { margin: 4px 0; }
</style>
