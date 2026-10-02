<template>
  <section class="execution-steps" :aria-label="t('common.executionSteps.title')">
    <h4>{{ t('common.executionSteps.title') }}</h4>
    <p v-if="currentStep">{{ t('common.executionSteps.current') }}：{{ currentStep }}</p>
    <p v-if="!steps.length" class="execution-steps-empty">{{ t('common.executionSteps.unrecorded') }}</p>
    <div v-else class="execution-steps-scroll">
      <table>
        <thead><tr>
          <th>{{ t('common.executionSteps.step') }}</th>
          <th>{{ t('common.executionSteps.status') }}</th>
          <th>{{ t('common.executionSteps.started') }}</th>
          <th>{{ t('common.executionSteps.ended') }}</th>
          <th>{{ t('common.executionSteps.duration') }}</th>
          <th>{{ t('common.executionSteps.error') }}</th>
        </tr></thead>
        <tbody><tr v-for="step in steps" :key="step.id" :data-status="step.status">
          <td>{{ step.id }}</td>
          <td><span class="execution-step-state">{{ recordedLabel(step, t(`common.executionSteps.states.${step.status}`)) }}</span>
            <small v-if="step.status === 'running' && ['dispatching', 'waiting'].includes(step.phase)" class="execution-step-phase">{{ recordedLabel(step, t(`common.executionSteps.phases.${step.phase}`)) }}</small>
          </td>
          <td>{{ step.started_at || '—' }}</td>
          <td>{{ step.ended_at || '—' }}</td>
          <td>{{ Number.isFinite(step.duration) ? `${step.duration} ms` : '—' }}</td>
          <td>{{ step.error_code ? t(`common.executionFailure.${step.error_code}`) : '—' }}</td>
        </tr></tbody>
      </table>
    </div>
    <p v-if="hasStoppedObservation" class="execution-step-observation-ended" role="status">{{ t('common.executionSteps.observationStopped') }}</p>
    <p v-if="attemptUnverified" role="status">{{ t('common.executionSteps.attemptUnverified') }}</p>
    <p v-if="truncated" role="status">{{ t('common.executionSteps.truncated') }}</p>
  </section>
</template>

<script setup>
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { isStepObservationStopped } from '../utils/executionStepObservation.mjs'
const props = defineProps({
  executionStatus: { type: String, required: true },
  steps: { type: Array, default: () => [] },
  currentStep: { type: String, default: '' },
  truncated: { type: Boolean, default: false },
  attemptUnverified: { type: Boolean, default: false }
})
const { t } = useI18n()
const hasStoppedObservation = computed(() => props.steps.some(step => isStepObservationStopped(props.executionStatus, step.status)))
function recordedLabel(step, status) {
  return isStepObservationStopped(props.executionStatus, step.status)
    ? t('common.executionSteps.lastRecorded', { status }) : status
}
</script>

<style scoped>
.execution-steps-scroll { max-width: 100%; overflow-x: auto; }
table { width: 100%; border-collapse: collapse; font-size: var(--el-font-size-base); }
th, td { padding: 8px; border-bottom: 1px solid var(--el-border-color); text-align: left; vertical-align: top; overflow-wrap: anywhere; }
th { white-space: nowrap; color: var(--el-text-color-secondary); }
td:first-child { min-width: 110px; }
td:last-child { min-width: 160px; }
.execution-step-phase { display: block; color: var(--el-text-color-secondary); }
.execution-steps-empty { color: var(--el-text-color-secondary); }
tr[data-status="failed"], tr[data-status="timeout"] { color: var(--el-color-danger); }
</style>
