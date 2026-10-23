<template>
  <section class="resource-comparison" :data-testid="testId" :aria-label="t('monitor.resources.comparison.title')">
    <h4>{{ title }}</h4>
    <div class="period-cards">
      <article v-for="period in ['current', 'previous']" :key="period" :data-testid="`${prefix}-${period}`">
        <h4>{{ t(`monitor.resources.comparison.${period}`) }}</h4>
        <strong>{{ formatValue(comparison[period].mean) }}</strong>
        <p>{{ date(comparison[period].start) }} — {{ date(comparison[period].end) }}</p>
        <p>{{ t('monitor.resources.comparison.coverage', { valid: comparison[period].valid, total: comparison[period].total }) }}</p>
      </article>
    </div>
    <p v-if="!comparison.complete" role="status" :data-testid="`${prefix}-incomplete`">{{ t('monitor.resources.comparison.incomplete') }}</p>
    <p v-else role="status" :data-testid="`${prefix}-change`"><strong>{{ change }}</strong></p>
    <p class="period-hint">{{ t('monitor.resources.comparison.method', { step: comparison.stepSeconds }) }}</p>
    <slot />
  </section>
</template>

<script setup>
import { useI18n } from 'vue-i18n'
import { formatFieldPresentationValue } from '../../../../common-frontend/basic/src/utils/fieldPresentation.mjs'
defineProps({ comparison: { type: Object, required: true }, title: { type: String, required: true }, formatValue: { type: Function, required: true }, change: { type: String, required: true }, prefix: { type: String, required: true }, testId: { type: String, required: true } })
const { t, locale } = useI18n()
const date = value => formatFieldPresentationValue(value, { temporal_format: 'datetime' }, locale.value)
</script>

<style scoped>
.resource-comparison { margin: 16px 0; min-width: 0; overflow-wrap: anywhere; }
h4 { margin: 12px 0; }
.period-cards { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(250px, 100%), 1fr)); gap: 16px; }
article { min-width: 0; border: 1px solid var(--addp-border-color); border-radius: 8px; padding: 16px; background: var(--addp-bg-primary); }
article strong { display: block; margin-top: 12px; font-size: 22px; }
article p, .period-hint { color: var(--addp-text-secondary); margin-top: 8px; font-size: 13px; }
</style>
